// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tetriz-ai/local-ci/internal/globmatcher"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

func TestProtoBreakingChecker_Getters(t *testing.T) {
	t.Parallel()
	c := NewProtoBreakingChecker()
	if got := c.GetName(); got != ProtoBreakingWFName {
		t.Errorf("GetName() = %q, want %q", got, ProtoBreakingWFName)
	}
	if got := c.GetLciStampKey(); got != ProtoBreakingWFName {
		t.Errorf("GetLciStampKey() = %q, want %q", got, ProtoBreakingWFName)
	}
	wfNames := c.GetWorkflowNames()
	if len(wfNames) != 1 || wfNames[0] != ProtoBreakingWFName {
		t.Errorf("GetWorkflowNames() = %v, want [%q]", wfNames, ProtoBreakingWFName)
	}
	if got := c.GetExecutionMode(); got != Serial {
		t.Errorf("GetExecutionMode() = %v, want Serial", got)
	}
}

func TestProtoBreakingChecker_GetFileFilter(t *testing.T) {
	t.Parallel()
	c := NewProtoBreakingChecker()
	patterns := c.GetFileFilter(&projectinfo.ProjectInfo{RepoName: "repo"})
	tests := []struct {
		name  string
		files []string
		want  bool
	}{
		{name: "proto file triggers", files: []string{"repo/protos/x.proto"}, want: true},
		{name: "unrelated file does not trigger", files: []string{"repo/backend/x/y.go"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := globmatcher.Matches(tt.files, patterns); got != tt.want {
				t.Errorf("Matches(%v) = %v, want %v", tt.files, got, tt.want)
			}
		})
	}
}

