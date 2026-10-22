// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package postprocessor

import "github.com/tetriz-ai/local-ci/projectinfo"

type PostProcessor interface {
	GetName() string
	Execute(p *projectinfo.ProjectInfo) error

	// RunAlways this allows the postprocessor to run even if the checkers are failing
	RunAlways() bool
}

func GetPostProcessor() []PostProcessor {
	return []PostProcessor{
		NewGithubPostProcessor(),
		NewStoreRunDetailsPostProcessor(),
	}
}
