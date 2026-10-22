// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package stamper records each checker's pass/fail for a commit as a git note
// and reads it back, so a merge gate can skip checks already run locally.
package stamper

import (
	"sync"

	"github.com/tetriz-ai/local-ci/projectinfo"
)

var (
	once             sync.Once
	singletonStamper Stamper
)

// Stamper takes the data from checker, prepper and stamps the current state of the project with the result
//
//go:generate go tool mockgen -source=stamper.go -destination=../internal/stamper/mocks/stamper.go -package=mocks
type Stamper interface {
	Stamp(p *projectinfo.ProjectInfo) error
	RetrieveLastValidStamp(p *projectinfo.ProjectInfo) (*projectinfo.StampInfo, error)
}

func GetStamper() Stamper {
	once.Do(func() {
		singletonStamper = &NotesStamper{}
	})
	return singletonStamper
}