// TestProtoBreakingChecker_Skips asserts the checker no-ops (passes) when there
// is nothing to compare — no PR base, or no proto in the diff — rather than
// erroring.
func TestProtoBreakingChecker_Skips(t *testing.T) {
	t.Parallel()
	c := NewProtoBreakingChecker()
	tests := []struct {
		name string
		info *projectinfo.ProjectInfo
	}{
		{name: "no PR base branch", info: &projectinfo.ProjectInfo{}},
		{
			name: "no proto files changed",
			info: &projectinfo.ProjectInfo{
				PrData: &projectinfo.PrData{BaseBranch: "master"},
				DiffData: &projectinfo.DiffData{OverallFilesChanges: &projectinfo.AffectedFiles{
					ModifiedFiles:   []string{"backend/x/y.go"},
					NonRemovedFiles: []string{"backend/x/y.go"},
				}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reports := c.Execute(tt.info)
			if len(reports) != 1 || reports[0].Status != Success {
				t.Errorf("Execute() = %+v, want a single Success (skip)", reports)
			}
		})
	}
}

// TestProtoBreakingChecker_FailsWhenBufMissing proves the fail-closed fix: on a proto PR
// (proto files changed) with no buf CLI, the mandatory checker must FAIL, not skip — a skip
// would stamp success and let a breaking change through on any setup without buf. Not
// parallel: it clears PATH via t.Setenv, which restores before the package's parallel tests
// run.
func TestProtoBreakingChecker_FailsWhenBufMissing(t *testing.T) {
	t.Setenv("PATH", "") // no buf (nor git) resolvable; the buf lookup is reached first
	c := NewProtoBreakingChecker()
	info := &projectinfo.ProjectInfo{
		PrData: &projectinfo.PrData{BaseBranch: "master"},
		DiffData: &projectinfo.DiffData{OverallFilesChanges: &projectinfo.AffectedFiles{
			ModifiedFiles:   []string{"protos/example/x/v1/x.proto"},
			NonRemovedFiles: []string{"protos/example/x/v1/x.proto"},
		}},
	}
	reports := c.Execute(info)
	if len(reports) != 1 || reports[0].Status != Failure {
		t.Errorf("Execute() = %+v, want a single Failure (a proto PR without buf must not skip)", reports)
	}
}

// TestRunBufBreaking is the acceptance proof: against a committed baseline, a
// field-key enum rename and a stored-enum renumber each fail the gate, while
// adding a new enum value passes. Skips when buf or git is unavailable.
func TestRunBufBreaking(t *testing.T) {
	if _, err := exec.LookPath("buf"); err != nil {
		t.Skipf("buf CLI not installed: %v", err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not installed: %v", err)
	}

	const baseProto = `syntax = "proto3";
package test.v1;
enum ProfileFieldKey {
  PROFILE_FIELD_KEY_UNSPECIFIED = 0;
  PROFILE_FIELD_KEY_USAGE_MODEL_ADOPTION = 1;
}
`
	protoRel := filepath.Join("protos", "test", "v1", "test.proto")

	tests := []struct {
		name         string
		mutated      string // written to the proto working tree; ignored when deleteProto
		deleteProto  bool
		wantBreaking bool
	}{
		{
			name: "rename a field-key enum value fails",
			mutated: `syntax = "proto3";
package test.v1;
enum ProfileFieldKey {
  PROFILE_FIELD_KEY_UNSPECIFIED = 0;
  PROFILE_FIELD_KEY_USAGE_MODEL_USE = 1;
}
`,
			wantBreaking: true,
		},
		{
			name: "renumber a stored enum value fails",
			mutated: `syntax = "proto3";
package test.v1;
enum ProfileFieldKey {
  PROFILE_FIELD_KEY_UNSPECIFIED = 0;
  PROFILE_FIELD_KEY_USAGE_MODEL_ADOPTION = 5;
}
`,
			wantBreaking: true,
		},
		{
			// Regression guard: whole-module compare (no --path scoping) is what
			// catches a deleted proto, which no surviving path could name.
			name:         "deleting a proto file fails",
			deleteProto:  true,
			wantBreaking: true,
		},
		{
			name: "adding a new enum value passes",
			mutated: `syntax = "proto3";
package test.v1;
enum ProfileFieldKey {
  PROFILE_FIELD_KEY_UNSPECIFIED = 0;
  PROFILE_FIELD_KEY_USAGE_MODEL_ADOPTION = 1;
  PROFILE_FIELD_KEY_USAGE_IDE_MIX = 2;
}
`,
			wantBreaking: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			baseSHA := seedProtoRepo(t, repo, protoRel, baseProto)
			// Apply the mutation to the working tree (uncommitted) so buf compares
			// current-vs-baseline.
			if tt.deleteProto {
				if err := os.Remove(filepath.Join(repo, protoRel)); err != nil {
					t.Fatalf("removing proto: %v", err)
				}
			} else {
				writeFile(t, filepath.Join(repo, protoRel), tt.mutated)
			}

			violations, err := runBufBreaking(repo, baseSHA)
			if err != nil {
				t.Fatalf("runBufBreaking() error = %v", err)
			}
			if got := len(violations) > 0; got != tt.wantBreaking {
				t.Errorf("runBufBreaking() breaking = %v (violations=%v), want %v", got, violations, tt.wantBreaking)
			}
		})
	}
}

// TestRunBufBreaking_PinsFilePolicy is the regression guard for a gaming
// vector: a PR that downgrades buf.yaml's breaking policy in the same change
// must not slip a rename past the gate. The baseline commits the FILE policy;
// the working tree downgrades it to the more-permissive WIRE (under which an
// enum-value rename is not breaking) and renames the value. The gate must still
// flag it, because it pins FILE via --config rather than trusting the tree.
func TestRunBufBreaking_PinsFilePolicy(t *testing.T) {
	if _, err := exec.LookPath("buf"); err != nil {
		t.Skipf("buf CLI not installed: %v", err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not installed: %v", err)
	}
	protoRel := filepath.Join("protos", "test", "v1", "test.proto")
	repo := t.TempDir()
	baseSHA := seedProtoRepo(t, repo, protoRel, `syntax = "proto3";
package test.v1;
enum ProfileFieldKey {
  PROFILE_FIELD_KEY_UNSPECIFIED = 0;
  PROFILE_FIELD_KEY_USAGE_MODEL_ADOPTION = 1;
}
`)
	// Working tree: downgrade the policy AND rename the enum value.
	writeFile(t, filepath.Join(repo, "buf.yaml"), `version: v2
modules:
  - path: protos
breaking:
  use:
    - WIRE
`)
	writeFile(t, filepath.Join(repo, protoRel), `syntax = "proto3";
package test.v1;
enum ProfileFieldKey {
  PROFILE_FIELD_KEY_UNSPECIFIED = 0;
  PROFILE_FIELD_KEY_USAGE_MODEL_USE = 1;
}
`)
	violations, err := runBufBreaking(repo, baseSHA)
	if err != nil {
		t.Fatalf("runBufBreaking() error = %v", err)
	}
	if len(violations) == 0 {
		t.Error("a rename must still fail even when the PR downgrades buf.yaml to WIRE — the gate pins FILE")
	}
}

// TestRunBufBreaking_OperationalErrorFailsLoud is the regression guard for the
// fail-open bug: an unresolvable --against ref must return an error (failing the
// gate), never an empty result that a caller reads as "no breaking change".
func TestRunBufBreaking_OperationalErrorFailsLoud(t *testing.T) {
	if _, err := exec.LookPath("buf"); err != nil {
		t.Skipf("buf CLI not installed: %v", err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not installed: %v", err)
	}
	repo := t.TempDir()
	seedProtoRepo(t, repo, filepath.Join("protos", "test", "v1", "test.proto"), `syntax = "proto3";
package test.v1;
enum ProfileFieldKey { PROFILE_FIELD_KEY_UNSPECIFIED = 0; }
`)
	// A ref that does not resolve is an operational failure, not a clean run.
	_, err := runBufBreaking(repo, "0000000000000000000000000000000000000000")
	if err == nil {
		t.Error("an unresolvable --against ref must fail the gate, not pass silently")
	} else {
		t.Logf("operational failure surfaced as expected: %v", err)
	}
}

// TestFilterAllowedPresenceWidenings_RealBufPhrasing binds the filter's phrase
// constants to buf's actual violation wording by running real buf over a
// seeded repo: if buf ever rewords the cardinality message, the widening stops
// being filtered and the case fails loud — the carve-out can only fail closed.
func TestFilterAllowedPresenceWidenings_RealBufPhrasing(t *testing.T) {
	if _, err := exec.LookPath("buf"); err != nil {
		t.Skipf("buf CLI not installed: %v", err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not installed: %v", err)
	}
	protoRel := filepath.Join("protos", "test", "v1", "test.proto")

	tests := []struct {
		name         string
		base         string
		mutated      string
		wantFiltered bool // true: the filter drops every violation; false: all survive
	}{
		{
			name: "scalar gaining optional is filtered (wire-compatible widening)",
			base: `syntax = "proto3";
package test.v1;
message TurnUsage {
  double context_usage_percent = 1;
}
`,
			mutated: `syntax = "proto3";
package test.v1;
message TurnUsage {
  optional double context_usage_percent = 1;
}
`,
			wantFiltered: true,
		},
		{
			name: "optional losing presence stays a hard failure",
			base: `syntax = "proto3";
package test.v1;
message TurnUsage {
  optional double context_usage_percent = 1;
}
`,
			mutated: `syntax = "proto3";
package test.v1;
message TurnUsage {
  double context_usage_percent = 1;
}
`,
			wantFiltered: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			baseSHA := seedProtoRepo(t, repo, protoRel, tt.base)
			writeFile(t, filepath.Join(repo, protoRel), tt.mutated)

			violations, err := runBufBreaking(repo, baseSHA)
			if err != nil {
				t.Fatalf("runBufBreaking() error = %v", err)
			}
			if len(violations) == 0 {
				t.Fatalf("expected buf to flag the cardinality change, got none")
			}
			remaining := (&protoBreakingChecker{}).filterAllowedPresenceWidenings(violations)
			if tt.wantFiltered && len(remaining) != 0 {
				t.Errorf("widening not filtered — buf's phrasing may have changed: %v", remaining)
			}
			if !tt.wantFiltered && len(remaining) != len(violations) {
				t.Errorf("filter dropped a non-widening violation: kept %d of %d (%v)", len(remaining), len(violations), violations)
			}
		})
	}
}

// TestFilterAllowedPresenceWidenings_Synthetic proves the filter never
// over-matches: only the exact implicit→explicit presence pair is dropped.
func TestFilterAllowedPresenceWidenings_Synthetic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		violation string
		wantKept  bool
	}{
		{
			name:      "implicit to explicit presence is dropped",
			violation: `protos/a.proto:10:3:Field "6" with name "pct" on message "M" changed cardinality from "optional with implicit presence" to "optional with explicit presence".`,
			wantKept:  false,
		},
		{
			name:      "explicit to implicit presence is kept",
			violation: `protos/a.proto:10:3:Field "6" with name "pct" on message "M" changed cardinality from "optional with explicit presence" to "optional with implicit presence".`,
			wantKept:  true,
		},
		{
			name:      "repeated to explicit presence is kept",
			violation: `protos/a.proto:10:3:Field "6" with name "pct" on message "M" changed cardinality from "repeated" to "optional with explicit presence".`,
			wantKept:  true,
		},
		{
			name:      "implicit presence to repeated is kept",
			violation: `protos/a.proto:10:3:Field "6" with name "pct" on message "M" changed cardinality from "optional with implicit presence" to "repeated".`,
			wantKept:  true,
		},
		{
			name:      "non-cardinality violation mentioning presence is kept",
			violation: `protos/a.proto:10:3:Field "6" was deleted from message "M" (had optional with implicit presence, to "optional with explicit presence" unrelated).`,
			wantKept:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			remaining := (&protoBreakingChecker{}).filterAllowedPresenceWidenings([]string{tt.violation})
			if got := len(remaining) == 1; got != tt.wantKept {
				t.Errorf("kept = %v, want %v (violation: %s)", got, tt.wantKept, tt.violation)
			}
		})
	}
}

