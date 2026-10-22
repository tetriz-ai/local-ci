// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package workflow models the minimal "workflow" shape the eligibility filter
// works with: a named unit carrying the file-glob triggers and the stamp key
// for one check. Each checker is converted to one of these so the shared filter
// can decide, from the PR diff and the prior stamp, whether the check must run.
package workflow

// Workflow is the bare-bones representation of a check used by the eligibility
// filter. FileName is the checker's name; the nested With carries its trigger
// globs (PathsToTrigger) and its stamp key (LciStampKey).
type Workflow struct {
	FileName string
	Jobs     struct {
		RunChecks struct {
			With struct {
				PathsToTrigger string
				LciStampKey    string
			}
		}
	}
}

// GetLciStampKey returns the stamp key this workflow's result is recorded under.
func (w *Workflow) GetLciStampKey() string {
	return w.Jobs.RunChecks.With.LciStampKey
}

// New returns an empty Workflow named filename.
func New(filename string) *Workflow {
	return &Workflow{FileName: filename}
}
