// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prepper

import (
	"testing"

	"github.com/tetriz-ai/local-ci/projectinfo"
)

func TestValidateAffectedFilesShellSafe(t *testing.T) {
	tests := []struct {
		name    string
		af      *projectinfo.AffectedFiles
		wantErr bool
	}{
		{
			name:    "clean paths",
			af:      &projectinfo.AffectedFiles{ModifiedFiles: []string{"backend/foo/bar_repo.go", "web/src/x.tsx"}},
			wantErr: false,
		},
		{
			name:    "Next.js route groups",
			af:      &projectinfo.AffectedFiles{ModifiedFiles: []string{"web/src/app/(publication)/layout.tsx", "web/src/app/(dashboard)/(foo(bar))/page.tsx"}},
			wantErr: false,
		},
		{
			name:    "nil",
			af:      nil,
			wantErr: false,
		},
		{
			name:    "command substitution in a new filename",
			af:      &projectinfo.AffectedFiles{NewFiles: []string{"foo$(touch /tmp/pwn).go"}},
			wantErr: true,
		},
		{
			name:    "semicolon injection in a modified filename",
			af:      &projectinfo.AffectedFiles{ModifiedFiles: []string{"foo;rm -rf x.proto"}},
			wantErr: true,
		},
		{
			name:    "pipe in NonRemovedFiles",
			af:      &projectinfo.AffectedFiles{NonRemovedFiles: []string{"foo|sh.ts"}},
			wantErr: true,
		},
		{
			name:    "well-formed route group is allowed in any location",
			af:      &projectinfo.AffectedFiles{ModifiedFiles: []string{"backend/foo/(publication)/bar.go"}},
			wantErr: false,
		},
		{
			name:    "parenthesized filename in Next.js app tree",
			af:      &projectinfo.AffectedFiles{ModifiedFiles: []string{"web/src/app/(publication)"}},
			wantErr: true,
		},
		{
			name:    "partial route-group segment",
			af:      &projectinfo.AffectedFiles{ModifiedFiles: []string{"web/src/app/(publication/page.tsx"}},
			wantErr: true,
		},
		{
			name:    "route-group segment with command substitution",
			af:      &projectinfo.AffectedFiles{ModifiedFiles: []string{"web/src/app/($(id))/page.tsx"}},
			wantErr: true,
		},
		{
			// A space is allowed: changed-file paths reach commands only as verbatim
			// argv elements, so a space cannot split an argument or inject.
			name:    "space in filename is allowed",
			af:      &projectinfo.AffectedFiles{ModifiedFiles: []string{"web/src/app/(publication)/with space.tsx"}},
			wantErr: false,
		},
		{
			// A tab is still rejected: it breaks the line-based parsing of
			// `git diff --name-only`, not because it is a shell hazard.
			name:    "tab in filename is rejected",
			af:      &projectinfo.AffectedFiles{ModifiedFiles: []string{"web/src/app/with\ttab.tsx"}},
			wantErr: true,
		},
		{
			name:    "route-group path with quote",
			af:      &projectinfo.AffectedFiles{ModifiedFiles: []string{"web/src/app/(publication)/bad'name.tsx"}},
			wantErr: true,
		},
		{
			name:    "route-group path with redirection",
			af:      &projectinfo.AffectedFiles{ModifiedFiles: []string{"web/src/app/(publication)/bad>name.tsx"}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAffectedFilesShellSafe(tt.af)
			if tt.wantErr && err == nil {
				t.Errorf("validateAffectedFilesShellSafe(%+v) = nil, want error", tt.af)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateAffectedFilesShellSafe(%+v) = %v, want nil", tt.af, err)
			}
		})
	}
}

func TestValidateBranchRef(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		wantErr bool
	}{
		{name: "plain branch", ref: "fix/coding-metrics-2791", wantErr: false},
		{name: "branch with dots and underscores", ref: "release_v1.2.3", wantErr: false},
		{name: "master", ref: "master", wantErr: false},
		{name: "empty ref", ref: "", wantErr: true},
		{name: "parenthesized ref", ref: "fix/(publication)", wantErr: true},
		{name: "command substitution dollar-paren", ref: "foo$(id)", wantErr: true},
		{name: "command substitution backtick", ref: "foo`id`", wantErr: true},
		{name: "semicolon chain", ref: "foo;rm", wantErr: true},
		{name: "and chain", ref: "foo&&rm", wantErr: true},
		{name: "pipe", ref: "foo|sh", wantErr: true},
		{name: "single quote breakout", ref: "foo'; id; echo '", wantErr: true},
		{name: "redirect", ref: "foo>bar", wantErr: true},
		{name: "whitespace", ref: "foo bar", wantErr: true},
		{name: "newline", ref: "foo\nrm", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBranchRef("head", tt.ref)
			if tt.wantErr && err == nil {
				t.Errorf("validateBranchRef(%q) = nil, want error", tt.ref)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateBranchRef(%q) = %v, want nil", tt.ref, err)
			}
		})
	}
}
