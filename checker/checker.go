// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package checker defines the Checker interface a host implements to add a check
// to local-ci, the eligibility filter that selects the checks a pull request needs,
// and the checks local-ci ships with.
package checker

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"github.com/samber/lo"

	"github.com/tetriz-ai/local-ci/checker/filter"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/projectinfo"
	stamperPkg "github.com/tetriz-ai/local-ci/stamper"
	"github.com/tetriz-ai/local-ci/workflow"
)

type ExecutionMode int

const (
	Serial = iota
	Parallel
	// Preflight checkers run before the serial/parallel checkers and abort the
	// run on failure, so a cheap fast-failing gate is caught in seconds rather
	// than after a full local-ci run.
	Preflight
)

// Checker is one check local-ci runs against a pull request. A host implements it and
// passes its set to runner.NewRunner; the engine filters the set by eligibility
// (file filter vs the diff, stamp staleness), runs each eligible checker, and
// records each result in the commit stamp. Execute returns one or more Reports; a
// Report with Status Failure carries the error shown to the developer.
//
// Contract: a Report's Name must equal this checker's GetLciStampKey(). The runner
// records a real result under Report.Name and a not-applicable pass under the stamp
// key; if they differ, the two land under different keys and the gate reads the
// wrong one. The built-in checkers keep GetName == GetLciStampKey == Report.Name.
type Checker interface {
	GetName() string
	GetLciStampKey() string
	GetWorkflowNames() []string
	Execute(p *projectinfo.ProjectInfo) []*Report
	Cleanup(p *projectinfo.ProjectInfo) error
	GetExecutionMode() ExecutionMode
	GetFileFilter(p *projectinfo.ProjectInfo) []string
}

type Report struct {
	Name   string
	Status ExecutionStatus
	Err    error
}

type ExecutionStatus string

const (
	Success ExecutionStatus = "success"
	Failure ExecutionStatus = "failure"
)

// Eligible filters the host-supplied checkers down to the set that should run
// for this PR. A checker is eligible when its file filter matches the PR diff
// and its prior stamp is stale; every Preflight checker is force-included
// regardless; then the user's --enable selection (if any) is applied. The caller
// supplies the checker set — the engine builds no fixed roster of its own, which
// is the plugin seam that lets any project register its own checks.
func Eligible(checkers []Checker, p *projectinfo.ProjectInfo) ([]Checker, error) {
	return eligibleWithStamper(stamperPkg.GetStamper(), checkers, p)
}

func eligibleWithStamper(stamper stamperPkg.Stamper, checkers []Checker, p *projectinfo.ProjectInfo) ([]Checker, error) {
	eligible, err := getFilteredCheckers(stamper, p, checkers)
	if err != nil {
		return nil, errors.Wrap(err, "filtering checkers")
	}

	// Record every eligible checker as an affected workflow so the stamper knows
	// which check statuses to recompute versus copy from the previous stamp.
	serialWfs, parallelWfs := convertCheckersToWorkflows(eligible, p)
	p.AffectedWorkflows = append(p.AffectedWorkflows, serialWfs...)
	p.AffectedWorkflows = append(p.AffectedWorkflows, parallelWfs...)

	// A cached pass may have removed an opted-out checker from eligible. It must
	// still be invalidated so disabling a required check cannot retain its pass.
	for _, c := range checkers {
		optedOut := lo.Contains(p.UserInput.DisabledWorkflows, c.GetName()) ||
			(len(p.UserInput.SelectiveWorkflows) > 0 && !lo.Contains(p.UserInput.SelectiveWorkflows, c.GetName()))
		if !optedOut || lo.ContainsBy(eligible, func(e Checker) bool { return e.GetName() == c.GetName() }) {
			continue
		}
		wf := workflow.New(c.GetName())
		wf.Jobs.RunChecks.With.LciStampKey = c.GetLciStampKey()
		p.AffectedWorkflows = append(p.AffectedWorkflows, wf)
		p.ExcludedAffectedWorkflows = append(p.ExcludedAffectedWorkflows, c.GetName())
	}

	var result []Checker
	for _, c := range eligible {
		// --disable turns a checker off by name: it does not run, and because it is
		// recorded as excluded (not applicable), the stamper leaves it out of the
		// stamp entirely rather than marking it passed. A gate requiring a disabled
		// check therefore fails — the honest outcome of opting out of a required one.
		if lo.Contains(p.UserInput.DisabledWorkflows, c.GetName()) {
			p.ExcludedAffectedWorkflows = append(p.ExcludedAffectedWorkflows, c.GetName())
			continue
		}
		if len(p.UserInput.SelectiveWorkflows) > 0 && !lo.Contains(p.UserInput.SelectiveWorkflows, c.GetName()) {
			p.ExcludedAffectedWorkflows = append(p.ExcludedAffectedWorkflows, c.GetName())
			continue
		}
		result = append(result, c)
	}
	return result, nil
}

