// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prepper

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/mod/semver"

	"github.com/tetriz-ai/local-ci/cliutils"
	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/internal/errgroup"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

// cliPrepper checks that the non-Go CLIs local-ci shells out to (gh, golangci-lint)
// are installed and new enough.
type cliPrepper struct {
	cmdExec *cmdrunner.CmdRunner
}

func (c *cliPrepper) GetName() string {
	return "CliPrepper"
}

func (c *cliPrepper) Execute(p *projectinfo.ProjectInfo) error {
	errGrp := errgroup.New()

	errGrp.Go(func() error {
		logger.DebugNoCtx("Checking gh cli version")
		return c.checkGhCliSetup()
	})
	// golangci-lint is only needed when the PR touches something that makes the
	// golangci_lint checker eligible (.go sources or a .golangci.* config), so a
	// docs- or JS-only PR is not blocked on having it installed. This precondition
	// must not be stricter than the check it guards. PrData is populated by
	// GithubDataPrepper, which runs before this prepper.
	if prNeedsGolangciLint(p) {
		errGrp.Go(func() error {
			logger.DebugNoCtx("Checking golangci-lint version")
			return c.checkGolangciLintSetup()
		})
	} else {
		logger.DebugNoCtx("No Go files in the PR; skipping golangci-lint setup check")
	}

	if err := errGrp.Wait(); err != nil {
		return fmt.Errorf("checking cli setup: %w", err)
	}
	return nil
}

// golangciConfigBasenames are the config files the golangci_lint checker triggers
// on in addition to .go sources; must stay in sync with that checker's file filter.
var golangciConfigBasenames = map[string]bool{
	".golangci.yml":  true,
	".golangci.yaml": true,
	".golangci.toml": true,
	".golangci.json": true,
}

// prNeedsGolangciLint reports whether the PR changes anything that makes the
// golangci_lint checker eligible — a .go file or a .golangci.* config — so the
// precondition is never stricter than the checker it guards (a config-only or
// rename-away-from-.go PR still runs the checker). It reads the PR file list
// fetched by GithubDataPrepper; a nil/empty list conservatively returns false so a
// PR that touches none of these never demands golangci-lint.
func prNeedsGolangciLint(p *projectinfo.ProjectInfo) bool {
	if p == nil || p.PrData == nil {
		return false
	}
	for _, f := range p.PrData.PrFiles {
		if f == nil {
			continue
		}
		// Both the current and the pre-rename name matter: a .go file renamed to a
		// non-.go name still makes the checker eligible via its previous name.
		for _, name := range []string{f.GetFilename(), f.GetPreviousFilename()} {
			if name == "" {
				continue
			}
			if strings.HasSuffix(name, ".go") || golangciConfigBasenames[filepath.Base(name)] {
				return true
			}
		}
	}
	return false
}

func (c *cliPrepper) Cleanup(p *projectinfo.ProjectInfo) error {
	return nil
}

func NewCliPrepper() (*cliPrepper, error) {
	cmdRunner, err := cmdrunner.NewCmdRunner(".", cmdrunner.WithNoLogger())
	if err != nil {
		return nil, fmt.Errorf("creating command runner: %w", err)
	}
	return &cliPrepper{
		cmdExec: cmdRunner,
	}, nil
}

func (c *cliPrepper) checkGhCliSetup() error {
	// Check cli present
	ghPresent, checkErr := cliutils.CheckCommandPresent(c.cmdExec, "gh")
	if checkErr != nil {
		return fmt.Errorf("checking gh cli present: %w", checkErr)
	}
	if !ghPresent {
		return errors.New("gh cli is not present. Install gh cli using `brew install gh` and login using `gh auth login`")
	}

	// Check version
	cliVersionOutput, cliVersionErr := c.cmdExec.ExecuteCommand("gh --version")
	if cliVersionErr != nil {
		return errors.Wrap(cliVersionErr, "error getting gh cli version")
	}
	versionRegex := regexp.MustCompile("gh version ([0-9.]*)")
	cliVersion := strings.Split(string(versionRegex.Find([]byte(cliVersionOutput))), " ")[2]
	if semver.Compare("v"+cliVersion, "v"+GhCliMinVersion) < 0 {
		return fmt.Errorf("expected gh cli version > %s, got: %s. Upgrade gh cli version using `brew upgrade gh`", GhCliMinVersion, cliVersion)
	}

	// Check login status
	ghAuthStatus, ghAuthErr := c.cmdExec.ExecuteCommand("gh auth status")
	if ghAuthErr != nil {
		return errors.Wrap(ghAuthErr, "error checking gh auth status")
	}
	if strings.Contains(ghAuthStatus, "Logged in") {
		return nil
	}
	return errors.New("gh cli is not logged in. Use `gh auth login` to login to your github account")
}

func (c *cliPrepper) checkGolangciLintSetup() error {
	currVersionStr, versionErr := c.cmdExec.ExecuteCommand("golangci-lint --version")
	if versionErr != nil {
		return errors.Wrap(versionErr, "error getting golangci-lint version")
	}

	// currVersionStr = golangci-lint has version 1.57.2 built with go1.21.3 from e3c2265f on 2023-11-03T12:59:25Z
	currVersion := strings.Split(strings.TrimPrefix(currVersionStr, "golangci-lint has version "), " ")[0]

	if semver.Compare("v"+currVersion, "v"+GolangciLintMinVersion) < 0 {
		return fmt.Errorf("expected golangci-lint version >= %s, got: %s. Install or upgrade it (`brew install golangci-lint` / `brew upgrade golangci-lint`, or see https://golangci-lint.run/welcome/install/)", GolangciLintMinVersion, currVersion)
	}
	return nil
}
