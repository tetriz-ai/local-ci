// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tetriz-ai/local-ci/projectinfo"
)

func TestPnpmAuditArgs(t *testing.T) {
	tests := []struct {
		name  string
		level string
		want  []string
	}{
		{name: "default high", level: pnpmAuditDefaultLevel, want: []string{"audit", "--audit-level", "high"}},
		{name: "configured critical", level: "critical", want: []string{"audit", "--audit-level", "critical"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pnpmAuditArgs(tt.level); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("pnpmAuditArgs(%q) = %v, want %v", tt.level, got, tt.want)
			}
		})
	}
}

func TestPnpmAuditChecker_GetFileFilter(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{name: "location-generic manifest globs", want: []string{"**/package.json", "**/pnpm-lock.yaml"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewPnpmAuditChecker().GetFileFilter(nil); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetFileFilter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPnpmAuditChecker_InvalidLevelIsConfigError(t *testing.T) {
	tests := []struct {
		name  string
		level string
		want  ExecutionStatus
	}{
		{name: "invalid level fails as config error", level: "severe", want: Failure},
		{name: "valid level is accepted", level: "moderate", want: Success},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A passing pnpm isolates the config-error path from the audit itself.
			fakeBin(t, "pnpm", "exit 0")
			p := &projectinfo.ProjectInfo{RepoPath: t.TempDir(), UserInput: &projectinfo.UserInput{}}
			got := NewPnpmAuditChecker(WithPnpmAuditLevel(tt.level)).Execute(p)
			if got[0].Status != tt.want {
				t.Errorf("Execute() = %v (err=%v), want %v", got[0].Status, got[0].Err, tt.want)
			}
		})
	}
}

// Fail-closed: with pnpm absent from PATH the check must report Failure with the
// install message, never a silent Success that lets a PR ride through unscanned.
func TestPnpmAuditChecker_FailsClosedWhenPnpmMissing(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "pnpm not on PATH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Isolate PATH to an empty dir so no real pnpm is found.
			t.Setenv("PATH", t.TempDir())
			p := &projectinfo.ProjectInfo{RepoPath: t.TempDir(), UserInput: &projectinfo.UserInput{}}
			got := NewPnpmAuditChecker().Execute(p)
			if got[0].Status != Failure {
				t.Fatalf("Execute() = %v, want Failure when pnpm is missing", got[0].Status)
			}
			if got[0].Err == nil || !strings.Contains(got[0].Err.Error(), "pnpm not installed") {
				t.Errorf("Execute() err = %v, want the install message", got[0].Err)
			}
		})
	}
}

func TestPnpmAuditChecker_ExecuteExitCodes(t *testing.T) {
	const advisory = "1 high severity vulnerability (CVE-2026-0000)"
	tests := []struct {
		name          string
		script        string
		want          ExecutionStatus
		wantOutSubstr string
	}{
		{name: "pnpm exit 0 is success", script: "exit 0", want: Success},
		{name: "pnpm advisories (exit non-zero) is failure with output surfaced", script: `echo "` + advisory + `"; exit 1`, want: Failure, wantOutSubstr: advisory},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeBin(t, "pnpm", tt.script)
			p := &projectinfo.ProjectInfo{RepoPath: t.TempDir(), UserInput: &projectinfo.UserInput{}}
			got := NewPnpmAuditChecker().Execute(p)
			if got[0].Status != tt.want {
				t.Fatalf("Execute() = %v (err=%v), want %v", got[0].Status, got[0].Err, tt.want)
			}
			if tt.wantOutSubstr != "" {
				if got[0].Err == nil || !strings.Contains(got[0].Err.Error(), tt.wantOutSubstr) {
					t.Errorf("Execute() err = %v, want it to surface the advisory %q", got[0].Err, tt.wantOutSubstr)
				}
			}
		})
	}
}

// A failure to start pnpm for a reason other than a missing binary (here, a
// non-existent working directory) must NOT be mislabeled "pnpm not installed":
// pnpm is on PATH. The direction stays safe (still Failure); only the message
// must be the generic run-failure, not the install hint.
func TestPnpmAuditChecker_StartFailureNotMislabeledAsMissing(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "non-existent working dir"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeBin(t, "pnpm", "exit 0") // pnpm IS installed
			p := &projectinfo.ProjectInfo{RepoPath: t.TempDir(), UserInput: &projectinfo.UserInput{}}
			got := NewPnpmAuditChecker(WithPnpmAuditDir("does-not-exist")).Execute(p)
			if got[0].Status != Failure {
				t.Fatalf("Execute() = %v, want Failure", got[0].Status)
			}
			if got[0].Err != nil && strings.Contains(got[0].Err.Error(), "pnpm not installed") {
				t.Errorf("Execute() err = %v, must not claim pnpm is not installed when it is on PATH", got[0].Err)
			}
		})
	}
}

func TestPnpmAuditChecker_GetExecutionMode(t *testing.T) {
	tests := []struct {
		name string
		want ExecutionMode
	}{
		{name: "pnpm audit is parallel (does not mutate the tree)", want: Parallel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewPnpmAuditChecker().GetExecutionMode(); got != tt.want {
				t.Errorf("GetExecutionMode() = %v, want %v", got, tt.want)
			}
		})
	}
}
