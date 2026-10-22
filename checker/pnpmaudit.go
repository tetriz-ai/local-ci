// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pkg/errors"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/pkg/cmdlogger"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

const PnpmAuditWFName = "pnpm_audit"

// maxCheckerOutput caps how much of a tool's output a checker folds into its
// error so a multi-megabyte report stays readable in the run log.
const maxCheckerOutput = 8000

// pnpmAuditLevels is the allowlist for the audit severity threshold, in pnpm's
// own order; pnpmAuditDefaultLevel is the threshold when a host configures none.
var pnpmAuditLevels = []string{"low", "moderate", "high", "critical"}

const pnpmAuditDefaultLevel = "high"

// pnpmAuditChecker runs `pnpm audit` and fails when pnpm reports an advisory at
// or above the configured severity. It fires on package.json / pnpm-lock.yaml
// changes anywhere in the repo. A missing pnpm fails the check loudly rather
// than passing a PR unscanned.
type pnpmAuditChecker struct {
	dir   string
	level string
	// configErr holds a construction-time error (an invalid audit level) so the
	// constructor can keep its single-return-value shape; Execute surfaces it.
	configErr error
}

// PnpmAuditOpt configures a pnpmAuditChecker.
type PnpmAuditOpt func(*pnpmAuditChecker)

// WithPnpmAuditDir sets the directory, relative to the repo root, the audit runs
// in (where the package.json / lockfile live). The default is the repo root.
func WithPnpmAuditDir(dir string) PnpmAuditOpt {
	return func(c *pnpmAuditChecker) { c.dir = dir }
}

// WithPnpmAuditLevel sets the severity threshold at or above which an advisory
// fails the check. It must be one of low, moderate, high, or critical; an
// invalid value is recorded as a config error that fails the check at run time.
func WithPnpmAuditLevel(level string) PnpmAuditOpt {
	return func(c *pnpmAuditChecker) {
		if err := validateAuditLevel(level); err != nil {
			c.configErr = err
			return
		}
		c.level = level
	}
}

func NewPnpmAuditChecker(opts ...PnpmAuditOpt) *pnpmAuditChecker {
	c := &pnpmAuditChecker{dir: ".", level: pnpmAuditDefaultLevel}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// validateAuditLevel reports whether level is one of pnpm's accepted audit
// severities, returning a clear config error otherwise.
func validateAuditLevel(level string) error {
	if slices.Contains(pnpmAuditLevels, level) {
		return nil
	}
	return fmt.Errorf("invalid pnpm audit level %q: must be one of %s", level, strings.Join(pnpmAuditLevels, ", "))
}

// pnpmAuditArgs builds the argv for `pnpm audit` at the given severity. Split out
// so the arguments are table-testable without running anything.
func pnpmAuditArgs(level string) []string {
	return []string{"audit", "--audit-level", level}
}

func (c *pnpmAuditChecker) GetName() string {
	return PnpmAuditWFName
}

func (c *pnpmAuditChecker) GetLciStampKey() string {
	return PnpmAuditWFName
}

func (c *pnpmAuditChecker) GetWorkflowNames() []string {
	return []string{PnpmAuditWFName}
}

func (c *pnpmAuditChecker) Execute(p *projectinfo.ProjectInfo) []*Report {
	if err := c.execute(p); err != nil {
		return []*Report{{Name: c.GetName(), Status: Failure, Err: err}}
	}
	return []*Report{{Name: c.GetName(), Status: Success}}
}

func (c *pnpmAuditChecker) execute(p *projectinfo.ProjectInfo) error {
	if c.configErr != nil {
		return c.configErr
	}

	cmdRunner, err := cmdrunner.NewCmdRunner(filepath.Join(p.RepoPath, c.dir), cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		return errors.Wrap(err, "error creating cmd runner")
	}
	defer func() {
		if closeErr := cmdRunner.Close(); closeErr != nil {
			logger.ErrorNoCtx(fmt.Sprintf("closing command runner: %v", closeErr))
		}
	}()

	logStep(c, fmt.Sprintf("Running pnpm audit (--audit-level %s)", c.level))
	out, runErr := cmdRunner.ExecuteCommandArgs("pnpm", pnpmAuditArgs(c.level))
	if runErr != nil {
		msg := execOutputMsg(out, runErr)
		if isBinaryMissing(msg) {
			return errors.New("pnpm not installed — cannot run pnpm audit (install pnpm: https://pnpm.io/installation)")
		}
		return fmt.Errorf("pnpm audit failed — vulnerabilities at or above level %q, or pnpm could not complete:\n%s", c.level, truncateOutput(msg))
	}
	return nil
}

func (c *pnpmAuditChecker) Cleanup(_ *projectinfo.ProjectInfo) error {
	return nil
}

func (c *pnpmAuditChecker) GetFileFilter(_ *projectinfo.ProjectInfo) []string {
	// Leading "**" so the globs match a manifest anywhere in the tree and absorb
	// any repo-name prefix the eligibility matcher adds.
	return []string{
		"**/package.json",
		"**/pnpm-lock.yaml",
	}
}

func (c *pnpmAuditChecker) GetExecutionMode() ExecutionMode {
	return Parallel
}

// execOutputMsg returns the most informative text from a command's combined
// output and its error: the output when the command produced any, else the
// error string. Used to classify a failure and to surface it to the developer.
func execOutputMsg(out string, err error) string {
	msg := strings.TrimSpace(out)
	if msg == "" && err != nil {
		msg = strings.TrimSpace(err.Error())
	}
	return msg
}

// isBinaryMissing reports whether a command failure was caused specifically by the
// executable not being on PATH, which os/exec reports as "executable file not
// found". It deliberately matches only that signal, not a general start failure
// (a bad working directory also fails to start the command), so a genuinely-absent
// tool is reported as "not installed" while any other start failure surfaces as an
// ordinary run failure instead of a misleading install hint. A checker that shells
// out still fails closed on a missing tool — a PR must never ride through unchecked
// because a required binary was absent.
func isBinaryMissing(msg string) bool {
	return strings.Contains(msg, "executable file not found")
}

// truncateOutput trims surrounding whitespace and caps the text at
// maxCheckerOutput so a large tool report stays readable in an error.
func truncateOutput(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxCheckerOutput {
		return s[:maxCheckerOutput] + "\n... (output truncated)"
	}
	return s
}
