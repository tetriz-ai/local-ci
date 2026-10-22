// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"gopkg.in/yaml.v3"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/pkg/cmdlogger"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

// ProtoBreakingWFName is the workflow name for this checker.
const ProtoBreakingWFName = "proto_breaking"

// bufBreakingExitCode is the exit code `buf breaking` uses when it finds
// breaking changes (as opposed to an operational failure, which exits with a
// different non-zero code and writes to stderr). Distinguishing the two is what
// keeps this gate from failing open on a buf malfunction.
const bufBreakingExitCode = 100

// bufBreakingTimeout bounds the `buf breaking` exec. This is a mandatory checker on
// every proto-touching PR across all teams, so an unbounded exec that stalls on
// dependency resolution (e.g. buf.build/protovalidate offline) would hang the gate for
// everyone. The bound is generous — the compare is against local git blobs at the
// merge-base, so a healthy run finishes in seconds; only a wedged network hits it.
const bufBreakingTimeout = 5 * time.Minute

// protoBreakingChecker runs `buf breaking` against the PR's merge base, so a
// wire-breaking proto change — a removed or renumbered field, a renamed or
// renumbered enum value — fails before it reaches CI. buf.yaml declares the
// FILE breaking policy but nothing ran it; this is that runner. It matters most
// for stored vocabularies whose format keys on enum names (the harness-profile
// field keys): a silent rename returns zero values for every historical row
// rather than failing, so the rename must fail here instead.
type protoBreakingChecker struct{}

func NewProtoBreakingChecker() *protoBreakingChecker {
	return &protoBreakingChecker{}
}

func (c *protoBreakingChecker) GetName() string            { return ProtoBreakingWFName }
func (c *protoBreakingChecker) GetLciStampKey() string     { return ProtoBreakingWFName }
func (c *protoBreakingChecker) GetWorkflowNames() []string { return []string{ProtoBreakingWFName} }

func (c *protoBreakingChecker) Execute(p *projectinfo.ProjectInfo) []*Report {
	if err := c.execute(p); err != nil {
		return []*Report{{Name: c.GetName(), Status: Failure, Err: err}}
	}
	return []*Report{{Name: c.GetName(), Status: Success}}
}

func (c *protoBreakingChecker) execute(p *projectinfo.ProjectInfo) error {
	if p.PrData == nil || p.PrData.BaseBranch == "" {
		logStep(c, "No PR base branch available, skipping")
		return nil
	}
	if p.DiffData == nil || p.DiffData.OverallFilesChanges == nil {
		return nil
	}

	// Decide run/no-run over ModifiedFiles, which includes removed files —
	// deleting a proto is itself a FILE-policy breaking change, so a
	// deletion-only PR must NOT skip here (NonRemovedFiles would be empty and
	// short-circuit before the whole-module comparison ever runs).
	protoFiles := filterProtoFiles(p.DiffData.OverallFilesChanges.ModifiedFiles)
	if len(protoFiles) == 0 {
		logStep(c, "No proto files changed, skipping")
		return nil
	}

	if _, err := exec.LookPath("buf"); err != nil {
		// This point is reached only when proto files changed (the len==0 skip is above).
		// A mandatory breaking-change gate must NOT pass just because buf is absent: the
		// runner stamps a successful checker and the remote gate trusts that stamp, so
		// returning nil here would let a breaking proto change through on any setup without
		// buf. Fail closed — a proto PR requires buf to actually run `buf breaking`.
		return errors.Wrapf(err, "buf CLI not found but %d proto file(s) changed; the breaking-change gate cannot be skipped on a proto PR — install buf", len(protoFiles))
	}

	cmdRunner, err := cmdrunner.NewCmdRunner(p.RepoPath, cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		return errors.Wrap(err, "creating cmd runner")
	}
	defer func() { _ = cmdRunner.Close() }()

	// Fetch the base into an explicit origin/<base> ref first: a CI checkout
	// scoped to one ref narrows origin's refspec, so origin/<base> is otherwise
	// unresolvable and the merge-base below would error before buf ever runs.
	baseRef, err := fetchBaseBranchRef(cmdRunner, p.PrData.BaseBranch)
	if err != nil {
		return errors.Wrap(err, "fetching base branch for buf breaking")
	}

	// Compare against the merge-base commit, not the base branch tip, so a base
	// that has moved ahead does not report its own unrelated changes as this
	// PR's breakage. Resolve it to a SHA buf can read from the shared git dir.
	mergeBase, err := cmdRunner.ExecuteCommand(fmt.Sprintf("git merge-base %s HEAD", baseRef))
	if err != nil {
		return errors.Wrap(err, "resolving merge base for buf breaking")
	}
	mergeBase = strings.TrimSpace(mergeBase)
	if mergeBase == "" {
		return fmt.Errorf("empty merge base against %s", baseRef)
	}

	logStep(c, fmt.Sprintf("Checking %d changed proto file(s) for breaking changes against %s", len(protoFiles), mergeBase[:min(12, len(mergeBase))]))

	violations, err := runBufBreaking(p.RepoPath, mergeBase)
	if err != nil {
		return errors.Wrap(err, "running buf breaking")
	}
	// Two wire-compatible violation classes are post-filtered: a scalar gaining
	// `optional` (presence widening) and a type moved to a sibling file in the
	// same package (relocation). Both are breaks buf's FILE policy reports but
	// that no historical payload or package-qualified consumer actually sees.
	kept := c.filterAllowedPresenceWidenings(violations)
	presenceAllowed := len(violations) - len(kept)
	kept, relocationAllowed, err := c.filterAllowedSamePackageRelocations(p.RepoPath, kept)
	if err != nil {
		return errors.Wrap(err, "checking same-package relocations")
	}
	if len(kept) == 0 {
		// Name any allowance used on the passing path — a green run whose only
		// trace is a per-line log inside a log nobody opens is a silent standing
		// exception; the pass message itself carries the counts.
		logStep(c, protoBreakingPassMessage(presenceAllowed, relocationAllowed))
		return nil
	}
	violations = kept

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d breaking proto change(s) — a stored vocabulary keys on field/enum names and numbers, so a rename or renumber orphans historical rows:\n\n", len(violations))
	for _, v := range violations {
		fmt.Fprintf(&sb, "  %s\n", v)
	}
	sb.WriteString("\nAdd a new field/value instead of renaming or renumbering; deprecate rather than remove (buf.yaml FILE policy).\n")
	return fmt.Errorf("%s", sb.String())
}

