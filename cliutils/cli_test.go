// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package cliutils

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetupOutputCliDirSymlinkContainment pins the fix for the output-setup escape:
// SetupOutputCliDir must never let a symlinked output/ redirect its RemoveAll,
// MkdirAll or .gitignore write outside the repository. Each case builds a repo root
// plus a sibling "outside" tree holding sentinels, points output/ (or output/local-ci) in
// various ways, runs setup, and asserts the outside sentinels are untouched while the
// ordinary paths still work.
func TestSetupOutputCliDirSymlinkContainment(t *testing.T) {
	const gitignore = "# Created by local-ci — keeps local-ci's working output out of git.\n*\n"

	// build creates <base>/repo and <base>/outside with sentinels, runs any
	// per-case arrangement, and returns the repo root and outside dir.
	tests := []struct {
		name string
		// arrange sets up output/ before the call; it returns the path passed to
		// SetupOutputCliDir (always <repo>/output/local-ci).
		arrange func(t *testing.T, repo, outside string) string
		wantErr bool
		// verify runs after the call; outside sentinels must always survive.
		verify func(t *testing.T, repo, outside string)
	}{
		{
			name: "ordinary first run creates output tree",
			arrange: func(_ *testing.T, repo, _ string) string {
				return filepath.Join(repo, "output", "local-ci")
			},
			wantErr: false,
			verify: func(t *testing.T, repo, _ string) {
				assertDir(t, filepath.Join(repo, "output", "local-ci"))
				assertFileContent(t, filepath.Join(repo, "output", ".gitignore"), gitignore)
				assertExists(t, filepath.Join(repo, "output", "local-ci", "stdoutput.log"))
			},
		},
		{
			name: "repeated run is idempotent",
			arrange: func(t *testing.T, repo, _ string) string {
				path := filepath.Join(repo, "output", "local-ci")
				if err := SetupOutputCliDir(path); err != nil {
					t.Fatalf("priming run failed: %v", err)
				}
				return path
			},
			wantErr: false,
			verify: func(t *testing.T, repo, _ string) {
				assertDir(t, filepath.Join(repo, "output", "local-ci"))
				assertFileContent(t, filepath.Join(repo, "output", ".gitignore"), gitignore)
			},
		},
		{
			name: "escaping intermediate symlink (output -> outside) is refused",
			arrange: func(t *testing.T, repo, outside string) string {
				if err := os.Symlink(outside, filepath.Join(repo, "output")); err != nil {
					t.Fatalf("symlink: %v", err)
				}
				return filepath.Join(repo, "output", "local-ci")
			},
			wantErr: true,
			verify:  func(_ *testing.T, _, _ string) {},
		},
		{
			name: "escaping final symlink (output/local-ci -> outside/local-ci) is refused",
			arrange: func(t *testing.T, repo, outside string) string {
				if err := os.MkdirAll(filepath.Join(repo, "output"), 0o750); err != nil {
					t.Fatalf("mkdir output: %v", err)
				}
				if err := os.Symlink(filepath.Join(outside, "local-ci"), filepath.Join(repo, "output", "local-ci")); err != nil {
					t.Fatalf("symlink: %v", err)
				}
				return filepath.Join(repo, "output", "local-ci")
			},
			wantErr: true,
			verify: func(t *testing.T, repo, _ string) {
				fi, err := os.Lstat(filepath.Join(repo, "output", "local-ci"))
				if err != nil {
					t.Fatal(err)
				}
				if fi.Mode()&os.ModeSymlink == 0 {
					t.Error("refused symlink was changed")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			repo := filepath.Join(base, "repo")
			outside := filepath.Join(base, "outside")
			mustMkdir(t, repo)
			mustMkdir(t, filepath.Join(outside, "local-ci"))
			mustWrite(t, filepath.Join(outside, "local-ci", "user.txt"), "precious")
			mustWrite(t, filepath.Join(outside, ".gitignore"), "keepme")

			path := tt.arrange(t, repo, outside)
			err := SetupOutputCliDir(path)

			if tt.wantErr && err == nil {
				t.Fatalf("SetupOutputCliDir(%q) = nil, want error", path)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("SetupOutputCliDir(%q) = %v, want nil", path, err)
			}

			// Outside sentinels must survive in every case.
			assertExists(t, filepath.Join(outside, "local-ci", "user.txt"))
			assertFileContent(t, filepath.Join(outside, ".gitignore"), "keepme")

			tt.verify(t, repo, outside)
		})
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatalf("mkdir %q: %v", path, err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %q to exist: %v", path, err)
	}
}

func assertDir(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Errorf("expected directory %q: %v", path, err)
		return
	}
	if !fi.IsDir() {
		t.Errorf("expected %q to be a directory", path)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("read %q: %v", path, err)
		return
	}
	if string(got) != want {
		t.Errorf("%q content = %q, want %q", path, string(got), want)
	}
}

func TestSetupOutputCliDirPreservesUserFiles(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
	}{
		{name: "unowned output", files: map[string]string{"output/local-ci/user.txt": "user data"}},
		{name: "mixed output", files: map[string]string{"output/local-ci/stdoutput.log": "previous log", "output/local-ci/user.txt": "user data"}},
		{name: "unowned log collision", files: map[string]string{"output/local-ci/stdoutput.log": "user log"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			for name, body := range tt.files {
				path := filepath.Join(repo, name)
				mustMkdir(t, filepath.Dir(path))
				mustWrite(t, path, body)
			}
			if err := SetupOutputCliDir(filepath.Join(repo, "output", "local-ci")); err == nil {
				t.Error("expected refusal of unowned output")
			}
			for name, body := range tt.files {
				assertFileContent(t, filepath.Join(repo, name), body)
			}
			if _, err := os.Lstat(filepath.Join(repo, "output", ".gitignore")); !os.IsNotExist(err) {
				t.Error("refusal mutated output/.gitignore")
			}
		})
	}
}

