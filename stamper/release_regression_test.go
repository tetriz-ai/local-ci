// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package stamper

import (
	"testing"

	"github.com/tetriz-ai/local-ci/projectinfo"
	"github.com/tetriz-ai/local-ci/workflow"
)

func TestRetrieveLastValidStamp_ReleaseVersions(t *testing.T) {
	repo, sha := newRepoWithRemote(t)
	for _, tt := range []struct {
		name, stamped, current string
		wantValid              bool
	}{
		{"release patch", "0.1.0", "0.1.1", true},
		{"mixed prefix", "v0.1.0", "0.1.1", true},
		{"release minor", "0.1.0", "0.2.0", false},
		{"release major", "0.1.0", "1.1.0", false},
		{"invalid versions", "dev", "dev", false},
		{"empty versions", "", "", false},
		{"invalid stamped", "dev", "0.1.0", false},
		{"invalid current", "0.1.0", "dev", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			writeRawStamp(t, repo, sha, projectinfo.StampInfo{CommitSHA: sha, Version: tt.stamped, BaseBranch: "main"})
			p := &projectinfo.ProjectInfo{RepoPath: repo,
				ToolInfo: &projectinfo.ToolInfo{Version: tt.current},
				PrData:   &projectinfo.PrData{BaseBranch: "main", AllCommits: []string{sha}}}
			stamp, err := (&NotesStamper{}).RetrieveLastValidStamp(p)
			if got := err == nil && stamp != nil; got != tt.wantValid {
				t.Fatalf("valid = %v, want %v; stamp = %v, error = %v", got, tt.wantValid, stamp, err)
			}
		})
	}
}

func TestGetStampInfo_DistinctCheckerStampKey(t *testing.T) {
	wf := workflow.New("checker_name")
	wf.Jobs.RunChecks.With.LciStampKey = "stamp_key"
	n := &NotesStamper{lastStamp: &projectinfo.StampInfo{ChecksStatus: map[string]bool{"stamp_key": true, "kept": true}}}
	p := &projectinfo.ProjectInfo{CheckerStatus: map[string]bool{}, AffectedWorkflows: []*workflow.Workflow{wf},
		ToolInfo: &projectinfo.ToolInfo{Version: "0.1.0"}, PrData: &projectinfo.PrData{LatestCommit: "head", BaseBranch: "main"}}
	got := n.getStampInfo(p)
	if _, present := got.ChecksStatus["stamp_key"]; present || !got.ChecksStatus["kept"] {
		t.Fatalf("stale affected key must be removed and unrelated status preserved: %v", got.ChecksStatus)
	}
}