// buf's cardinality-violation phrasing for the two proto3 scalar presence
// forms. Matched as exact quoted phrases so only the presence transition is
// filtered — repeated/required cardinality changes never contain this pair.
const (
	bufImplicitPresencePhrase = `from "optional with implicit presence"`
	bufExplicitPresencePhrase = `to "optional with explicit presence"`
)

// filterAllowedPresenceWidenings drops the one violation class this gate
// deliberately allows: a proto3 scalar gaining `optional` (implicit → explicit
// presence). The wire encoding is identical — every historical payload parses
// unchanged — so the stored-vocabulary rationale this gate exists for does not
// apply; the only break is generated-source (value → pointer accessors), which
// the same PR must fix to compile. The reverse transition removes presence
// information readers rely on and stays a hard failure, as does every other
// cardinality change.
func (c *protoBreakingChecker) filterAllowedPresenceWidenings(violations []string) []string {
	var remaining []string
	for _, v := range violations {
		if strings.Contains(v, "changed cardinality") &&
			strings.Contains(v, bufImplicitPresencePhrase) &&
			strings.Contains(v, bufExplicitPresencePhrase) {
			logStep(c, fmt.Sprintf("Allowed presence widening (wire-compatible): %s", v))
			continue
		}
		remaining = append(remaining, v)
	}
	return remaining
}

// protoBreakingPassMessage is the checker's success line, naming any allowance
// it exercised so a passing run that leaned on one is not indistinguishable from
// one with nothing to allow.
func protoBreakingPassMessage(presenceAllowed, relocationAllowed int) string {
	var notes []string
	if presenceAllowed > 0 {
		notes = append(notes, fmt.Sprintf("%d wire-compatible presence widening(s)", presenceAllowed))
	}
	if relocationAllowed > 0 {
		notes = append(notes, fmt.Sprintf("%d same-package type relocation(s)", relocationAllowed))
	}
	if len(notes) == 0 {
		return "No breaking proto changes"
	}
	return fmt.Sprintf("No breaking proto changes (%s allowed)", strings.Join(notes, ", "))
}

// buf's phrasing for a top-level type removed from a file. The same line is
// emitted whether the type was genuinely deleted or merely moved to a sibling
// file in the same package; the two are told apart against the current image
// (below), never by this phrasing alone. A NESTED type deletion reads "deleted
// from message ..." instead, so it never matches here and stays a hard failure.
const (
	bufEnumDeletedPrefix     = `Previously present enum "`
	bufMessageDeletedPrefix  = `Previously present message "`
	bufDeletedFromFileSuffix = `" was deleted from file.`
)

