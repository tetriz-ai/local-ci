// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"fmt"

	"github.com/pkg/errors"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/pkg/cmdlogger"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

const GolangciLintWFName = "golangci_lint"

// golangciLintChecker runs `golangci-lint run` at the repo root, using the host's
// own `.golangci.*` config. Any non-zero exit fails the check — lint findings and
// operational errors alike (a bad config, code that will not build) all block.
// A missing golangci-lint fails loudly rather than passing a PR unlinted.
type golangciLintChecker struct {
	extraArgs []string
}

// GolangciLintOpt configures a golangciLintChecker.
type GolangciLintOpt func(*golangciLintChecker)

// WithExtraArgs appends arguments to `golangci-lint run`, so a host can scope the
// run (for example `--new-from-merge-base=origin/main` to lint only new findings).
func WithExtraArgs(args []string) GolangciLintOpt {
	return func(c *golangciLintChecker) {
		c.extraArgs = append([]string(nil), args...)
	}
}

func NewGolangciLintChecker(opts ...GolangciLintOpt) *golangciLintChecker {
	c := &golangciLintChecker{}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// golangciLintArgs builds the argv for golangci-lint: `run` followed by any
// host-supplied extra args. Split out so the arguments are table-testable.
func golangciLintArgs(extraArgs []string) []string {
	return append([]string{"run"}, extraArgs...)
}

func (c *golangciLintChecker) GetName() string {
	return GolangciLintWFName
}

func (c *golangciLintChecker) GetLciStampKey() string {
	return GolangciLintWFName
}

func (c *golangciLintChecker) GetWorkflowNames() []string {
	return []string{GolangciLintWFName}
}

func (c *golangciLintChecker) Execute(p *projectinfo.ProjectInfo) []*Report {
	if err := c.execute(p); err != nil {
		return []*Report{{Name: c.GetName(), Status: Failure, Err: err}}
	}
	return []*Report{{Name: c.GetName(), Status: Success}}
}

func (c *golangciLintChecker) execute(p *projectinfo.ProjectInfo) error {
	cmdRunner, err := cmdrunner.NewCmdRunner(p.RepoPath, cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		return errors.Wrap(err, "error creating cmd runner")
	}
	defer func() {
		if closeErr := cmdRunner.Close(); closeErr != nil {
			logger.ErrorNoCtx(fmt.Sprintf("closing command runner: %v", closeErr))
		}
	}()

	logStep(c, "Running golangci-lint")
	out, runErr := cmdRunner.ExecuteCommandArgs("golangci-lint", golangciLintArgs(c.extraArgs))
	if runErr != nil {
		if isBinaryMissing(execOutputMsg(out, runErr)) {
			return errors.New("golangci-lint not installed — cannot run the lint gate (install: https://golangci-lint.run/docs/welcome/install/)")
		}
		return fmt.Errorf("golangci-lint failed (findings or an operational error):\n%s", truncateOutput(out))
	}
	return nil
}

func (c *golangciLintChecker) Cleanup(_ *projectinfo.ProjectInfo) error {
	return nil
}

func (c *golangciLintChecker) GetFileFilter(_ *projectinfo.ProjectInfo) []string {
	// Leading "**" so the globs match a Go source or a golangci config anywhere
	// in the tree and absorb any repo-name prefix the eligibility matcher adds.
	return []string{
		"**.go",
		"**/.golangci.yml",
		"**/.golangci.yaml",
		"**/.golangci.toml",
		"**/.golangci.json",
	}
}

func (c *golangciLintChecker) GetExecutionMode() ExecutionMode {
	return Parallel
}