// TestParseDeletedFromFileViolation pins the parse to buf's top-level
// "deleted from file" phrasing, and — critically — proves a nested-type deletion
// ("deleted from message") and every non-deletion violation are NOT parsed as
// relocatable, so they can never be dropped by the same-package carve-out.
func TestParseDeletedFromFileViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		line     string
		wantOK   bool
		wantName string
		wantPath string
	}{
		{
			name:     "top-level enum deleted",
			line:     `protos/a/v1/x.proto:1:1:Previously present enum "Fidelity" was deleted from file.`,
			wantOK:   true,
			wantName: "Fidelity",
			wantPath: "protos/a/v1/x.proto",
		},
		{
			name:     "top-level message deleted",
			line:     `protos/a/v1/x.proto:3:1:Previously present message "Foo" was deleted from file.`,
			wantOK:   true,
			wantName: "Foo",
			wantPath: "protos/a/v1/x.proto",
		},
		{
			name: "nested enum deleted from a message is not matched",
			line: `protos/a/v1/x.proto:5:3:Previously present enum "Inner" was deleted from message "Outer".`,
		},
		{
			name: "cardinality change is not matched",
			line: `protos/a/v1/x.proto:9:3:Field "1" with name "c" on message "M" changed cardinality from "optional with implicit presence" to "optional with explicit presence".`,
		},
		{
			name: "field deletion is not matched",
			line: `protos/a/v1/x.proto:9:3:Previously present field "1" with name "c" on message "M" was deleted.`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			name, path, ok := parseDeletedFromFileViolation(tt.line)
			if ok != tt.wantOK || name != tt.wantName || path != tt.wantPath {
				t.Errorf("parseDeletedFromFileViolation(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.line, name, path, ok, tt.wantName, tt.wantPath, tt.wantOK)
			}
		})
	}
}

