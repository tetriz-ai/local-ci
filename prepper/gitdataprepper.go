// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prepper

import (
	"fmt"
	"os"
	"strings"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/pkg/cmdlogger"
	"github.com/tetriz-ai/local-ci/pkg/git"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

// gitDataPrepper is responsible for preparing Git-related data.
type gitDataPrepper struct {
	initialBranch string
	branchRestore bool
}

// NewGitDataPrepper creates a new instance of gitDataPrepper.
func NewGitDataPrepper() *gitDataPrepper {
	return &gitDataPrepper{}
}

// GetName returns the name of the gitDataPrepper.
func (g *gitDataPrepper) GetName() string {
	return "GitDataPrepper"
}

// Execute prepares Git-related data for a project.
func (g *gitDataPrepper) Execute(p *projectinfo.ProjectInfo) error {
	cmdRunner, err := cmdrunner.NewCmdRunner(p.RepoPath, cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		return errors.Wrap(err, "error while trying to create cmd runner")
	}
	defer func() {
		if cerr := cmdRunner.Close(); cerr != nil {
			logger.ErrorNoCtx("closing cmd runner", zap.Error(cerr))
		}
	}()

	// Fetch notes from origin
	cmdErr := git.FetchNotesFromRemoteRepo(cmdRunner)
	if cmdErr != nil {
		return errors.Wrap(cmdErr, "error fetching git note from remote repo")
	}

	if p.UserInput.DryRunMode {
		return nil
	}

	// Get branch name
	currBranch, cmdErr := cmdRunner.ExecuteCommand("git rev-parse --abbrev-ref HEAD")
	if cmdErr != nil {
		return errors.Wrap(cmdErr, "error getting current git branch")
	}
	g.initialBranch = currBranch

	if p.PrData == nil {
		return errors.New("Found empty pr data, make sure githubDataPrepper is run")
	}

	if currBranch != p.PrData.HeadBranch {
		// A branch can be checked out in only one worktree at a time. If the PR
		// head branch is already checked out in another worktree, the checkout
		// below fails with git's opaque "fatal: '<branch>' is already checked out
		// at '<path>'". Detect it first and tell the operator where to run local-ci from.
		wtList, wtErr := cmdRunner.ExecuteCommand("git worktree list --porcelain")
		if wtErr != nil {
			// Best-effort: a failed worktree-list falls through to the normal
			// checkout, which surfaces git's own error. Log so the skipped
			// detection is visible rather than silently swallowed.
			logger.InfoNoCtx("skipping worktree-collision check: " + wtErr.Error())
		} else if wtPath := worktreeForBranch(wtList, p.PrData.HeadBranch); wtPath != "" {
			if _, statErr := os.Stat(wtPath); statErr != nil {
				// The branch is registered to a worktree whose directory is gone
				// (deleted without `git worktree prune`). git still refuses the
				// checkout, so point at prune, not a path that no longer exists.
				return fmt.Errorf("branch %q is registered to a stale worktree %q that no longer exists (%w) — run `git worktree prune`, then retry", p.PrData.HeadBranch, wtPath, statErr)
			}
			return fmt.Errorf("branch %q is checked out in worktree %q — run local-ci from there (cd %q && local-ci ...), not from this checkout", p.PrData.HeadBranch, wtPath, wtPath)
		}
		g.branchRestore = true
		// change branch to base branch
		if err := checkoutBranch(cmdRunner, p.PrData.HeadBranch); err != nil {
			return errors.Wrap(err, "error checking out pr head branch")
		}
	}
	// Get current commit
	latestCommit, commitCmdErr := cmdRunner.ExecuteCommand("git rev-parse HEAD")
	if commitCmdErr != nil {
		return errors.Wrap(commitCmdErr, "error getting latest commit SHA")
	}

	if latestCommit != p.PrData.LatestCommit {
		return errors.New("latest commit on github and local does not match, please pull/push changes")
	}

	return nil
}

// worktreeForBranch parses `git worktree list --porcelain` output and returns
// the path of the worktree that currently has refs/heads/<branch> checked out,
// or "" if no worktree holds it. A branch is checked out in at most one worktree,
// so the first match is authoritative. The porcelain format is newline-separated
// records, each opening with a `worktree <path>` line and carrying a
// `branch refs/heads/<name>` line when a branch (not a detached HEAD) is checked out.
func worktreeForBranch(porcelain, branch string) string {
	target := "branch refs/heads/" + branch
	var path string
	for line := range strings.SplitSeq(porcelain, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case line == target:
			return path
		}
	}
	return ""
}

// Cleanup performs clean-up operations.
func (g *gitDataPrepper) Cleanup(p *projectinfo.ProjectInfo) error {
	if !g.branchRestore {
		return nil
	}
	cmdRunner, err := cmdrunner.NewCmdRunner(p.RepoPath, cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		return errors.Wrap(err, "error while trying to create cmd runner")
	}
	defer func() {
		if cerr := cmdRunner.Close(); cerr != nil {
			logger.ErrorNoCtx("closing cmd runner", zap.Error(cerr))
		}
	}()
	// check if any changes were made as part of the checkers (e.g. a code generator a checker ran)
	isClean, err := isGitWorkspaceClean(cmdRunner)
	if err != nil {
		return fmt.Errorf("checking git workspace clean: %w", err)
	}
	if !isClean {
		logger.InfoNoCtx("Detected file changes in current branch, not reverting back to initial branch: " + g.initialBranch)
		return nil
	}
	if err = checkoutBranch(cmdRunner, g.initialBranch); err != nil {
		return errors.Wrap(err, "error reverting back to initial branch")
	}
	return nil
}

// Git plumbing can create leading-dash refs even though `git branch` refuses
// them. A trailing -- separates paths, but does not stop checkout parsing options.
func checkoutBranch(cmdRunner *cmdrunner.CmdRunner, branch string) error {
	if branch == "" || strings.HasPrefix(branch, "-") {
		return fmt.Errorf("refusing empty or option-like checkout branch %q", branch)
	}
	if _, err := cmdRunner.ExecuteCommandArgs("git", []string{"checkout", branch, "--"}); err != nil {
		return fmt.Errorf("checking out branch %q: %w", branch, err)
	}
	return nil
}
