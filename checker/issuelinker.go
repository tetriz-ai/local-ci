// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"

	"github.com/pkg/errors"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/pkg/cmdlogger"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

const IssueLinkerWFName = "issue_linker"

var (
	// Matches: Closes #123, Fixes #456, Resolves #789 (case-insensitive, optional colon).
	// The leading boundary stops a stem-suffixed word (unresolved, discloses) from matching.
	closingKeywordRe = regexp.MustCompile(`(?i)(?:^|[^A-Za-z])(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?):?\s+#\d+`)
	// Matches: Closes https://github.com/<owner>/<repo>/issues/123 (closing keyword required)
	closingKeywordURLRe = regexp.MustCompile(`(?i)(?:^|[^A-Za-z])(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?):?\s+https://github\.com/[^/\s]+/[^/\s]+/issues/\d+`)
	// Matches bare issue URLs without a closing keyword (used to detect and warn)
	bareIssueURLRe = regexp.MustCompile(`https://github\.com/[^/\s]+/[^/\s]+/issues/\d+`)
	// Strips ANSI escape sequences from command output
	ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
)

type issueLinkerChecker struct{}

func NewIssueLinkerChecker() *issueLinkerChecker {
	return &issueLinkerChecker{}
}

func (c *issueLinkerChecker) GetName() string {
	return IssueLinkerWFName
}

func (c *issueLinkerChecker) GetLciStampKey() string {
	return IssueLinkerWFName
}

func (c *issueLinkerChecker) GetWorkflowNames() []string {
	return []string{IssueLinkerWFName}
}

func (c *issueLinkerChecker) Execute(p *projectinfo.ProjectInfo) []*Report {
	if err := c.execute(p); err != nil {
		return []*Report{{Name: c.GetName(), Status: Failure, Err: err}}
	}
	return []*Report{{Name: c.GetName(), Status: Success}}
}

type ghPRView struct {
	Body string `json:"body"`
}

func (c *issueLinkerChecker) execute(p *projectinfo.ProjectInfo) error {
	// A linked issue is enforced on every pull request, regardless of the base
	// branch — the concern is PR-level, not branch-specific.

	// Skip gracefully if gh CLI is not installed
	if _, err := exec.LookPath("gh"); err != nil {
		logger.Warn(fmt.Sprintf("[%s] gh CLI not found, skipping issue link check: %v", c.GetName(), err))
		return nil
	}

	cmdRunner, err := cmdrunner.NewCmdRunner(p.RepoPath, cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		return errors.Wrap(err, "error creating cmd runner")
	}
	defer func() {
		if closeErr := cmdRunner.Close(); closeErr != nil {
			logger.ErrorNoCtx(fmt.Sprintf("closing command runner: %v", closeErr))
		}
	}()

	logStep(c, "Checking PR body for GitHub issue reference")
	output, err := cmdRunner.ExecuteCommand(
		fmt.Sprintf("gh pr view %d --json body", p.UserInput.PrNumber),
		"NO_COLOR=1",
	)
	if err != nil {
		return errors.Wrap(err, "error fetching PR data")
	}

	var pr ghPRView
	cleanOutput := ansiEscapeRe.ReplaceAllString(output, "")
	if err := json.Unmarshal([]byte(cleanOutput), &pr); err != nil {
		return errors.Wrap(err, "error parsing PR data")
	}

	// Check for closing keywords (with shorthand or full URL)
	if closingKeywordRe.MatchString(pr.Body) || closingKeywordURLRe.MatchString(pr.Body) {
		logStep(c, "GitHub issue reference found in PR body")
		return nil
	}

	// Detect bare issue URLs without a closing keyword and give a targeted hint
	if bareIssueURLRe.MatchString(pr.Body) {
		return fmt.Errorf(`issue URL found but missing a closing keyword

GitHub only auto-closes issues when the URL is preceded by a keyword.
Change your PR description from:
  https://github.com/<owner>/<repo>/issues/123
To:
  Closes https://github.com/<owner>/<repo>/issues/123

Accepted keywords: Closes, Fixes, Resolves (and their variants)`)
	}

	return fmt.Errorf(`no GitHub issue linked in PR body

Add a closing keyword to your PR description:
  Closes #123  |  Fixes #123  |  Resolves #123

Or use a full issue URL with a closing keyword:
  Closes https://github.com/<owner>/<repo>/issues/123`)
}

func (c *issueLinkerChecker) Cleanup(_ *projectinfo.ProjectInfo) error {
	return nil
}

func (c *issueLinkerChecker) GetFileFilter(_ *projectinfo.ProjectInfo) []string {
	return []string{"**"}
}

func (c *issueLinkerChecker) GetExecutionMode() ExecutionMode {
	return Parallel
}