// TestDropSamePackageRelocations proves the decision against a hand-built image
// index: a type still present in the LOSING file's package is a relocation
// (dropped); a type gone from the image, a type that reappeared in a DIFFERENT
// package, an unmatched losing file, and a non-deletion violation all stay.
func TestDropSamePackageRelocations(t *testing.T) {
	t.Parallel()
	idx := &protoImageIndex{
		pkgTypes: map[string]map[string]bool{
			"test.v1":       {"Fidelity": true, "KeepAlive": true},
			"test.other.v1": {"Elsewhere": true},
		},
		fileToPkg: map[string]string{
			"test/v1/x.proto":       "test.v1",
			"test/v1/keep.proto":    "test.v1",
			"test/other/v1/o.proto": "test.other.v1",
		},
	}
	del := func(path, name string) string {
		return path + `:1:1:Previously present enum "` + name + `" was deleted from file.`
	}
	tests := []struct {
		name      string
		violation string
		wantKept  bool
	}{
		{"same-package relocation is dropped", del("protos/test/v1/x.proto", "Fidelity"), false},
		{"true deletion (type gone from image) is kept", del("protos/test/v1/x.proto", "Ghost"), true},
		{"reappearance in a different package is kept", del("protos/test/v1/x.proto", "Elsewhere"), true},
		{"losing file with no package match is kept", del("protos/unknown/v1/z.proto", "Fidelity"), true},
		{"non-deletion violation is kept", `protos/test/v1/x.proto:2:3:Field "1" with name "c" on message "M" changed type.`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kept, allowed := (&protoBreakingChecker{}).dropSamePackageRelocations([]string{tt.violation}, idx)
			gotKept := len(kept) == 1
			if gotKept != tt.wantKept {
				t.Errorf("kept = %v, want %v (violation: %s)", gotKept, tt.wantKept, tt.violation)
			}
			wantAllowed := 0
			if !tt.wantKept {
				wantAllowed = 1
			}
			if allowed != wantAllowed {
				t.Errorf("allowed = %d, want %d", allowed, wantAllowed)
			}
		})
	}
}

