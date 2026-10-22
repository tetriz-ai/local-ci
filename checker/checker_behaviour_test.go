// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tetriz-ai/local-ci/projectinfo"
)

// fakeBin writes an executable script named name into a fresh dir and prepends
// that dir to PATH for the test, so a checker that shells out to the command
// reaches this fake instead of the real tool.
func fakeBin(t *testing.T, name, script string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// issue_linker enforces on every PR, not only a "master" base. A PR with a
// "main" base and an issue-less body must fail. Before the fix the checker
// skipped any non-master base and reported Success.
func TestIssueLinker_EnforcesOnAnyBase(t *testing.T) {
	tests := []struct {
		name string
		body string
		want ExecutionStatus
	}{
		{name: "main base, no issue linked", body: "", want: Failure},
		{name: "main base, issue linked", body: "Closes #1", want: Success},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeBin(t, "gh", "printf '{\"body\":\""+tt.body+"\"}'")
			p := &projectinfo.ProjectInfo{
				RepoPath:  t.TempDir(),
				UserInput: &projectinfo.UserInput{PrNumber: 1},
				PrData:    &projectinfo.PrData{BaseBranch: "main"},
			}
			got := NewIssueLinkerChecker().Execute(p)
			if got[0].Status != tt.want {
				t.Errorf("Execute() = %v (err=%v), want %v", got[0].Status, got[0].Err, tt.want)
			}
		})
	}
}

// filterProtoFiles covers a .proto anywhere, not only under protos/. Before
// the fix a .proto outside protos/ was dropped, so both proto gates passed open.
func TestFilterProtoFiles_CoversAnyLocation(t *testing.T) {
	got := filterProtoFiles([]string{"deep/nested/x.proto", "protos/y.proto", "main.go"})
	if len(got) != 2 {
		t.Fatalf("filterProtoFiles returned %v, want both .proto files", got)
	}
	found := map[string]bool{}
	for _, f := range got {
		found[f] = true
	}
	if !found["deep/nested/x.proto"] {
		t.Errorf("a .proto outside protos/ was dropped: %v", got)
	}
}

// runBufLint fails loud when buf fails operationally (a non-violation exit),
// instead of reading empty stdout as "no violations". Before the fix the error
// from cmd.Output() was discarded and the gate passed open.
func TestRunBufLint_FailsLoudOnBufError(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "buf.yaml"), []byte("version: v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("operational failure (exit 1) is an error", func(t *testing.T) {
		fakeBin(t, "buf", "exit 1")
		if _, err := runBufLint(repo, []string{"a.proto"}); err == nil {
			t.Error("runBufLint returned nil error on a buf operational failure; want an error")
		}
	})

	t.Run("violations (exit 100) are data, not an error", func(t *testing.T) {
		fakeBin(t, "buf", `printf '{"path":"a.proto","start_line":1,"type":"COMMENT_MESSAGE","message":"x"}\n'; exit 100`)
		v, err := runBufLint(repo, []string{"a.proto"})
		if err != nil {
			t.Errorf("runBufLint returned error on a lint-violation exit: %v", err)
		}
		if len(v) == 0 {
			t.Error("runBufLint dropped the violations from exit-100 stdout")
		}
	})
}

// the govulncheck checker runs `govulncheck ./...` directly, not `make
// govulncheck`. With a govulncheck that passes and a make that fails, the check
// passes only if it calls govulncheck. Before the fix it called make and failed.
func TestGovulncheck_RunsGovulncheckDirectly(t *testing.T) {
	fakeBin(t, "govulncheck", "exit 0")
	// Shadow make with a failing one, so the old `make govulncheck` path cannot pass.
	dir := filepath.Dir(firstOnPath(t, "govulncheck"))
	if err := os.WriteFile(filepath.Join(dir, "make"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := &projectinfo.ProjectInfo{RepoPath: t.TempDir(), UserInput: &projectinfo.UserInput{}}
	got := NewGovulncheckChecker().Execute(p)
	if got[0].Status != Success {
		t.Errorf("Execute() = %v (err=%v), want Success (must run govulncheck ./..., not make)", got[0].Status, got[0].Err)
	}
}

// firstOnPath returns the resolved path of name using the test's PATH.
func firstOnPath(t *testing.T, name string) string {
	t.Helper()
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		cand := filepath.Join(dir, name)
		if info, err := os.Stat(cand); err == nil && !info.IsDir() {
			return cand
		}
	}
	t.Fatalf("%s not found on PATH", name)
	return ""
}
