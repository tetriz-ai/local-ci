// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tetriz-ai/local-ci/projectinfo"
)

func TestGolangciLintArgs(t *testing.T) {
	tests := []struct {
		name  string
		extra []string
		want  []string
	}{
		{name: "no extra args", extra: nil, want: []string{"run"}},
		{name: "scoped extra args", extra: []string{"--new-from-merge-base=origin/main"}, want: []string{"run", "--new-from-merge-base=origin/main"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := golangciLintArgs(tt.extra); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("golangciLintArgs(%v) = %v, want %v", tt.extra, got, tt.want)
			}
		})
	}
}

func TestGolangciLintChecker_WithExtraArgs(t *testing.T) {
	tests := []struct {
		name string
		opts []GolangciLintOpt
		want []string
	}{
		{name: "default", opts: nil, want: []string{"run"}},
		{name: "configured", opts: []GolangciLintOpt{WithExtraArgs([]string{"--timeout=5m"})}, want: []string{"run", "--timeout=5m"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewGolangciLintChecker(tt.opts...)
			if got := golangciLintArgs(c.extraArgs); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("golangciLintArgs(extraArgs) = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGolangciLintChecker_GetFileFilter(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{name: "go sources and golangci config globs", want: []string{
			"**.go",
			"**/.golangci.yml",
			"**/.golangci.yaml",
			"**/.golangci.toml",
			"**/.golangci.json",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewGolangciLintChecker().GetFileFilter(nil); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetFileFilter() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Fail-closed: with golangci-lint absent the check must report Failure with the
// install message, never a silent Success that lets a PR ride through unlinted.
func TestGolangciLintChecker_FailsClosedWhenBinaryMissing(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "golangci-lint not on PATH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PATH", t.TempDir())
			p := &projectinfo.ProjectInfo{RepoPath: t.TempDir(), UserInput: &projectinfo.UserInput{}}
			got := NewGolangciLintChecker().Execute(p)
			if got[0].Status != Failure {
				t.Fatalf("Execute() = %v, want Failure when golangci-lint is missing", got[0].Status)
			}
			if got[0].Err == nil || !strings.Contains(got[0].Err.Error(), "golangci-lint not installed") {
				t.Errorf("Execute() err = %v, want the install message", got[0].Err)
			}
		})
	}
}

func TestGolangciLintChecker_ExecuteExitCodes(t *testing.T) {
	const finding = "main.go:1:1: some finding (govet)"
	const operational = "can't load config: unsupported version"
	tests := []struct {
		name          string
		script        string
		want          ExecutionStatus
		wantOutSubstr string
	}{
		{name: "exit 0 is success", script: "exit 0", want: Success},
		{name: "lint findings (exit 1) is failure with output surfaced", script: `echo "` + finding + `"; exit 1`, want: Failure, wantOutSubstr: finding},
		// An operational error (bad config, unbuildable code) exits 2+ and must also
		// fail — never be read as a pass. golangci-lint writes these to stderr.
		{name: "operational error (exit 3) is failure with output surfaced", script: `echo "` + operational + `" >&2; exit 3`, want: Failure, wantOutSubstr: operational},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeBin(t, "golangci-lint", tt.script)
			p := &projectinfo.ProjectInfo{RepoPath: t.TempDir(), UserInput: &projectinfo.UserInput{}}
			got := NewGolangciLintChecker().Execute(p)
			if got[0].Status != tt.want {
				t.Fatalf("Execute() = %v (err=%v), want %v", got[0].Status, got[0].Err, tt.want)
			}
			if tt.wantOutSubstr != "" {
				if got[0].Err == nil || !strings.Contains(got[0].Err.Error(), tt.wantOutSubstr) {
					t.Errorf("Execute() err = %v, want it to surface %q", got[0].Err, tt.wantOutSubstr)
				}
			}
		})
	}
}

func TestGolangciLintChecker_GetExecutionMode(t *testing.T) {
	tests := []struct {
		name string
		want ExecutionMode
	}{
		{name: "golangci-lint is parallel (does not mutate the tree)", want: Parallel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewGolangciLintChecker().GetExecutionMode(); got != tt.want {
				t.Errorf("GetExecutionMode() = %v, want %v", got, tt.want)
			}
		})
	}
}