// TestFilterAllowedSamePackageRelocations_RealBuf runs real buf end to end: it
// moves an enum to a sibling file in the same package and confirms the resulting
// deletion violation is dropped, while an outright deletion (no sibling re-adds
// it) stays a hard failure — binding the carve-out to buf's actual image + text.
func TestFilterAllowedSamePackageRelocations_RealBuf(t *testing.T) {
	if _, err := exec.LookPath("buf"); err != nil {
		t.Skipf("buf CLI not installed: %v", err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not installed: %v", err)
	}
	protoRel := filepath.Join("protos", "test", "v1", "test.proto")
	keepRel := filepath.Join("protos", "test", "v1", "keep.proto")

	const baseWithEnum = `syntax = "proto3";
package test.v1;
enum Fidelity {
  FIDELITY_UNSPECIFIED = 0;
  FIDELITY_MEASURED = 1;
}
`
	const emptyTest = `syntax = "proto3";
package test.v1;
`
	const keepWithEnum = `syntax = "proto3";
package test.v1;
message KeepAlive {}
enum Fidelity {
  FIDELITY_UNSPECIFIED = 0;
  FIDELITY_MEASURED = 1;
}
`
	const keepWithoutEnum = `syntax = "proto3";
package test.v1;
message KeepAlive {}
`
	tests := []struct {
		name        string
		mutateKeep  string
		wantAllowed bool // true: the deletion is a relocation and gets dropped
	}{
		{"enum moved to a sibling file in the same package", keepWithEnum, true},
		{"enum deleted outright stays a hard failure", keepWithoutEnum, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			baseSHA := seedProtoRepo(t, repo, protoRel, baseWithEnum)
			writeFile(t, filepath.Join(repo, protoRel), emptyTest)
			writeFile(t, filepath.Join(repo, keepRel), tt.mutateKeep)

			violations, err := runBufBreaking(repo, baseSHA)
			if err != nil {
				t.Fatalf("runBufBreaking() error = %v", err)
			}
			if len(violations) == 0 {
				t.Fatalf("expected buf to flag the enum deletion, got none")
			}
			kept, allowed, err := (&protoBreakingChecker{}).filterAllowedSamePackageRelocations(repo, violations)
			if err != nil {
				t.Fatalf("filterAllowedSamePackageRelocations() error = %v", err)
			}
			if tt.wantAllowed {
				if len(kept) != 0 {
					t.Errorf("relocation not filtered: kept %v", kept)
				}
				if allowed == 0 {
					t.Errorf("expected at least one allowed relocation, got 0")
				}
			} else {
				if len(kept) != len(violations) {
					t.Errorf("true deletion was dropped: kept %d of %d (%v)", len(kept), len(violations), violations)
				}
				if allowed != 0 {
					t.Errorf("expected 0 allowed, got %d", allowed)
				}
			}
		})
	}
}

