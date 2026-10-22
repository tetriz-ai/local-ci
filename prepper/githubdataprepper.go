// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prepper

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/internal/ghclient"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/pkg/cmdlogger"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

// shellUnsafeChars are shell metacharacters and quote-breakers rejected in
// contributor-controlled values before they reach command construction.
const shellUnsafeChars = "`$();&|<>'\"\\ \t\n\r"

// changedFileUnsafeChars is the set of characters a changed-file path may not
// contain. It is deliberately narrower than shellUnsafeChars in two ways:
//   - It admits a space: a filename may legitimately contain one, and every
//     command that consumes a changed-file path passes it as a verbatim argv
//     element (ExecuteCommandArgs), never interpolated into a shell string, so a
//     space cannot split an argument or inject. Rejecting it aborted the whole run
//     on one space-named file.
//   - It excludes parentheses because Next.js uses parenthesized directory
//     segments for route groups (e.g. (marketing)); validateChangedFilePath admits
//     those only as whole, well-formed route-group segments.
//
// Tab, newline and carriage return stay rejected: they are not shell hazards here
// but they break the line-based parsing of `git diff --name-only` output. Command
// substitution is impossible regardless because `$` and backtick are rejected.
const changedFileUnsafeChars = "`$;&|<>'\"\\\t\n\r"

// validateShellSafe rejects a contributor-controlled value that carries a shell
// metacharacter. label names the value for the error.
func validateShellSafe(label, value string) error {
	if strings.ContainsAny(value, shellUnsafeChars) {
		return errors.Errorf("%s %q contains shell metacharacters; refusing to run", label, value)
	}
	return nil
}

func validateChangedFilePath(value string) error {
	if strings.ContainsAny(value, changedFileUnsafeChars) {
		return errors.Errorf("changed file path %q contains shell metacharacters; refusing to run", value)
	}
	if strings.ContainsAny(value, "()") && !isRouteGroupPath(value) {
		return errors.Errorf("changed file path %q contains parentheses outside a well-formed route-group segment; refusing to run", value)
	}
	return nil
}

// isRouteGroupPath reports whether every parenthesized segment of value is a
// whole, well-formed route-group directory — a segment that starts with "(",
// ends with ")", and is at least three characters — while the filename segment
// itself carries no parentheses. This admits Next.js route groups like
// web/src/app/(marketing)/page.tsx in any location, yet rejects a malformed or
// partial-paren segment such as foo(bar) or (unterminated outright. Command
// substitution cannot slip through here because `$` is rejected before this.
func isRouteGroupPath(value string) bool {
	parts := strings.Split(value, "/")
	// The final segment is the filename; it must not contain parentheses.
	if strings.ContainsAny(parts[len(parts)-1], "()") {
		return false
	}
	hasRouteGroup := false
	for _, part := range parts[:len(parts)-1] {
		if !strings.ContainsAny(part, "()") {
			continue
		}
		if len(part) < 3 || part[0] != '(' || part[len(part)-1] != ')' {
			return false
		}
		hasRouteGroup = true
	}
	return hasRouteGroup
}

// validateBranchRef rejects an empty or shell-unsafe github-sourced branch ref.
// kind is "head"/"base" for the error message.
func validateBranchRef(kind, ref string) error {
	if ref == "" {
		return errors.Errorf("%s branch ref from github is empty", kind)
	}
	if err := validateShellSafe(kind+" branch ref", ref); err != nil {
		return fmt.Errorf("validating branch ref: %w", err)
	}
	return nil
}

// githubDataPrepper is responsible for getting pr related details from github
type githubDataPrepper struct{}

func NewGithubDataPrepper() *githubDataPrepper {
	return &githubDataPrepper{}
}

func (g *githubDataPrepper) GetName() string {
	return "GithubDataPrepper"
}

func (g *githubDataPrepper) Execute(p *projectinfo.ProjectInfo) error {
	cmdRunner, err := cmdrunner.NewCmdRunner(p.RepoPath, cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		return errors.Wrap(err, "error while trying to create cmd runner")
	}
	defer func() {
		if cerr := cmdRunner.Close(); cerr != nil {
			logger.ErrorNoCtx("closing cmd runner", zap.Error(cerr))
		}
	}()
	githubClient, ghErr := ghclient.NewGhClient(cmdRunner)
	if ghErr != nil {
		return errors.Wrap(ghErr, "error initialising github client")
	}

	prInfo, err := githubClient.GetPrInfo(p.UserInput.PrNumber, p.RepoOwner, p.RepoName)
	if err != nil {
		return errors.Wrap(err, "error getting pr info from github")
	}

	if *prInfo.Merged {
		return errors.New("provided pr is already merged")
	}

	prFiles, err := githubClient.GetPrFiles(p.UserInput.PrNumber, p.RepoOwner, p.RepoName)
	if err != nil {
		return errors.Wrap(err, "error fetching PR files")
	}

	prCommits, prCommitsErr := githubClient.GetPrCommits(p.UserInput.PrNumber, p.RepoOwner, p.RepoName)
	if prCommitsErr != nil {
		return errors.Wrap(prCommitsErr, "error getting pr commits")
	}

	p.PrData = &projectinfo.PrData{
		BaseBranch:   prInfo.GetBase().GetRef(),
		HeadBranch:   prInfo.GetHead().GetRef(),
		LatestCommit: prInfo.GetHead().GetSHA(),
		PrFiles:      prFiles,
		AllCommits:   prCommits,
	}

	for _, b := range []struct{ kind, ref string }{
		{"head", p.PrData.HeadBranch},
		{"base", p.PrData.BaseBranch},
	} {
		if err := validateBranchRef(b.kind, b.ref); err != nil {
			return fmt.Errorf("validating %s branch ref: %w", b.kind, err)
		}
	}

	return nil
}

func (g *githubDataPrepper) Cleanup(p *projectinfo.ProjectInfo) error {
	return nil
}