func TestSetupOutputCliDirPreservesExistingGitignore(t *testing.T) {
	repo := t.TempDir()
	mustMkdir(t, filepath.Join(repo, "output"))
	const content = "# user policy\n*.tmp\n!important.txt\n"
	mustWrite(t, filepath.Join(repo, "output", ".gitignore"), content)
	if err := SetupOutputCliDir(filepath.Join(repo, "output", "local-ci")); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(repo, "output", ".gitignore"), content)
}

func TestSetupOutputCliDirOwnedLifecycle(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*testing.T, string)
		wantErr bool
	}{
		{name: "unchanged completed output", change: func(t *testing.T, path string) {
			mustWrite(t, filepath.Join(path, "summary.log"), "summary")
			mustMkdir(t, filepath.Join(path, "custom"))
			mustWrite(t, filepath.Join(path, "custom", "check.log"), "custom")
			if err := FinalizeOutputCliDir(path); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing owned file", change: func(t *testing.T, path string) {
			if err := os.Remove(filepath.Join(path, "stdoutput.log")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "edited owned log", wantErr: true, change: func(t *testing.T, path string) { mustWrite(t, filepath.Join(path, "stdoutput.log"), "edited") }},
		{name: "new user file mixed with owned logs", wantErr: true, change: func(t *testing.T, path string) { mustWrite(t, filepath.Join(path, "user.txt"), "user") }},
		{name: "new empty user directory", wantErr: true, change: func(t *testing.T, path string) { mustMkdir(t, filepath.Join(path, "user")) }},
		{name: "bad manifest schema", wantErr: true, change: func(t *testing.T, path string) {
			mustWrite(t, filepath.Join(path, outputManifest), `{"version":1,"files":[]}`)
		}},
		{name: "trailing manifest data", wantErr: true, change: func(t *testing.T, path string) {
			f, err := os.OpenFile(filepath.Join(path, outputManifest), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.WriteString("{}"); err != nil {
				t.Fatal(err)
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing manifest", wantErr: true, change: func(t *testing.T, path string) {
			if err := os.Remove(filepath.Join(path, outputManifest)); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unreadable owned output", wantErr: true, change: func(t *testing.T, path string) {
			if os.Geteuid() == 0 {
				t.Skip("root bypasses permissions")
			}
			file := filepath.Join(path, "stdoutput.log")
			if err := os.Chmod(file, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(file, 0o600) })
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			path := filepath.Join(repo, "output", "local-ci")
			if err := SetupOutputCliDir(path); err != nil {
				t.Fatal(err)
			}
			tt.change(t, path)
			before := treeSnapshot(t, path)
			err := SetupOutputCliDir(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr {
				if got := treeSnapshot(t, path); got != before {
					t.Fatalf("refusal changed prior tree: before=%s after=%s", before, got)
				}
			} else {
				assertFileContent(t, filepath.Join(path, "stdoutput.log"), "")
				for _, name := range []string{"summary.log", "custom"} {
					if _, err := os.Lstat(filepath.Join(path, name)); !os.IsNotExist(err) {
						t.Errorf("stale owned output %s remains", name)
					}
				}
			}
		})
	}
}

func treeSnapshot(t *testing.T, path string) string {
	t.Helper()
	entries := map[string]string{}
	err := filepath.WalkDir(path, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(path, name)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			body, err := os.ReadFile(name)
			if err != nil {
				entries[rel] = "unreadable"
			} else {
				entries[rel] = string(body)
			}
		} else {
			entries[rel] = info.Mode().String()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestOutputSymlinksAndHardlinks(t *testing.T) {
	tests := []struct {
		name   string
		link   bool
		target string
	}{
		{name: "log symlink", link: true, target: "stdoutput.log"},
		{name: "nested symlink", link: true, target: "custom"},
		{name: "manifest symlink", link: true, target: outputManifest},
		{name: "hard linked log", target: "stdoutput.log"},
		{name: "hard linked manifest", target: outputManifest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			path := filepath.Join(repo, "output", "local-ci")
			if err := SetupOutputCliDir(path); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "keep")
			const sentinel = "outside user content"
			mustWrite(t, outside, sentinel)
			target := filepath.Join(path, tt.target)
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			var err error
			if tt.link {
				err = os.Symlink(outside, target)
			} else {
				err = os.Link(outside, target)
			}
			if err != nil {
				t.Fatal(err)
			}
			before := treeSnapshot(t, path)
			if err := SetupOutputCliDir(path); err == nil {
				t.Fatal("expected refusal")
			}
			assertFileContent(t, outside, sentinel)
			if got := treeSnapshot(t, path); got != before {
				t.Fatal("refusal changed tree")
			}
		})
	}
}

func TestOutputIgnoresPreservedAndGitClean(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(fmt.Sprint(custom), func(t *testing.T) {
			repo := t.TempDir()
			run := func(args ...string) string {
				cmd := exec.Command("git", args...)
				cmd.Dir = repo
				body, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v %s", args, err, body)
				}
				return string(body)
			}
			run("init", "-q")
			if custom {
				mustMkdir(t, filepath.Join(repo, "output"))
				mustWrite(t, filepath.Join(repo, "output", ".gitignore"), "*.tmp\n!important.txt\n")
				run("add", "output/.gitignore")
			}
			before := run("status", "--porcelain", "--untracked-files=all")
			path := filepath.Join(repo, "output", "local-ci")
			if err := SetupOutputCliDir(path); err != nil {
				t.Fatal(err)
			}
			mustWrite(t, filepath.Join(path, "summary.log"), "summary")
			if err := FinalizeOutputCliDir(path); err != nil {
				t.Fatal(err)
			}
			after := run("status", "--porcelain", "--untracked-files=all")
			if before != after {
				t.Fatalf("git status changed: %q -> %q", before, after)
			}
		})
	}
}

func TestOutputSetupCleanupFailurePreservesFiles(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	repo := t.TempDir()
	path := filepath.Join(repo, "output", "local-ci")
	if err := SetupOutputCliDir(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o750) })
	before := treeSnapshot(t, path)
	if err := SetupOutputCliDir(path); err == nil {
		t.Fatal("expected cleanup permission error")
	}
	if got := treeSnapshot(t, path); got != before {
		t.Fatalf("failed cleanup changed files: before=%s after=%s", before, got)
	}
	if err := FinalizeOutputCliDir(path); err == nil {
		t.Fatal("expected snapshot permission error")
	}
	if got := treeSnapshot(t, path); got != before {
		t.Fatal("failed finalization changed files")
	}
}

