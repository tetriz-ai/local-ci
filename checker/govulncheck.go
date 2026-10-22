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

const GovulncheckWFName = "govulncheck"

// govulncheckChecker runs `govulncheck ./...` to detect known vulnerabilities in
// the module and its dependencies. Triggers when go.mod, go.sum, or any .go file
// changes. govulncheck must be installed; if it is missing or fails to run, the
// check fails loudly rather than passing a PR unscanned.
type govulncheckChecker struct{}

func NewGovulncheckChecker() *govulncheckChecker {
	return &govulncheckChecker{}
}

func (c *govulncheckChecker) GetName() string {
	return GovulncheckWFName
}

func (c *govulncheckChecker) GetLciStampKey() string {
	return GovulncheckWFName
}

func (c *govulncheckChecker) GetWorkflowNames() []string {
	return []string{GovulncheckWFName}
}

func (c *govulncheckChecker) Execute(p *projectinfo.ProjectInfo) []*Report {
	if err := c.execute(p); err != nil {
		return []*Report{{Name: c.GetName(), Status: Failure, Err: err}}
	}
	return []*Report{{Name: c.GetName(), Status: Success}}
}

func (c *govulncheckChecker) execute(p *projectinfo.ProjectInfo) error {
	cmdRunner, err := cmdrunner.NewCmdRunner(p.RepoPath, cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		return errors.Wrap(err, "error creating cmd runner")
	}
	defer func() {
		if closeErr := cmdRunner.Close(); closeErr != nil {
			logger.ErrorNoCtx(fmt.Sprintf("closing command runner: %v", closeErr))
		}
	}()

	logStep(c, "Running govulncheck on the module")
	if _, err := cmdRunner.ExecuteCommand("govulncheck ./..."); err != nil {
		return errors.Wrap(err, "govulncheck reported vulnerabilities or failed to run (install govulncheck: go install golang.org/x/vuln/cmd/govulncheck@latest)")
	}
	return nil
}

func (c *govulncheckChecker) Cleanup(_ *projectinfo.ProjectInfo) error {
	return nil
}

func (c *govulncheckChecker) GetFileFilter(_ *projectinfo.ProjectInfo) []string {
	return []string{
		"**.go",
		"**/go.mod",
		"**/go.sum",
	}
}

func (c *govulncheckChecker) GetExecutionMode() ExecutionMode {
	return Parallel
}
