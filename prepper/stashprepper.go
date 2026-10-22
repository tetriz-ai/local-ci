// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prepper

import (
	"fmt"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/pkg/cmdlogger"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

type stashPrepper struct {
	filesStashed bool
	// flag to control if actual stash should be done or show error and exit
	showErrorAndExit bool
}

func NewStashPrepper() *stashPrepper {
	return &stashPrepper{
		showErrorAndExit: true,
	}
}

func (s *stashPrepper) GetName() string {
	return "StashPrepper"
}

func (s *stashPrepper) Execute(p *projectinfo.ProjectInfo) error {
	if p.UserInput.DryRunMode {
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
	isClean, err := isGitWorkspaceClean(cmdRunner)
	if err != nil {
		return fmt.Errorf("checking git workspace clean: %w", err)
	}
	if isClean {
		return nil
	}

	if s.showErrorAndExit {
		return fmt.Errorf("found uncommitted/untracked files in git repo, please stash the files before running tool")
	}
	_, cmdErr := cmdRunner.ExecuteCommand("git stash save")
	if cmdErr != nil {
		return errors.Wrap(cmdErr, "error stashing changes. Please manually stash and run again")
	}
	s.filesStashed = true

	return nil
}

func (s *stashPrepper) Cleanup(p *projectinfo.ProjectInfo) error {
	if s.showErrorAndExit {
		return nil
	}
	if !s.filesStashed {
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
	_, cmdErr := cmdRunner.ExecuteCommand("git stash apply")
	if cmdErr != nil {
		return errors.Wrap(cmdErr, "error recovering files from stash")
	}

	_, cmdErr = cmdRunner.ExecuteCommand("git stash drop")
	if cmdErr != nil {
		return errors.Wrap(cmdErr, "error dropping temporary stash created")
	}
	return nil
}

func isGitWorkspaceClean(cmdExec *cmdrunner.CmdRunner) (bool, error) {
	cmdOutput, cmdErr := cmdExec.ExecuteCommand("git status --porcelain")
	if cmdErr != nil {
		return false, errors.Wrap(cmdErr, "error getting git status")
	}

	isClean := cmdOutput == ""
	return isClean, nil
}