func TestOutputManifestRejectsUnsafeEntries(t *testing.T) {
	for _, name := range []string{"../outside", "/outside", ".", outputManifest, "a\\b"} {
		t.Run(strings.ReplaceAll(name, "/", "-"), func(t *testing.T) {
			repo := t.TempDir()
			path := filepath.Join(repo, "output", "local-ci")
			if err := SetupOutputCliDir(path); err != nil {
				t.Fatal(err)
			}
			content, _ := json.Marshal(outputOwnership{Version: 1, Files: map[string]string{name: strings.Repeat("0", 64)}})
			mustWrite(t, filepath.Join(path, outputManifest), string(content))
			before := treeSnapshot(t, path)
			if err := SetupOutputCliDir(path); err == nil {
				t.Fatal("expected invalid entry refusal")
			}
			if treeSnapshot(t, path) != before {
				t.Fatal("invalid manifest caused mutation")
			}
		})
	}
}

func TestOutputLinkedIgnoreIsNeverTruncated(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(fmt.Sprint(symlink), func(t *testing.T) {
			repo := t.TempDir()
			mustMkdir(t, filepath.Join(repo, "output"))
			outside := filepath.Join(t.TempDir(), "ignore")
			const contents = "user ignore policy\n"
			mustWrite(t, outside, contents)
			target := filepath.Join(repo, "output", ".gitignore")
			var err error
			if symlink {
				err = os.Symlink(outside, target)
			} else {
				err = os.Link(outside, target)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := SetupOutputCliDir(filepath.Join(repo, "output", "local-ci")); err != nil {
				t.Fatal(err)
			}
			assertFileContent(t, outside, contents)
			assertFileContent(t, target, contents)
		})
	}
}

