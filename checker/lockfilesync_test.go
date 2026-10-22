// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tetriz-ai/local-ci/projectinfo"
)

func TestLockfilePathFor(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		want string
	}{
		{name: "repo root", dir: ".", want: "pnpm-lock.yaml"},
		{name: "nested workspace", dir: "web", want: "web/pnpm-lock.yaml"},
		{name: "deeper workspace", dir: "apps/web", want: "apps/web/pnpm-lock.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lockfilePathFor(tt.dir); got != tt.want {
				t.Errorf("lockfilePathFor(%q) = %q, want %q", tt.dir, got, tt.want)
			}
		})
	}
}

func TestLockfileSyncChecker_GetFileFilter(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{name: "location-generic manifest globs", want: []string{"**/package.json", "**/pnpm-lock.yaml"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewLockfileSyncChecker().GetFileFilter(nil); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetFileFilter() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Fail-closed: with pnpm absent the check must report Failure with the install
// message — never a silent Success. pnpm runs first, so git is not needed here.
func TestLockfileSyncChecker_FailsClosedWhenPnpmMissing(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "pnpm not on PATH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PATH", t.TempDir())
			p := &projectinfo.ProjectInfo{RepoPath: t.TempDir(), UserInput: &projectinfo.UserInput{}}
			got := NewLockfileSyncChecker().Execute(p)
			if got[0].Status != Failure {
				t.Fatalf("Execute() = %v, want Failure when pnpm is missing", got[0].Status)
			}
			if got[0].Err == nil || !strings.Contains(got[0].Err.Error(), "pnpm not installed") {
				t.Errorf("Execute() err = %v, want the install message", got[0].Err)
			}
		})
	}
}

// With a fake pnpm and a real temp git repo: a fake that dirties the lockfile
// fails AND the tree is restored (git status clean after); a fake that makes no
// change succeeds. No registry call — pnpm is faked.
func TestLockfileSyncChecker_DriftFailsAndTreeIsRestored(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tests := []struct {
		name        string
		pnpmScript  string // runs with cwd = repo root
		wantStatus  ExecutionStatus
		wantRestore bool // tree must be clean afterwards
	}{
		{
			name:        "lockfile drift fails and is restored",
			pnpmScript:  `printf 'lockfileVersion: drifted\n' > pnpm-lock.yaml`,
			wantStatus:  Failure,
			wantRestore: true,
		},
		{
			name:        "no drift succeeds",
			pnpmScript:  `true`,
			wantStatus:  Success,
			wantRestore: true,
		},
		{
			// pnpm dirties the lockfile and THEN fails: the failure branch must
			// still restore the tree, not leave it dirty for a later serial checker.
			name:        "pnpm failure after dirtying the lockfile still restores",
			pnpmScript:  `printf 'lockfileVersion: drifted\n' > pnpm-lock.yaml; exit 1`,
			wantStatus:  Failure,
			wantRestore: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := initGitRepoWithLockfile(t)
			fakeBin(t, "pnpm", tt.pnpmScript)

			p := &projectinfo.ProjectInfo{RepoPath: repo, UserInput: &projectinfo.UserInput{}}
			got := NewLockfileSyncChecker().Execute(p)
			if got[0].Status != tt.wantStatus {
				t.Fatalf("Execute() = %v (err=%v), want %v", got[0].Status, got[0].Err, tt.wantStatus)
			}
			if tt.wantRestore && !gitTreeClean(t, repo) {
				t.Errorf("working tree is dirty after the check; the lockfile was not restored:\n%s", gitStatusPorcelain(t, repo))
			}
		})
	}
}

// initGitRepoWithLockfile creates a temp git repo with one committed
// pnpm-lock.yaml and returns its path.
func initGitRepoWithLockfile(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=local-ci-test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=local-ci-test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "pnpm-lock.yaml"), []byte("lockfileVersion: '9.0'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "pnpm-lock.yaml")
	git("commit", "-q", "-m", "init")
	return repo
}

func gitStatusPorcelain(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "status", "--porcelain")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	return string(out)
}

func gitTreeClean(t *testing.T, repo string) bool {
	t.Helper()
	return strings.TrimSpace(gitStatusPorcelain(t, repo)) == ""
}

// Cleanup is the safety net that restores the lockfile when execute() exits
// before its own restore (e.g. a panic). Called directly on a repo whose
// lockfile is dirty, it must leave the tree clean.
func TestLockfileSyncChecker_CleanupRestoresDirtyLockfile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tests := []struct {
		name string
	}{
		{name: "dirty lockfile is restored by Cleanup"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := initGitRepoWithLockfile(t)
			if err := os.WriteFile(filepath.Join(repo, "pnpm-lock.yaml"), []byte("lockfileVersion: drifted\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if gitTreeClean(t, repo) {
				t.Fatal("precondition: the lockfile should be dirty before Cleanup")
			}
			p := &projectinfo.ProjectInfo{RepoPath: repo, UserInput: &projectinfo.UserInput{}}
			if err := NewLockfileSyncChecker().Cleanup(p); err != nil {
				t.Fatalf("Cleanup() error = %v", err)
			}
			if !gitTreeClean(t, repo) {
				t.Errorf("working tree is dirty after Cleanup; the lockfile was not restored:\n%s", gitStatusPorcelain(t, repo))
			}
		})
	}
}

func TestLockfileSyncChecker_GetExecutionMode(t *testing.T) {
	tests := []struct {
		name string
		want ExecutionMode
	}{
		{name: "lockfile sync is serial because it mutates the tree", want: Serial},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewLockfileSyncChecker().GetExecutionMode(); got != tt.want {
				t.Errorf("GetExecutionMode() = %v, want %v (it mutates the tree)", got, tt.want)
			}
		})
	}
}

func TestLockfileSyncChecker_WithLockfileDir(t *testing.T) {
	tests := []struct {
		name string
		opts []LockfileSyncOpt
		want string
	}{
		{name: "default is repo root", opts: nil, want: "pnpm-lock.yaml"},
		{name: "configured dir", opts: []LockfileSyncOpt{WithLockfileDir("web")}, want: "web/pnpm-lock.yaml"},
		{name: "explicit override wins", opts: []LockfileSyncOpt{WithLockfileDir("web"), WithLockfilePath("custom/pnpm-lock.yaml")}, want: "custom/pnpm-lock.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewLockfileSyncChecker(tt.opts...).lockfile(); got != tt.want {
				t.Errorf("effective lockfile path = %q, want %q", got, tt.want)
			}
		})
	}
}
