// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"os"
	"testing"

	"go.uber.org/zap"

	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

func TestMain(m *testing.M) {
	// Initialize a no-op logger so logStep calls don't panic.
	logger.Log = zap.NewNop()
	// Isolate HOME so a test that shells out to gh/git cannot reach the
	// developer's real credentials or config. No checker test talks to a live
	// GitHub; this is the backstop that enforces it.
	tmp, err := os.MkdirTemp("", "checker-test-home-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("HOME", tmp)
	_ = os.Setenv("USERPROFILE", tmp)
	_ = os.Setenv("GH_CONFIG_DIR", tmp)
	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

func TestIssueLinker_ClosingKeywordRegex(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		match bool
	}{
		{name: "Closes #123", body: "Some description\n\nCloses #123", match: true},
		{name: "closes #456 lowercase", body: "closes #456", match: true},
		{name: "Fixes #789", body: "Fixes #789\nmore text", match: true},
		{name: "fix #10 bare keyword", body: "This fix #10 completely", match: true},
		{name: "fixes #10", body: "This fixes #10 completely", match: true},
		{name: "Fixed #11", body: "Fixed #11", match: true},
		{name: "Resolves #100", body: "Resolves #100", match: true},
		{name: "resolved #200", body: "resolved #200", match: true},
		{name: "CLOSES #999 all caps", body: "CLOSES #999", match: true},
		{name: "Closes: #123 with colon", body: "Closes: #123", match: true},
		{name: "Fixes: #456 with colon", body: "Fixes: #456", match: true},
		{name: "no issue reference", body: "Just a description with no issue", match: false},
		{name: "hash without keyword", body: "See #123 for details", match: false},
		{name: "empty body", body: "", match: false},
		{name: "unresolved #4 is not resolve", body: "unresolved #4", match: false},
		{name: "discloses #5 is not close", body: "discloses #5", match: false},
		{name: "postfixes #1 is not fix", body: "postfixes #1", match: false},
		{name: "closes #7 at start", body: "closes #7", match: true},
		{name: "Closes#1 no space", body: "Closes#1", match: false},
		{name: "closesx #1 stem suffix", body: "closesx #1", match: false},
		{name: "encloses #2 is not close", body: "encloses #2", match: false},
		{name: "Resolves: #1 with colon", body: "Resolves: #1", match: true},
		{name: "done. Closes #1 after punctuation", body: "done. Closes #1", match: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := closingKeywordRe.MatchString(tt.body)
			if got != tt.match {
				t.Errorf("closingKeywordRe.MatchString(%q) = %v, want %v", tt.body, got, tt.match)
			}
		})
	}
}

func TestIssueLinker_ClosingKeywordURLRegex(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		match bool
	}{
		{
			name:  "Closes with full URL",
			body:  "Closes https://github.com/example-org/example-repo/issues/123",
			match: true,
		},
		{
			name:  "Fixes with full URL",
			body:  "Fixes https://github.com/example-org/example-repo/issues/456",
			match: true,
		},
		{
			name:  "Resolves with full URL",
			body:  "Resolves https://github.com/example-org/example-repo/issues/789",
			match: true,
		},
		{
			name:  "closes lowercase with URL",
			body:  "closes https://github.com/example-org/example-repo/issues/10",
			match: true,
		},
		{
			name:  "Closes: with colon and URL",
			body:  "Closes: https://github.com/example-org/example-repo/issues/11",
			match: true,
		},
		{
			name:  "URL in surrounding text with keyword",
			body:  "This PR fixes https://github.com/example-org/example-repo/issues/456 and more",
			match: true,
		},
		{
			name:  "bare URL without keyword",
			body:  "https://github.com/example-org/example-repo/issues/123",
			match: false,
		},
		{
			name:  "URL with non-closing prefix",
			body:  "Related to https://github.com/example-org/example-repo/issues/456",
			match: false,
		},
		{
			name:  "any repo issue URL with keyword matches",
			body:  "Closes https://github.com/other-org/other-repo/issues/123",
			match: true,
		},
		{
			name:  "PR URL with keyword",
			body:  "Closes https://github.com/example-org/example-repo/pull/123",
			match: false,
		},
		{
			name:  "no URL",
			body:  "Just text",
			match: false,
		},
		{
			name:  "discloses URL is not close",
			body:  "discloses https://github.com/example-org/example-repo/issues/1",
			match: false,
		},
		{
			name:  "encloses URL is not close",
			body:  "encloses https://github.com/example-org/example-repo/issues/1",
			match: false,
		},
		{
			name:  "keyword with no space before URL",
			body:  "Closeshttps://github.com/example-org/example-repo/issues/1",
			match: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := closingKeywordURLRe.MatchString(tt.body)
			if got != tt.match {
				t.Errorf("closingKeywordURLRe.MatchString(%q) = %v, want %v", tt.body, got, tt.match)
			}
		})
	}
}

func TestIssueLinker_BareURLDetection(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		match bool
	}{
		{
			name:  "bare URL detected",
			body:  "https://github.com/example-org/example-repo/issues/123",
			match: true,
		},
		{
			name:  "URL with keyword still detected as bare",
			body:  "Closes https://github.com/example-org/example-repo/issues/123",
			match: true,
		},
		{
			name:  "any repo bare issue URL is detected",
			body:  "https://github.com/other-org/other-repo/issues/123",
			match: true,
		},
		{
			name:  "no URL",
			body:  "Just text",
			match: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bareIssueURLRe.MatchString(tt.body)
			if got != tt.match {
				t.Errorf("bareIssueURLRe.MatchString(%q) = %v, want %v", tt.body, got, tt.match)
			}
		})
	}
}

// The checker enforces on every PR regardless of base branch; when gh is not
// installed it skips gracefully rather than failing a developer who cannot run
// the check. (Enforcement on a given base is covered by TestIssueLinker_EnforcesOnAnyBase.)
func TestIssueLinker_SkipsWhenGhMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // a PATH with no gh
	c := &issueLinkerChecker{}
	p := &projectinfo.ProjectInfo{
		PrData:    &projectinfo.PrData{BaseBranch: "release/v1.0"},
		UserInput: &projectinfo.UserInput{PrNumber: 1},
	}

	reports := c.Execute(p)
	if len(reports) != 1 || reports[0].Status != Success {
		t.Errorf("expected graceful success when gh is missing, got %v", reports)
	}
}
