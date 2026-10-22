// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"testing"

	"github.com/tetriz-ai/local-ci/projectinfo"
)

type cachedResultStamper struct{ stamp *projectinfo.StampInfo }

func (s cachedResultStamper) Stamp(_ *projectinfo.ProjectInfo) error { return nil }
func (s cachedResultStamper) RetrieveLastValidStamp(_ *projectinfo.ProjectInfo) (*projectinfo.StampInfo, error) {
	return s.stamp, nil
}

type cacheTestChecker struct{ name, key string }

func (c cacheTestChecker) GetName() string            { return c.name }
func (c cacheTestChecker) GetLciStampKey() string     { return c.key }
func (c cacheTestChecker) GetWorkflowNames() []string { return []string{c.name} }
func (c cacheTestChecker) Execute(_ *projectinfo.ProjectInfo) []*Report {
	return []*Report{{Name: c.key, Status: Success}}
}
func (c cacheTestChecker) Cleanup(_ *projectinfo.ProjectInfo) error { return nil }
func (c cacheTestChecker) GetExecutionMode() ExecutionMode          { return Serial }
func (c cacheTestChecker) GetFileFilter(_ *projectinfo.ProjectInfo) []string {
	return []string{"**"}
}

func TestEligibleCachedSuccessUserOptOut(t *testing.T) {
	for _, tt := range []struct {
		name               string
		disabled, selected []string
		wantInvalidated    bool
		noDiff             bool
	}{
		{name: "disabled", disabled: []string{"custom"}, wantInvalidated: true},
		{name: "unselected", selected: []string{"other"}, wantInvalidated: true},
		{name: "normal cache reuse"},
		{name: "explicitly selected cache reuse", selected: []string{"custom"}},
		{name: "no diff non-opt-out", noDiff: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stamp := &projectinfo.StampInfo{CommitSHA: "head", ChecksStatus: map[string]bool{"custom_key": true}}
			files := &projectinfo.AffectedFiles{ModifiedFiles: []string{"file.go"}}
			if tt.noDiff {
				files.ModifiedFiles = nil
			}
			p := &projectinfo.ProjectInfo{
				RepoName: "repo", UserInput: &projectinfo.UserInput{DisabledWorkflows: tt.disabled, SelectiveWorkflows: tt.selected},
				PrData:   &projectinfo.PrData{LatestCommit: "head"},
				DiffData: &projectinfo.DiffData{FilesChanges: files, OverallFilesChanges: files},
			}
			got, err := eligibleWithStamper(cachedResultStamper{stamp}, []Checker{cacheTestChecker{"custom", "custom_key"}}, p)
			if err != nil || len(got) != 0 {
				t.Fatalf("cached or opted-out checker must not run: %v, %v", got, err)
			}
			invalidated := false
			for _, wf := range p.AffectedWorkflows {
				if wf.GetLciStampKey() == "custom_key" {
					invalidated = true
				}
			}
			if invalidated != tt.wantInvalidated {
				t.Fatalf("cached custom_key invalidation = %v, want %v", invalidated, tt.wantInvalidated)
			}
			if excluded := len(p.ExcludedAffectedWorkflows) > 0; excluded != tt.wantInvalidated {
				t.Fatalf("excluded checker recorded = %v, want %v", excluded, tt.wantInvalidated)
			}
		})
	}
}
