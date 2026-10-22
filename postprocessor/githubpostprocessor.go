// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package postprocessor

import (
	"fmt"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/internal/ghclient"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/pkg/cmdlogger"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

const RunWorkflowsLabel = "RunWorkflows"

type githubPostProcessor struct {
	cmdExec  cmdrunner.CommandRunner
	ghClient *ghclient.GhClient
}

func NewGithubPostProcessor() PostProcessor {
	return &githubPostProcessor{}
}

func (g *githubPostProcessor) GetName() string {
	return "GithubPostProcessor"
}

func (g *githubPostProcessor) RunAlways() bool {
	return false
}

func (g *githubPostProcessor) Execute(p *projectinfo.ProjectInfo) error {
	cmdRunner, err := cmdrunner.NewCmdRunner(p.RepoPath, cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		return errors.Wrap(err, "error while trying to create cmd runner")
	}
	defer func() {
		if closeErr := cmdRunner.Close(); closeErr != nil {
			logger.ErrorNoCtx("failed to close cmd runner", zap.Error(closeErr))
		}
	}()
	g.cmdExec = cmdRunner

	githubClient, ghErr := ghclient.NewGhClient(cmdRunner)
	if ghErr != nil {
		return errors.Wrap(ghErr, "error initialising github client")
	}
	g.ghClient = githubClient

	// create status for GitHub UI
	_, cmdErr := g.cmdExec.ExecuteCommand(fmt.Sprintf("gh api --method POST -H \"Accept: application/vnd.github+json\" -H \"X-GitHub-Api-Version: 2022-11-28\"  /repos/%s/%s/statuses/%s -f state='success' -f description='Local CI Stamped' -f context='Local CI Stamped'", p.RepoOwner, p.RepoName, p.PrData.LatestCommit))
	if cmdErr != nil {
		return errors.Wrap(cmdErr, "error marking commit status on github")
	}

	// apply run workflows label automatically
	if g.shouldApplyRunWorkflowsLabel(p) {
		labelApplyErr := g.ghClient.ReapplyLabel(p.UserInput.PrNumber, RunWorkflowsLabel, p.RepoOwner, p.RepoName)
		if labelApplyErr != nil {
			return errors.Wrap(labelApplyErr, fmt.Sprintf("error reapplying %s label on PR", RunWorkflowsLabel))
		}
	}
	return nil
}

func (g *githubPostProcessor) shouldApplyRunWorkflowsLabel(p *projectinfo.ProjectInfo) bool {
	// if running inside github, don't apply label
	if p.UserInput.GhActionMode {
		return false
	}

	// check for any failure
	for _, v := range p.CheckerStatus {
		if !v {
			logger.InfoNoCtx("Skipping auto label applying due to some checks failing locally")
			return false
		}
	}

	// check for any workflows still pending
	if len(p.ExcludedAffectedWorkflows) != 0 {
		logger.InfoNoCtx("Skipping auto label applying due to some workflows pending to be run")
		return false
	}

	// No review state is consulted. The label only triggers the mandatory check;
	// it does not merge, and any developer can add it by hand in one click, so a
	// review gate here inconveniences an honest author without stopping anyone.
	// Where review must gate a merge, the branch ruleset is the enforcement point
	// this tool cannot be a substitute for.
	return true
}
