// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package stamper

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

// runGit runs a git command in dir and fails the test on error.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepoWithRemote creates a working repo with one commit and a local bare
// remote as origin, so the notes push/fetch flow runs entirely offline.
func newRepoWithRemote(t *testing.T) (repo, sha string) {
	t.Helper()
	base := t.TempDir()
	repo = filepath.Join(base, "work")
	bare := filepath.Join(base, "remote.git")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, base, "init", "--bare", "-b", "main", bare)
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "dev@example.com")
	runGit(t, repo, "config", "user.name", "Dev")
	runGit(t, repo, "commit", "--allow-empty", "-m", "initial")
	runGit(t, repo, "remote", "add", "origin", bare)
	runGit(t, repo, "push", "-u", "origin", "main")
	sha = runGit(t, repo, "rev-parse", "HEAD")
	return repo, sha
}

// TestStampAcceptsStampedRejectsUnstamped is the acceptance test: a custom
// checker's result is stamped onto the head commit and pushed to a (local)
// remote; the stamp is then readable and reports the custom check passed, while
// a commit that was never stamped reports no stamp present. This is the core
// the merge gate relies on — accept a stamped commit, reject an unstamped one.
func TestStampAcceptsStampedRejectsUnstamped(t *testing.T) {
	repo, sha := newRepoWithRemote(t)

	const customCheck = "my_custom_check"
	p := &projectinfo.ProjectInfo{
		RepoPath:      repo,
		CheckerStatus: map[string]bool{customCheck: true},
		ToolInfo:      &projectinfo.ToolInfo{Version: "v1.0.0"},
		PrData: &projectinfo.PrData{
			BaseBranch:   "main",
			LatestCommit: sha,
			AllCommits:   []string{sha},
		},
	}

	s := &NotesStamper{}
	if err := s.Stamp(p); err != nil {
		t.Fatalf("Stamp() error: %v", err)
	}

	// Accept: the stamped commit yields a stamp whose custom check passed and
	// whose base branch matches, exactly what the merge gate checks.
	reader := &NotesStamper{}
	runner, err := cmdrunner.NewCmdRunner(repo, cmdrunner.WithNoLogger())
	if err != nil {
		t.Fatal(err)
	}
	reader.cmdExec = runner
	stamp, err := reader.getExistingStampForCommit(sha)
	if err != nil {
		t.Fatalf("reading stamp back: %v", err)
	}
	if stamp.CommitSHA != sha {
		t.Errorf("stamp CommitSHA = %q, want %q", stamp.CommitSHA, sha)
	}
	if stamp.BaseBranch != "main" {
		t.Errorf("stamp BaseBranch = %q, want main", stamp.BaseBranch)
	}
	if !stamp.ChecksStatus[customCheck] {
		t.Errorf("stamp ChecksStatus[%q] = false, want true", customCheck)
	}

	// Reject: a fresh commit that was never stamped reports no stamp present.
	runGit(t, repo, "commit", "--allow-empty", "-m", "unstamped")
	unstampedSHA := runGit(t, repo, "rev-parse", "HEAD")
	if _, err := reader.getExistingStampForCommit(unstampedSHA); err == nil {
		t.Errorf("getExistingStampForCommit(unstamped) = nil error, want NoStampPresentError")
	}
}