// TestFilterAllowedSamePackageRelocations_RealBufHardFailures pins buf's phrasing
// for two shapes that must NEVER be read as a same-package relocation: a
// whole-file deletion and a file's package rename. buf reports the first as a
// file-level "...was deleted." line (not a per-type "deleted from file" line, so
// no decoy in a path-colliding sibling can suppress it) and the second as "File
// package changed" — neither is parsed as a relocatable deletion, so both stay
// hard failures. If a future buf version reworded either into a parseable
// "deleted from file" line, this guard fails (allowed>0) rather than opening the
// gate — the empirical backstop behind the assumption the carve-out rests on.
func TestFilterAllowedSamePackageRelocations_RealBufHardFailures(t *testing.T) {
	if _, err := exec.LookPath("buf"); err != nil {
		t.Skipf("buf CLI not installed: %v", err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not installed: %v", err)
	}
	protoRel := filepath.Join("protos", "test", "v1", "test.proto")
	const base = `syntax = "proto3";
package test.v1;
enum Fidelity {
  FIDELITY_UNSPECIFIED = 0;
}
`
	tests := []struct {
		name   string
		mutate func(t *testing.T, repo string)
	}{
		{
			name: "whole-file deletion is never read as a relocation",
			mutate: func(t *testing.T, repo string) {
				if err := os.Remove(filepath.Join(repo, protoRel)); err != nil {
					t.Fatalf("remove %s: %v", protoRel, err)
				}
			},
		},
		{
			name: "file package rename is never read as a relocation",
			mutate: func(t *testing.T, repo string) {
				writeFile(t, filepath.Join(repo, protoRel), `syntax = "proto3";
package test.renamed.v1;
enum Fidelity {
  FIDELITY_UNSPECIFIED = 0;
}
`)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			baseSHA := seedProtoRepo(t, repo, protoRel, base)
			tt.mutate(t, repo)

			violations, err := runBufBreaking(repo, baseSHA)
			if err != nil {
				t.Fatalf("runBufBreaking() error = %v", err)
			}
			if len(violations) == 0 {
				t.Fatalf("expected buf to report a breaking change, got none")
			}
			kept, allowed, err := (&protoBreakingChecker{}).filterAllowedSamePackageRelocations(repo, violations)
			if err != nil {
				t.Fatalf("filterAllowedSamePackageRelocations() error = %v", err)
			}
			if allowed != 0 {
				t.Errorf("a hard failure was mistaken for a relocation (allowed %d): %v", allowed, violations)
			}
			if len(kept) != len(violations) {
				t.Errorf("filter dropped a violation: kept %d of %d (%v)", len(kept), len(violations), violations)
			}
		})
	}
}

// seedProtoRepo initialises a git repo with a v2 buf.yaml (FILE breaking policy)
// and one committed proto, returning the baseline commit SHA.
func seedProtoRepo(t *testing.T, repo, protoRel, protoBody string) string {
	t.Helper()
	writeFile(t, filepath.Join(repo, "buf.yaml"), `version: v2
modules:
  - path: protos
lint:
  use:
    - STANDARD
breaking:
  use:
    - FILE
`)
	writeFile(t, filepath.Join(repo, protoRel), protoBody)
	// A second, untouched proto keeps the module non-empty when a test deletes
	// protoRel — so buf reports the deletion as a FILE breaking change rather
	// than erroring that the module has no .proto files.
	writeFile(t, filepath.Join(repo, "protos", "test", "v1", "keep.proto"), `syntax = "proto3";
package test.v1;
message KeepAlive {}
`)

	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "t@t.test")
	runGit(t, repo, "config", "user.name", "t")
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-q", "-m", "baseline")
	return strings.TrimSpace(runGit(t, repo, "rev-parse", "HEAD"))
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
