// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package ghclient

import (
	"context"
	"fmt"

	"github.com/google/go-github/v60/github"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	"golang.org/x/oauth2"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
)

type GhClient struct {
	cmdRunner *cmdrunner.CmdRunner
	authToken string
	cl        *github.Client
}

func NewGhClient(cmdRunner *cmdrunner.CmdRunner) (*GhClient, error) {
	// Use the secret path so the token never reaches the debug log or the
	// on-disk output file (see ExecuteCommandSecret). `gh auth token` prints the
	// token on stdout, which ExecuteCommand would stream to both under --debug.
	authToken, err := cmdRunner.ExecuteCommandSecret("gh auth token")
	if err != nil {
		return nil, errors.Wrap(err, "error getting auth token from gh cli")
	}
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: authToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	return &GhClient{cmdRunner, authToken, client}, nil
}

func (g *GhClient) GetPrInfo(prNumber int, owner, repoName string) (*github.PullRequest, error) {
	ctx := context.Background()
	prInfo, _, err := g.cl.PullRequests.Get(ctx, owner, repoName, prNumber)
	if err != nil {
		return nil, fmt.Errorf("fetching PR %d info: %w", prNumber, err)
	}
	return prInfo, nil
}

func (g *GhClient) GetPrFiles(prNumber int, owner, repoName string) ([]*github.CommitFile, error) {
	ctx := context.Background()
	opt := &github.ListOptions{
		PerPage: 100,
	}

	var allPrFiles []*github.CommitFile
	for {
		prFiles, resp, err := g.cl.PullRequests.ListFiles(ctx, owner, repoName, prNumber, opt)
		if err != nil {
			return allPrFiles, fmt.Errorf("listing PR %d files: %w", prNumber, err)
		}

		allPrFiles = append(allPrFiles, prFiles...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return allPrFiles, nil
}

func (g *GhClient) GetPrCommits(prNumber int, owner, repoName string) ([]string, error) {
	ctx := context.Background()
	opt := &github.ListOptions{
		PerPage: 100,
	}
	var allPrCommits []string
	for {
		prCommits, resp, err := g.cl.PullRequests.ListCommits(ctx, owner, repoName, prNumber, opt)
		if err != nil {
			return allPrCommits, fmt.Errorf("listing PR %d commits: %w", prNumber, err)
		}

		for _, commit := range prCommits {
			allPrCommits = append(allPrCommits, commit.GetSHA())
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return allPrCommits, nil
}

func (g *GhClient) ReapplyLabel(prNumber int, label string, owner, repoName string) error {
	ctx := context.Background()
	opt := &github.ListOptions{
		PerPage: 100,
	}

	var allPrLabels []*github.Label
	for {
		prFiles, resp, err := g.cl.Issues.ListLabelsByIssue(ctx, owner, repoName, prNumber, opt)
		if err != nil {
			return errors.Wrap(err, "error getting labels on pr")
		}

		allPrLabels = append(allPrLabels, prFiles...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	labelAlreadyPresent := lo.SomeBy(allPrLabels, func(item *github.Label) bool {
		return item.GetName() == label
	})

	if labelAlreadyPresent {
		_, err := g.cl.Issues.RemoveLabelForIssue(ctx, owner, repoName, prNumber, label)
		if err != nil {
			return errors.Wrap(err, "error removing label from pr")
		}
	}

	_, _, err := g.cl.Issues.AddLabelsToIssue(ctx, owner, repoName, prNumber, []string{label})
	if err != nil {
		return errors.Wrap(err, "error adding label to pr")
	}
	return nil
}
