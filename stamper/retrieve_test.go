// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package stamper

import (
	stderrors "errors"
	"testing"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

// stampCommit writes a stamp on the repo's head commit with the given base and
// version, so a retrieval test can check what counts as a valid stamp.
func stampCommit(t *testing.T, repo, sha, base, version string) {
	t.Helper()
	p := &projectinfo.ProjectInfo{
		RepoPath:      repo,
		CheckerStatus: map[string]bool{"a_check": true},
		ToolInfo:      &projectinfo.ToolInfo{Version: version},
		PrData:        &projectinfo.PrData{BaseBranch: base, LatestCommit: sha, AllCommits: []string{sha}},
	}
	if err := (&NotesStamper{}).Stamp(p); err != nil {
		t.Fatalf("Stamp(): %v", err)
	}
}

// T1: RetrieveLastValidStamp rejects a stamp whose base branch differs from the
// PR's, and one whose major/minor version differs from the running tool; a
// same-base, same-major/minor (patch-different) stamp is valid.
func TestRetrieveLastValidStamp_Invalidation(t *testing.T) {
	repo, sha := newRepoWithRemote(t)
	stampCommit(t, repo, sha, "main", "v1.0.0")

	tests := []struct {
		name      string
		base      string
		version   string
		wantValid bool
	}{
		{name: "same base, patch-different version is valid", base: "main", version: "v1.0.5", wantValid: true},
		{name: "different base is rejected", base: "develop", version: "v1.0.0", wantValid: false},
		{name: "different major/minor version is rejected", base: "main", version: "v2.0.0", wantValid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &projectinfo.ProjectInfo{
				RepoPath: repo,
				ToolInfo: &projectinfo.ToolInfo{Version: tt.version},
				PrData:   &projectinfo.PrData{BaseBranch: tt.base, AllCommits: []string{sha}},
			}
			stamp, err := (&NotesStamper{}).RetrieveLastValidStamp(p)
			if tt.wantValid {
				if err != nil || stamp == nil {
					t.Fatalf("RetrieveLastValidStamp() = (%v, %v), want a valid stamp", stamp, err)
				}
				return
			}
			if !stderrors.Is(err, NoStampPresentError) {
				t.Errorf("RetrieveLastValidStamp() err = %v, want NoStampPresentError", err)
			}
		})
	}
}

// T2: a note that is not valid base64/JSON surfaces errStampDecode rather than
// an empty stamp read as valid.
func TestGetExistingStampForCommit_DecodeError(t *testing.T) {
	repo, sha := newRepoWithRemote(t)
	runGit(t, repo, "notes", "add", "-f", "-m", "this-is-not-base64-or-json!!!", sha)

	n := &NotesStamper{}
	runner, err := cmdrunner.NewCmdRunner(repo, cmdrunner.WithNoLogger())
	if err != nil {
		t.Fatal(err)
	}
	n.cmdExec = runner
	if _, err := n.getExistingStampForCommit(sha); !stderrors.Is(err, errStampDecode) {
		t.Errorf("getExistingStampForCommit() err = %v, want errStampDecode", err)
	}
}