func TestOutputUnchangedHardlinksRemainOutside(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "output", "local-ci")
	if err := SetupOutputCliDir(path); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	for _, name := range []string{"stdoutput.log", outputManifest} {
		if err := os.Link(filepath.Join(path, name), filepath.Join(outside, name)); err != nil {
			t.Fatal(err)
		}
	}
	previous := treeSnapshot(t, outside)
	mustWrite(t, filepath.Join(path, "summary.log"), "completed")
	if err := FinalizeOutputCliDir(path); err != nil {
		t.Fatal(err)
	}
	if err := SetupOutputCliDir(path); err != nil {
		t.Fatal(err)
	}
	if treeSnapshot(t, outside) != previous {
		t.Fatal("finalization/setup mutated hard linked outside files")
	}
}

func TestOutputInRepositoryAncestorLinkIsRefused(t *testing.T) {
	repo := t.TempDir()
	target := filepath.Join(repo, "real-output")
	mustMkdir(t, target)
	mustWrite(t, filepath.Join(target, "user"), "keep")
	if err := os.Symlink(target, filepath.Join(repo, "output")); err != nil {
		t.Fatal(err)
	}
	before := treeSnapshot(t, target)
	if err := SetupOutputCliDir(filepath.Join(repo, "output", "local-ci")); err == nil {
		t.Fatal("expected ancestor link refusal")
	}
	if treeSnapshot(t, target) != before {
		t.Fatal("ancestor link target mutated")
	}
}

func TestOutputOversizedManifestIsRefusedBeforeMutation(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "output", "local-ci")
	if err := SetupOutputCliDir(path); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(path, outputManifest)
	body, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, []byte(strings.Repeat(" ", 4<<20))...)
	body = append(body, []byte("not json")...)
	if err := os.WriteFile(manifest, body, 0o600); err != nil {
		t.Fatal(err)
	}
	before := treeSnapshot(t, path)
	if err := SetupOutputCliDir(path); err == nil {
		t.Fatal("oversized manifest unexpectedly authorized cleanup")
	}
	if treeSnapshot(t, path) != before {
		t.Fatal("oversized manifest refusal changed output")
	}
}
