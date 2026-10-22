// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prepper

import (
	"os"
	"os/exec"
	"testing"

	"github.com/tetriz-ai/local-ci/projectinfo"
)

func TestWorktreeForBranch(t *testing.T) {
	// A representative `git worktree list --porcelain` dump: the main checkout on
	// master, a linked worktree holding the PR branch, and a detached-HEAD worktree
	// (no `branch` line) to confirm it is never mismatched.
	porcelain := "worktree /repo/main\n" +
		"HEAD 1111111111111111111111111111111111111111\n" +
		"branch refs/heads/master\n" +
		"\n" +
		"worktree /repo/.worktrees/feat\n" +
		"HEAD 2222222222222222222222222222222222222222\n" +
		"branch refs/heads/feat/my-branch\n" +
		"\n" +
		"worktree /repo/.worktrees/detached\n" +
		"HEAD 3333333333333333333333333333333333333333\n" +
		"detached\n"

	tests := []struct {
		name      string
		porcelain string
		branch    string
		want      string
	}{
		{
			name:      "branch checked out in a linked worktree",
			porcelain: porcelain,
			branch:    "feat/my-branch",
			want:      "/repo/.worktrees/feat",
		},
		{
			name:      "branch is the main checkout",
			porcelain: porcelain,
			branch:    "master",
			want:      "/repo/main",
		},
		{
			name:      "branch not checked out anywhere",
			porcelain: porcelain,
			branch:    "feat/other",
			want:      "",
		},
		{
			name:      "substring of a checked-out branch does not match",
			porcelain: porcelain,
			branch:    "feat/my",
			want:      "",
		},
		{
			name:      "empty porcelain output",
			porcelain: "",
			branch:    "feat/my-branch",
			want:      "",
		},
		{
			name:      "CRLF line endings are tolerated",
			porcelain: "worktree /repo/win\r\nHEAD abc\r\nbranch refs/heads/feat/win\r\n",
			branch:    "feat/win",
			want:      "/repo/win",
		},
		{
			// A branch line with no preceding worktree line must not return a
			// stale/phantom path — guards the parser's mutable path carry-over.
			name:      "branch line with no preceding worktree line",
			porcelain: "branch refs/heads/feat/orphan\n",
			branch:    "feat/orphan",
			want:      "",
		},
		{
			name: "multiple records, target is the last — path is not carried from earlier records",
			porcelain: "worktree /repo/a\nHEAD 1\nbranch refs/heads/aaa\n\n" +
				"worktree /repo/b\nHEAD 2\nbranch refs/heads/bbb\n\n" +
				"worktree /repo/c\nHEAD 3\nbranch refs/heads/ccc\n",
			branch: "ccc",
			want:   "/repo/c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := worktreeForBranch(tt.porcelain, tt.branch); got != tt.want {
				t.Errorf("worktreeForBranch() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGitDataCleanupTreatsBranchAsLiteralArgument(t *testing.T) {
	for _, branch := range []string{"feature/ordinary", "topic;printf${IFS}INJECTED"} {
		t.Run(branch, func(t *testing.T) {
			repo := t.TempDir()
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
			run := func(args ...string) string {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = repo
				body, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v %s", args, err, body)
				}
				return string(body)
			}
			run("init", "-q")
			run("-c", "user.name=Local CI Test", "-c", "user.email=local-ci@example.invalid", "commit", "--allow-empty", "-qm", "synthetic")
			run("branch", branch)
			g := &gitDataPrepper{initialBranch: branch, branchRestore: true}
			if err := g.Cleanup(&projectinfo.ProjectInfo{RepoPath: repo}); err != nil {
				t.Fatal(err)
			}
			if got := run("branch", "--show-current"); got != branch+"\n" {
				t.Fatalf("cleanup restored branch %q; want %q", got, branch)
			}
		})
	}
}

func TestGitDataCleanupRejectsOptionLikeBranch(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		body, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, body)
		}
		return string(body)
	}
	run("init", "-q")
	run("-c", "user.name=Local CI Test", "-c", "user.email=local-ci@example.invalid", "commit", "--allow-empty", "-qm", "synthetic")
	// Plumbing can create a ref that porcelain branch creation refuses.
	run("update-ref", "refs/heads/-f", "HEAD")
	before := run("symbolic-ref", "HEAD")
	for _, branch := range []string{"-f", ""} {
		t.Run(branch, func(t *testing.T) {
			g := &gitDataPrepper{initialBranch: branch, branchRestore: true}
			if err := g.Cleanup(&projectinfo.ProjectInfo{RepoPath: repo}); err == nil {
				t.Fatalf("cleanup accepted unsafe branch %q", branch)
			}
			if got := run("symbolic-ref", "HEAD"); got != before {
				t.Fatalf("cleanup changed HEAD from %q to %q", before, got)
			}
		})
	}
}
