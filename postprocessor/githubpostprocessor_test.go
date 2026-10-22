// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package postprocessor

import (
	"testing"

	"github.com/tetriz-ai/local-ci/projectinfo"
)

// TestShouldApplyRunWorkflowsLabel pins the three conditions that withhold the
// RunWorkflows label. Review state is deliberately absent from that list — the
// label only triggers the mandatory check and any developer can add it by hand,
// so gating it on a review stops nobody. A case here asserting the label is
// applied on a PR with no reviews is what keeps that decision from being quietly
// reversed.
//
// The receiver is a zero value on purpose: with review state gone, no path
// reaches the GitHub client, so a passing test is also the proof that this
// function makes no network call.
func TestShouldApplyRunWorkflowsLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info *projectinfo.ProjectInfo
		want bool
	}{
		{
			name: "all checks passed, nothing pending, no reviews involved — apply",
			info: &projectinfo.ProjectInfo{
				UserInput:     &projectinfo.UserInput{PrNumber: 1},
				CheckerStatus: map[string]bool{"client_lint": true, "arch_test": true},
			},
			want: true,
		},
		{
			name: "no checkers ran at all — nothing failed, so apply",
			info: &projectinfo.ProjectInfo{
				UserInput:     &projectinfo.UserInput{PrNumber: 1},
				CheckerStatus: map[string]bool{},
			},
			want: true,
		},
		{
			name: "running inside GitHub Actions — the workflow is already the runner",
			info: &projectinfo.ProjectInfo{
				UserInput:     &projectinfo.UserInput{PrNumber: 1, GhActionMode: true},
				CheckerStatus: map[string]bool{"client_lint": true},
			},
			want: false,
		},
		{
			name: "a checker failed locally — withhold",
			info: &projectinfo.ProjectInfo{
				UserInput:     &projectinfo.UserInput{PrNumber: 1},
				CheckerStatus: map[string]bool{"client_lint": true, "arch_test": false},
			},
			want: false,
		},
		{
			name: "a workflow was excluded by a filter and never ran — withhold",
			info: &projectinfo.ProjectInfo{
				UserInput:                 &projectinfo.UserInput{PrNumber: 1},
				CheckerStatus:             map[string]bool{"client_lint": true},
				ExcludedAffectedWorkflows: []string{"arch_test"},
			},
			want: false,
		},
		{
			name: "GitHub Actions mode wins even when everything else would apply",
			info: &projectinfo.ProjectInfo{
				UserInput:                 &projectinfo.UserInput{PrNumber: 1, GhActionMode: true},
				CheckerStatus:             map[string]bool{"arch_test": false},
				ExcludedAffectedWorkflows: []string{"client_lint"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := (&githubPostProcessor{}).shouldApplyRunWorkflowsLabel(tt.info); got != tt.want {
				t.Errorf("shouldApplyRunWorkflowsLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}
