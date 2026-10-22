// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package depsglob

import (
	"fmt"
	"path/filepath"
)

type IFileGlobFilter interface {
	Glob(pattern string) (matches []string, err error)
}

type filepathGlobFilter struct{}

func (f *filepathGlobFilter) Glob(pattern string) ([]string, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing pattern %q: %w", pattern, err)
	}
	return matches, nil
}

func NewFilePathGlobFilter() IFileGlobFilter {
	return &filepathGlobFilter{}
}