// parseDeletedFromFileViolation extracts the removed type's name and the proto
// file it was removed from, for a buf top-level "deleted from file" violation.
// ok is false for every other violation. buf's text format is
// `path:line:col:message`, and neither a proto path nor a quoted type name can
// contain a colon, so a 4-way split cleanly separates the file path from the
// message.
func parseDeletedFromFileViolation(v string) (typeName, filePath string, ok bool) {
	parts := strings.SplitN(v, ":", 4)
	if len(parts) < 4 {
		return "", "", false
	}
	filePath, msg := parts[0], parts[3]
	var prefix string
	switch {
	case strings.HasPrefix(msg, bufEnumDeletedPrefix):
		prefix = bufEnumDeletedPrefix
	case strings.HasPrefix(msg, bufMessageDeletedPrefix):
		prefix = bufMessageDeletedPrefix
	default:
		return "", "", false
	}
	if !strings.HasSuffix(msg, bufDeletedFromFileSuffix) {
		return "", "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(msg, prefix), bufDeletedFromFileSuffix)
	if filePath == "" || name == "" || strings.Contains(name, `"`) {
		return "", "", false
	}
	return name, filePath, true
}

// protoImageIndex is a lookup over the current proto image: which top-level
// types each package defines, and which package each file declares.
type protoImageIndex struct {
	pkgTypes  map[string]map[string]bool // package → set of top-level message/enum names
	fileToPkg map[string]string          // image file name (module-relative) → package
}

// stillDefined reports whether typeName is a top-level type of pkg in the image.
func (idx *protoImageIndex) stillDefined(pkg, typeName string) bool {
	return idx.pkgTypes[pkg][typeName]
}

// packageForFile returns the package declared by the image file whose
// module-relative name matches the given (repo-relative) violation path,
// preferring the longest suffix match so a nested path can't be shadowed by a
// shorter one. Empty when no image file matches.
func (idx *protoImageIndex) packageForFile(filePath string) string {
	bestLen, bestPkg := -1, ""
	for name, pkg := range idx.fileToPkg {
		if (name == filePath || strings.HasSuffix(filePath, "/"+name)) && len(name) > bestLen {
			bestLen, bestPkg = len(name), pkg
		}
	}
	return bestPkg
}

// buildCurrentProtoImageIndex builds the working tree's proto image with buf and
// indexes it. It is the ground truth for "does this type still exist": a build
// or parse failure is returned as an error so the caller fails the gate closed
// rather than dropping a deletion it could not disprove.
func buildCurrentProtoImageIndex(repoPath string) (*protoImageIndex, error) {
	ctx, cancel := context.WithTimeout(context.Background(), bufBreakingTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "buf", "build", "-o", "-#format=binpb")
	cmd.Dir = repoPath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("buf build for relocation check did not run cleanly (%w): %s", err, strings.TrimSpace(stderr.String()))
	}
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(stdout.Bytes(), &fds); err != nil {
		return nil, fmt.Errorf("parsing buf image: %w", err)
	}
	idx := &protoImageIndex{pkgTypes: map[string]map[string]bool{}, fileToPkg: map[string]string{}}
	for _, f := range fds.GetFile() {
		pkg := f.GetPackage()
		idx.fileToPkg[f.GetName()] = pkg
		if idx.pkgTypes[pkg] == nil {
			idx.pkgTypes[pkg] = map[string]bool{}
		}
		for _, m := range f.GetMessageType() {
			idx.pkgTypes[pkg][m.GetName()] = true
		}
		for _, e := range f.GetEnumType() {
			idx.pkgTypes[pkg][e.GetName()] = true
		}
	}
	return idx, nil
}

// filterAllowedSamePackageRelocations drops a "type deleted from file" violation
// when that type still exists as a top-level type in the same package in the
// current image — the type was moved to a sibling file, not removed. Its
// package-qualified name and field/value numbers are unchanged, so every
// historical payload and every consumer keyed on that qualified name is
// unaffected; buf's FILE policy flags the move only because it pins types to
// files. A genuine deletion (type absent from the image) and a move to a
// DIFFERENT package (qualified name changed, so absent from the losing file's
// package) are both kept as hard failures. The image is built only when a
// deletion violation is actually present, and any build failure fails closed.
func (c *protoBreakingChecker) filterAllowedSamePackageRelocations(repoPath string, violations []string) ([]string, int, error) {
	anyDeletion := false
	for _, v := range violations {
		if _, _, ok := parseDeletedFromFileViolation(v); ok {
			anyDeletion = true
			break
		}
	}
	if !anyDeletion {
		return violations, 0, nil
	}
	idx, err := buildCurrentProtoImageIndex(repoPath)
	if err != nil {
		return nil, 0, errors.Wrap(err, "building current proto image index")
	}
	kept, allowed := c.dropSamePackageRelocations(violations, idx)
	return kept, allowed, nil
}

