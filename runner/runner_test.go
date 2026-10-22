// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package runner

import (
	"reflect"
	"sync"
	"testing"

	checkerPkg "github.com/tetriz-ai/local-ci/checker"
	"github.com/tetriz-ai/local-ci/internal/syncmap"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

// countingChecker records how many times Cleanup is invoked on it.
type countingChecker struct {
	name     string
	mode     checkerPkg.ExecutionMode
	cleanups *int64
	mu       *sync.Mutex
}

func (c *countingChecker) GetName() string        { return c.name }
func (c *countingChecker) GetLciStampKey() string { return c.name }
func (c *countingChecker) GetWorkflowNames() []string {
	return []string{c.name}
}
func (c *countingChecker) Execute(_ *projectinfo.ProjectInfo) []*checkerPkg.Report {
	return []*checkerPkg.Report{{Name: c.name, Status: checkerPkg.Success}}
}
func (c *countingChecker) Cleanup(_ *projectinfo.ProjectInfo) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	*c.cleanups++
	return nil
}
func (c *countingChecker) GetExecutionMode() checkerPkg.ExecutionMode { return c.mode }
func (c *countingChecker) GetFileFilter(_ *projectinfo.ProjectInfo) []string {
	return []string{"**"}
}

// TestApplyCheckerStatusesFailureDominant pins the fix for the stamp integrity
// defect: when a checker emits several reports under one stamp key, the key must be
// stamped passed only if every report succeeded. The critical cases are a mixed
// slice in either order — a trailing Success must not overwrite an earlier Failure
// and hand the gate a passing stamp over a failed subcheck.
func TestApplyCheckerStatusesFailureDominant(t *testing.T) {
	rep := func(name string, status checkerPkg.ExecutionStatus) *checkerPkg.Report {
		return &checkerPkg.Report{Name: name, Status: status}
	}

	tests := []struct {
		name    string
		reports map[string][]*checkerPkg.Report
		want    map[string]bool
	}{
		{
			name:    "single success passes",
			reports: map[string][]*checkerPkg.Report{"a": {rep("a", checkerPkg.Success)}},
			want:    map[string]bool{"a": true},
		},
		{
			name:    "single failure fails",
			reports: map[string][]*checkerPkg.Report{"a": {rep("a", checkerPkg.Failure)}},
			want:    map[string]bool{"a": false},
		},
		{
			name:    "failure then success stays failed",
			reports: map[string][]*checkerPkg.Report{"a": {rep("a", checkerPkg.Failure), rep("a", checkerPkg.Success)}},
			want:    map[string]bool{"a": false},
		},
		{
			name:    "success then failure stays failed",
			reports: map[string][]*checkerPkg.Report{"a": {rep("a", checkerPkg.Success), rep("a", checkerPkg.Failure)}},
			want:    map[string]bool{"a": false},
		},
		{
			name:    "all success passes",
			reports: map[string][]*checkerPkg.Report{"a": {rep("a", checkerPkg.Success), rep("a", checkerPkg.Success)}},
			want:    map[string]bool{"a": true},
		},
		{
			name: "independent keys aggregate independently",
			reports: map[string][]*checkerPkg.Report{
				"a": {rep("a", checkerPkg.Success)},
				"b": {rep("b", checkerPkg.Failure), rep("b", checkerPkg.Success)},
			},
			want: map[string]bool{"a": true, "b": false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := &syncmap.Map[string, []*checkerPkg.Report]{}
			for key, reports := range tt.reports {
				events.Store(key, reports)
			}
			p := &projectinfo.ProjectInfo{CheckerStatus: map[string]bool{}}

			applyCheckerStatuses(p, events)

			if !reflect.DeepEqual(p.CheckerStatus, tt.want) {
				t.Errorf("CheckerStatus = %v, want %v", p.CheckerStatus, tt.want)
			}
		})
	}
}

