// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prepper

import (
	"fmt"

	"github.com/tetriz-ai/local-ci/projectinfo"
)

// Prepper prepares and vets the project before the checkers are run
// It can do task ranging from checking dependency version, getting common info to be passed to Checker, to cleaning workspace
type Prepper interface {
	GetName() string
	Execute(p *projectinfo.ProjectInfo) error
	Cleanup(p *projectinfo.ProjectInfo) error
}

func GetPreppers() ([]Prepper, error) {
	cliPrepper, err := NewCliPrepper()
	if err != nil {
		return nil, fmt.Errorf("creating cli prepper: %w", err)
	}
	return []Prepper{
		// have stash as first prepper to always ensure clean workspace
		NewStashPrepper(),
		NewGithubDataPrepper(),
		cliPrepper,
		NewGitDataPrepper(),
		NewDiffPrepper(),
		NewRunHistoryPrepper(),
	}, nil
}
