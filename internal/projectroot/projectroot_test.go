// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package projectroot_test

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tetriz-ai/local-ci/internal/projectroot"
)

// TestGetRepoName pins the worktree-aware contract that #10458 depends on: the repo
// name comes from `git remote origin`, so it stays "core" even when the checkout's
// directory basename is something else (the git-worktree case), and only falls back
// to the basename when there is no remote to read.
func TestGetRepoName(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}

	tests := []struct {
		name      string
		remoteURL string // "" means: do not add a remote (fallback path)
		want      string // "" means: expect the temp dir basename
	}{
		{
			name:      "ssh remote wins over directory basename",
			remoteURL: "git@github.com:example-org/myrepo.git",
			want:      "myrepo",
		},
		{
			name:      "https remote wins over directory basename",
			remoteURL: "https://github.com/example-org/myrepo.git",
			want:      "myrepo",
		},
		{
			name:      "trailing slash on the remote url is tolerated",
			remoteURL: "https://github.com/example-org/myrepo/",
			want:      "myrepo",
		},
		{
			name:      "no remote falls back to directory basename",
			remoteURL: "",
			want:      "", // filled in with the temp dir basename below
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// A temp dir whose basename is deliberately NOT the repo name, so a name
			// equal to "core" can only have come from the remote, not the path.
			dir := t.TempDir()
			mustRun(t, dir, "git", "init")
			if tt.remoteURL != "" {
				mustRun(t, dir, "git", "remote", "add", "origin", tt.remoteURL)
			}
			want := tt.want
			if want == "" {
				want = filepath.Base(dir)
			}
			if got := projectroot.GetRepoName(dir); got != want {
				t.Errorf("GetRepoName(%q) = %q, want %q", dir, got, want)
			}
		})
	}
}

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}