// TestCleanupEnqueuedOncePerChecker pins the fix for the double/triple-cleanup
// defect: the serial, parallel and preflight phases must each enqueue only the
// checkers they actually run, so every checker's Cleanup fires exactly once.
// Before the fix the serial and parallel phases appended every checker
// unconditionally (and preflight a third time), so Cleanup ran 2x (3x for a
// preflight checker).
func TestCleanupEnqueuedOncePerChecker(t *testing.T) {
	tests := []struct {
		name string
		mode checkerPkg.ExecutionMode
	}{
		{name: "serial checker", mode: checkerPkg.Serial},
		{name: "parallel checker", mode: checkerPkg.Parallel},
		{name: "preflight checker", mode: checkerPkg.Preflight},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var count int64
			c := &countingChecker{name: "c", mode: tt.mode, cleanups: &count, mu: &sync.Mutex{}}
			all := []checkerPkg.Checker{c}
			r := NewRunner(&projectinfo.UserInput{}, "test", all)
			p := &projectinfo.ProjectInfo{UserInput: &projectinfo.UserInput{}, CheckerStatus: map[string]bool{}}
			events := &syncmap.Map[string, []*checkerPkg.Report]{}

			var queue []cleanupFn
			pq, _ := r.executePreflightCheckers(all, p, events)
			queue = append(queue, pq...)
			queue = append(queue, r.executeSerialCheckers(all, p, events)...)
			queue = append(queue, r.executeParallelCheckers(all, p, events)...)

			// Run cleanup exactly as runner.Run's deferred loop does.
			for _, cf := range queue {
				if err := cf.Cleanup(p); err != nil {
					t.Fatalf("cleanup error: %v", err)
				}
			}

			if count != 1 {
				t.Errorf("Cleanup ran %d times for a %s, want exactly 1", count, tt.name)
			}
		})
	}
}

// TestRecordNotApplicableCheckers pins the rule that a check is stamped as a
// vacuous pass ONLY when it was never eligible for the PR. The critical case is an
// eligible checker that produced no result (a preflight failure skips the heavy
// phases; a phase can also time out or panic): it must stay absent from the stamp,
// never be flipped to passed — otherwise the gate would clear an unrun required
// check and erase an earlier real failure.
func TestRecordNotApplicableCheckers(t *testing.T) {
	mk := func(name string) *countingChecker { return &countingChecker{name: name} }

	tests := []struct {
		name       string
		checkers   []checkerPkg.Checker
		eligible   []checkerPkg.Checker
		selective  []string
		disabled   []string
		initial    map[string]bool
		wantStatus map[string]bool
	}{
		{
			name:       "not-applicable checker is recorded as passed",
			checkers:   []checkerPkg.Checker{mk("a"), mk("b")},
			eligible:   []checkerPkg.Checker{mk("a")},
			initial:    map[string]bool{"a": true},
			wantStatus: map[string]bool{"a": true, "b": true},
		},
		{
			// Regression guard: "heavy" is eligible but produced no result (preflight
			// failure skipped it). It must NOT be stamped true.
			name:       "eligible-but-incomplete checker stays absent",
			checkers:   []checkerPkg.Checker{mk("preflight"), mk("heavy")},
			eligible:   []checkerPkg.Checker{mk("preflight"), mk("heavy")},
			initial:    map[string]bool{"preflight": false},
			wantStatus: map[string]bool{"preflight": false},
		},
		{
			name:       "disabled checker is not stamped",
			checkers:   []checkerPkg.Checker{mk("a")},
			eligible:   nil,
			disabled:   []string{"a"},
			initial:    map[string]bool{},
			wantStatus: map[string]bool{},
		},
		{
			name:       "under --enable only selected checkers are stamped",
			checkers:   []checkerPkg.Checker{mk("a"), mk("b")},
			eligible:   nil,
			selective:  []string{"a"},
			initial:    map[string]bool{},
			wantStatus: map[string]bool{"a": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Runner{checkers: tt.checkers}
			p := &projectinfo.ProjectInfo{
				UserInput: &projectinfo.UserInput{
					SelectiveWorkflows: tt.selective,
					DisabledWorkflows:  tt.disabled,
				},
				CheckerStatus: tt.initial,
			}
			r.recordNotApplicableCheckers(p, tt.eligible)
			if !reflect.DeepEqual(p.CheckerStatus, tt.wantStatus) {
				t.Errorf("CheckerStatus = %v, want %v", p.CheckerStatus, tt.wantStatus)
			}
		})
	}
}