// dropSamePackageRelocations is the pure filtering step, split out so the
// relocation-vs-deletion decision is unit-testable against a hand-built index.
func (c *protoBreakingChecker) dropSamePackageRelocations(violations []string, idx *protoImageIndex) ([]string, int) {
	var remaining []string
	allowed := 0
	for _, v := range violations {
		if name, filePath, ok := parseDeletedFromFileViolation(v); ok {
			if pkg := idx.packageForFile(filePath); pkg != "" && idx.stillDefined(pkg, name) {
				logStep(c, fmt.Sprintf("Allowed same-package relocation (wire-compatible): %s", v))
				allowed++
				continue
			}
		}
		remaining = append(remaining, v)
	}
	return remaining, allowed
}

// runBufBreaking runs `buf breaking` over the whole proto module against the
// given git ref and returns one formatted string per violation. It compares the
// whole module — never scoped by --path — so a deleted proto file (a FILE-policy
// breaking change that no surviving path could name) is still caught; untouched
// files are byte-identical to the ref and report nothing.
//
// The exit code is load-bearing, and a naive `out, _ := cmd.Output()` would fail
// this gate OPEN: buf exits bufBreakingExitCode with the violations on stdout
// when it finds breaking changes, but exits a different non-zero code with an
// empty stdout and the cause on stderr on an operational failure (an unreadable
// ref, a module-resolution error, a crash). Treating that empty stdout as "no
// violations" would pass a broken gate, so an operational failure returns an
// error (failing the checker) rather than a clean result.
func runBufBreaking(repoPath, againstRef string) ([]string, error) {
	// Pin the breaking policy to FILE via an inline --config, rather than
	// trusting the working tree's buf.yaml: a PR that also weakened or removed
	// the breaking policy would otherwise have this checker delegate to the
	// downgraded config and stamp a rename/renumber as clean under the wrong
	// rules. The rest of the module config (deps, module paths) is read from
	// buf.yaml so it stays in sync.
	config, err := buildBufBreakingConfig(repoPath)
	if err != nil {
		return nil, errors.Wrap(err, "building buf breaking config")
	}

	args := []string{
		"breaking",
		"--against", fmt.Sprintf(".git#ref=%s", againstRef),
		"--config", config,
		"--error-format", "text",
	}

	ctx, cancel := context.WithTimeout(context.Background(), bufBreakingTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "buf", args...)
	cmd.Dir = repoPath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err == nil {
		return nil, nil // exit 0: no breaking change
	}

	var exitErr *exec.ExitError
	if stderrors.As(err, &exitErr) && exitErr.ExitCode() == bufBreakingExitCode {
		return splitNonEmptyLines(stdout.String()), nil // breaking changes on stdout
	}

	// Any other failure is operational — surface it instead of passing silently.
	detail := strings.TrimSpace(stderr.String())
	if detail == "" {
		detail = strings.TrimSpace(stdout.String())
	}
	return nil, fmt.Errorf("buf breaking did not run cleanly (%w): %s", err, detail)
}

func splitNonEmptyLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

// buildBufBreakingConfig reads buf.yaml and returns it as JSON with the breaking
// rule set forced to FILE and any except/ignore stripped, so the policy this
// gate enforces cannot be weakened or exempted by the same PR under test. The
// module paths and deps are preserved from buf.yaml so dependency resolution
// stays correct.
func buildBufBreakingConfig(repoPath string) (string, error) {
	data, err := os.ReadFile(filepath.Join(repoPath, "buf.yaml"))
	if err != nil {
		return "", fmt.Errorf("reading buf.yaml: %w", err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parsing buf.yaml: %w", err)
	}
	breaking, ok := cfg["breaking"].(map[string]any)
	if !ok {
		breaking = map[string]any{}
		cfg["breaking"] = breaking
	}
	breaking["use"] = []any{"FILE"}
	delete(breaking, "except")
	delete(breaking, "ignore")
	out, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshaling buf config: %w", err)
	}
	return string(out), nil
}

func (c *protoBreakingChecker) Cleanup(_ *projectinfo.ProjectInfo) error { return nil }

func (c *protoBreakingChecker) GetFileFilter(_ *projectinfo.ProjectInfo) []string {
	return []string{"**.proto"}
}

func (c *protoBreakingChecker) GetExecutionMode() ExecutionMode { return Serial }

// fetchBaseBranchRef fetches the PR's base branch into origin/<base> and returns
// that ref, so `buf breaking` can diff against the merge base. The branch name
// is passed as a verbatim argv element (ExecuteCommandArgs), never through a
// shell, so a crafted branch name cannot inject a command.
func fetchBaseBranchRef(cmdRunner *cmdrunner.CmdRunner, baseBranch string) (string, error) {
	refspec := "+" + baseBranch + ":refs/remotes/origin/" + baseBranch
	if _, err := cmdRunner.ExecuteCommandArgs("git", []string{"fetch", "origin", refspec}); err != nil {
		return "", errors.Wrap(err, "git fetch origin "+baseBranch)
	}
	return "origin/" + baseBranch, nil
}
