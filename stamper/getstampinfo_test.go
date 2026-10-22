// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package stamper

import (
	"testing"

	"github.com/tetriz-ai/local-ci/projectinfo"
	"github.com/tetriz-ai/local-ci/workflow"
)

// I5: getStampInfo must drop a prior stamp's result for a check that is affected
// by this diff but did not run this time, rather than carry it as a stale pass.
// A mutation removing the delete-affected loop survived without this test.
func TestGetStampInfo_DropsStaleAffectedCheck(t *testing.T) {
	staleWf := workflow.New("stale_check")
	staleWf.Jobs.RunChecks.With.LciStampKey = "stale_check"
	n := &NotesStamper{lastStamp: &projectinfo.StampInfo{
		ChecksStatus: map[string]bool{"stale_check": true, "kept_check": true},
	}}
	p := &projectinfo.ProjectInfo{
		CheckerStatus:     map[string]bool{}, // stale_check did not run this time
		AffectedWorkflows: []*workflow.Workflow{staleWf},
		ToolInfo:          &projectinfo.ToolInfo{Version: "v1.0.0"},
		PrData:            &projectinfo.PrData{LatestCommit: "deadbeef", BaseBranch: "main"},
	}

	got := n.getStampInfo(p)
	if _, present := got.ChecksStatus["stale_check"]; present {
		t.Errorf("an affected check that did not run was carried as a stale pass: %v", got.ChecksStatus)
	}
	if !got.ChecksStatus["kept_check"] {
		t.Errorf("a non-affected prior check should be preserved: %v", got.ChecksStatus)
	}
}