func logStep(c Checker, log string) {
	logger.InfoNoCtx(fmt.Sprintf("[%s] %s", c.GetName(), log))
}

func logStepD(c Checker, log string) {
	logger.DebugNoCtx(fmt.Sprintf("[%s] %s", c.GetName(), log))
}

// getFilteredCheckers converts the supplied checkers to bare-bone workflows,
// filters them with the shared eligibility utility (file filter vs diff, stamp
// staleness), converts back to checkers, and force-includes every Preflight one.
func getFilteredCheckers(stamper stamperPkg.Stamper, p *projectinfo.ProjectInfo, checkers []Checker) ([]Checker, error) {
	checkerByName := lo.SliceToMap(checkers, func(c Checker) (string, Checker) {
		return c.GetName(), c
	})

	serialWfs, parallelWfs := convertCheckersToWorkflows(checkers, p)
	serialWfs, parallelWfs, filterErr := filter.FilterEligibleWorkflows(stamper, p, serialWfs, parallelWfs)
	if filterErr != nil {
		return nil, errors.Wrap(filterErr, "error filtering checker workflows")
	}

	var eligibleCheckers []Checker
	for _, wf := range serialWfs {
		if checker, ok := checkerByName[wf.FileName]; ok {
			eligibleCheckers = append(eligibleCheckers, checker)
		}
	}
	for _, wf := range parallelWfs {
		if checker, ok := checkerByName[wf.FileName]; ok {
			eligibleCheckers = append(eligibleCheckers, checker)
		}
	}

	return ensurePreflightCheckers(eligibleCheckers, checkers), nil
}

// ensurePreflightCheckers force-includes every Preflight checker regardless of
// the stamp-based eligibility filter. Preflight checkers gate on external state
// (e.g. GitHub review comments) that changes without a new commit, so the
// stamp cache — which drops a checker that passed for the current commit — would
// wrongly skip them on a re-run of the same commit and let post-stamp feedback
// through. Their own Execute still no-ops when there is nothing to check.
func ensurePreflightCheckers(eligible []Checker, all []Checker) []Checker {
	present := lo.SliceToMap(eligible, func(c Checker) (string, bool) {
		return c.GetName(), true
	})
	for _, c := range all {
		if c.GetExecutionMode() == Preflight && !present[c.GetName()] {
			eligible = append(eligible, c)
		}
	}
	return eligible
}

// convertCheckersToWorkflows converts checkers to bare-bone serial and parallel workflows with filename and path filter
func convertCheckersToWorkflows(checkers []Checker, p *projectinfo.ProjectInfo) ([]*workflow.Workflow, []*workflow.Workflow) {
	var serialWfs []*workflow.Workflow
	var parallelWfs []*workflow.Workflow

	for _, checker := range checkers {
		wf := workflow.New(checker.GetName())
		fileFilter := strings.Builder{}
		for _, ff := range checker.GetFileFilter(p) {
			fmt.Fprintf(&fileFilter, "- '%s'\n", ff)
		}
		wf.Jobs.RunChecks.With.PathsToTrigger = fmt.Sprintf("impacted_paths:\n%s", fileFilter.String())
		wf.Jobs.RunChecks.With.LciStampKey = checker.GetLciStampKey()

		// Preflight checkers are bucketed with serial for eligibility mapping;
		// the runner dispatches them by their actual execution mode.
		if checker.GetExecutionMode() == Parallel {
			parallelWfs = append(parallelWfs, wf)
		} else {
			serialWfs = append(serialWfs, wf)
		}
	}

	return serialWfs, parallelWfs
}
