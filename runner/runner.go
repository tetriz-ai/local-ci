// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package runner drives one local-ci validation of a pull request: it runs the
// preppers, filters the host's checker set to the eligible checks, runs them in
// their phases, stamps the commit, and runs the postprocessors.
package runner

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/enescakir/emoji"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	checkerPkg "github.com/tetriz-ai/local-ci/checker"
	"github.com/tetriz-ai/local-ci/internal/goroutine"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/internal/syncmap"
	"github.com/tetriz-ai/local-ci/internal/waitgroup"
	postProcessorPkg "github.com/tetriz-ai/local-ci/postprocessor"
	prepperPkg "github.com/tetriz-ai/local-ci/prepper"
	"github.com/tetriz-ai/local-ci/projectinfo"
	stamperPkg "github.com/tetriz-ai/local-ci/stamper"
)

// ctxKey is a private type for context keys to avoid collisions with keys
// defined in other packages (staticcheck SA1029).
type ctxKey string

const checkerCtxKey ctxKey = "checker"

type Runner struct {
	userInput   *projectinfo.UserInput
	toolVersion string
	// checkers is the host-supplied checker set; the engine builds no fixed
	// roster of its own. checker.Eligible filters this down to the applicable set.
	checkers []checkerPkg.Checker
}

type cleanupFn interface {
	Cleanup(p *projectinfo.ProjectInfo) error
	GetName() string
}

// NewRunner returns a Runner that validates a pull request with the given
// host-supplied checker set and stamps the head commit with the results. The
// engine holds no fixed checker list; the caller owns the set.
func NewRunner(userInput *projectinfo.UserInput, toolVersion string, checkers []checkerPkg.Checker) *Runner {
	return &Runner{userInput: userInput, toolVersion: toolVersion, checkers: checkers}
}

func (r *Runner) Run() error {
	projInfo, err := projectinfo.NewProjectInfo(r.userInput, r.toolVersion)
	if err != nil {
		return errors.Wrap(err, "error creating project info")
	}

	var cleanupQueue []cleanupFn
	defer func() {
		// Perform cleanup in the reverse order
		queueLen := len(cleanupQueue)
		for i := queueLen - 1; i >= 0; i-- {
			logger.DebugNoCtx("====================Executing cleanup for: " + cleanupQueue[i].GetName())
			if err := cleanupQueue[i].Cleanup(projInfo); err != nil {
				logger.ErrorNoCtx("Error cleaning up: "+cleanupQueue[i].GetName(), zap.Error(err))
			}
		}
	}()

	// Get all preppers
	allPreppers, err := prepperPkg.GetPreppers()
	if err != nil {
		logger.ErrorNoCtx("Error getting Preppers")
		return errors.Wrap(err, "error getting Preppers")
	}

	// Execute all preppers
	for _, prepper := range allPreppers {
		cleanupQueue = append(cleanupQueue, prepper)
		t := time.Now()
		logger.InfoNoCtx("====================Executing prepper: " + prepper.GetName())
		if err := prepper.Execute(projInfo); err != nil {
			logger.DebugNoCtx(fmt.Sprintf("====================Completed prepper: %s in %s", prepper.GetName(), time.Since(t).String()))
			return errors.Wrap(err, "Error executing prepper: "+prepper.GetName())
		}
		logger.DebugNoCtx(fmt.Sprintf("====================Completed prepper: %s in %s", prepper.GetName(), time.Since(t).String()))

	}

	// Filter the host-supplied checkers down to the ones eligible for this PR.
	allCheckers, err := checkerPkg.Eligible(r.checkers, projInfo)
	if err != nil {
		return errors.Wrap(err, "error selecting eligible checkers")
	}
	var allWorkflowNames []string
	for _, checker := range allCheckers {
		allWorkflowNames = append(allWorkflowNames, checker.GetWorkflowNames()...)
	}

	logger.InfoNoCtx("Starting workflows: " + strings.Join(allWorkflowNames, ", "))
	if projInfo.UserInput.DryRunMode {
		logger.InfoNoCtx("Running in dry run mode, exiting without starting workflows")
		return nil
	}

	eventsMap := &syncmap.Map[string, []*checkerPkg.Report]{}

	// Preflight gate: run cheap remote checks (e.g. unanswered review comments)
	// first. On failure, skip the heavy serial/parallel checkers so the gate is
	// surfaced in seconds rather than after a full run — but still fall through to
	// stamping and the RunAlways postprocessors below, so the preflight checker's
	// failure is recorded on the commit (invalidating any stale success stamp)
	// and run history is captured, exactly like a normal checker failure.
	preflightQ, preflightFailed := r.executePreflightCheckers(allCheckers, projInfo, eventsMap)
	cleanupQueue = append(cleanupQueue, preflightQ...)
	if preflightFailed {
		logger.InfoNoCtx(fmt.Sprintf("%sPreflight gate failed — skipping the remaining checkers", emoji.CrossMark))
	} else {
		q := r.executeSerialCheckers(allCheckers, projInfo, eventsMap)
		cleanupQueue = append(cleanupQueue, q...)
		q = r.executeParallelCheckers(allCheckers, projInfo, eventsMap)
		cleanupQueue = append(cleanupQueue, q...)
		logger.InfoNoCtx("Completed running all checkers")
	}

	checkerErrMessage := buildCheckerErrMessage(eventsMap)

	applyCheckerStatuses(projInfo, eventsMap)

	// Record checks that did not apply to this PR as vacuously passed, so a gate
	// that requires one can still go green on a PR that does not exercise it. Keyed
	// off the eligible set (allCheckers), never off whether a result was produced —
	// see the function doc for why an eligible-but-incomplete check must stay absent.
	r.recordNotApplicableCheckers(projInfo, allCheckers)

	// Stamp with local run
	stamper := stamperPkg.GetStamper()
	if err := stamper.Stamp(projInfo); err != nil {
		return errors.Wrap(err, "error stamping commit with checks status")
	}
	logger.InfoNoCtx("Successfully stamped commit")
	logger.DebugNoCtx("Checker status: ", zap.Any("stamp:", projInfo.CheckerStatus))

	var lciRunErrMsg strings.Builder
	if checkerErrMessage == "" {
		logger.InfoNoCtx(fmt.Sprintf("Specified checkers ran successfully %s%s%s", emoji.CheckMarkButton, emoji.CheckMarkButton, emoji.CheckMarkButton))
	} else {
		logger.InfoNoCtx(fmt.Sprintf("%sSome checkers failed to run. Run `local-ci --pr=%d --dry-run` to view list of not run/non passing eligible workflows to be run on the PR", emoji.CrossMark, projInfo.UserInput.PrNumber))
		fmt.Fprintf(&lciRunErrMsg, "checkers execution failed:\n%s\n", checkerErrMessage)
	}
	// Run post processors
	var postProcessorErrMsg strings.Builder
	postprocessors := postProcessorPkg.GetPostProcessor()
	for _, postprocessor := range postprocessors {
		if checkerErrMessage != "" && !postprocessor.RunAlways() {
			continue
		}
		t := time.Now()
		logger.InfoNoCtx("====================Executing postprocessor: " + postprocessor.GetName())
		if ppErr := postprocessor.Execute(projInfo); ppErr != nil {
			logger.DebugNoCtx(fmt.Sprintf("====================Completed postprocessor: %s in %s", postprocessor.GetName(), time.Since(t).String()))
			fmt.Fprintf(&postProcessorErrMsg, "Error executing postprocessor %s: %v", postprocessor.GetName(), ppErr)
		}

		logger.DebugNoCtx(fmt.Sprintf("====================Completed postprocessor: %s in %s", postprocessor.GetName(), time.Since(t).String()))

	}

	if postProcessorErrMsg.Len() != 0 {
		logger.ErrorNoCtx("some post processor failed to run ")
		fmt.Fprintf(&lciRunErrMsg, "post processor execution failed :\n%s\n", postProcessorErrMsg.String())
	}
	if checkerErrMessage != "" || postProcessorErrMsg.Len() != 0 {
		return fmt.Errorf("error in local-ci execution: %s", lciRunErrMsg.String())
	}

	return nil
}

// applyCheckerStatuses folds every checker's reports in eventsMap into the per-key
// pass/fail map that is stamped on the commit. A single checker may emit several
// reports under one stamp key (Checker.Execute returns a slice), and they are
// aggregated failure-dominant: a key is stamped passed only if every report for it
// succeeded. A plain last-write-wins assignment lets a trailing Success overwrite an
// earlier Failure for the same key and stamp a green gate over a failed subcheck.
func applyCheckerStatuses(projInfo *projectinfo.ProjectInfo, eventsMap *syncmap.Map[string, []*checkerPkg.Report]) {
	eventsMap.Range(func(_ string, events []*checkerPkg.Report) bool {
		for _, event := range events {
			pass := event.Status == checkerPkg.Success
			if prev, seen := projInfo.CheckerStatus[event.Name]; seen {
				pass = prev && pass
			}
			projInfo.CheckerStatus[event.Name] = pass
		}
		return true
	})
}

// recordNotApplicableCheckers marks a host checker as vacuously passed in the
// stamp only when it was never eligible for this PR.
//
// Without this, a required gate check that does not apply to a PR — its file
// filter never matched, so checker.Eligible dropped it — is absent from the stamp,
// which the gate reads as "not passed"; the gate could then never go green on, say,
// a docs-only PR guarded by a govulncheck gate.
//
// Eligibility, not "did it produce a result", is the test on purpose. An eligible
// checker is expected to run, and when it does not — a preflight failure skips the
// heavy phases, a phase times out, or a checker panics — its result is simply
// absent, and the gate correctly reads that as "not passed". Treating absence as a
// pass there would stamp an unrun required check green, erase an earlier real
// failure the stamper carries forward, and let the next run skip it as already
// passing: a silent gate bypass. So the only checkers recorded here are those not
// in the eligible set and not opted out, i.e. not applicable to the diff or already
// covered by a fresh passing stamp. A checker the user excluded (--enable) or
// disabled (--disable) is left absent on purpose, because the user chose not to run
// it and local-ci must not assert a pass it never computed.
func (r *Runner) recordNotApplicableCheckers(projInfo *projectinfo.ProjectInfo, eligible []checkerPkg.Checker) {
	eligibleByName := make(map[string]bool, len(eligible))
	for _, c := range eligible {
		eligibleByName[c.GetName()] = true
	}
	selective := make(map[string]bool, len(projInfo.UserInput.SelectiveWorkflows))
	for _, name := range projInfo.UserInput.SelectiveWorkflows {
		selective[name] = true
	}
	disabled := make(map[string]bool, len(projInfo.UserInput.DisabledWorkflows))
	for _, name := range projInfo.UserInput.DisabledWorkflows {
		disabled[name] = true
	}
	for _, c := range r.checkers {
		// Eligible this run (ran, or should have): never vacuously passed — an absent
		// result means it did not complete, which the gate must read as not passed.
		if eligibleByName[c.GetName()] {
			continue
		}
		// Opted out by the user: left absent so a gate requiring it fails honestly.
		if disabled[c.GetName()] {
			continue
		}
		// Under an explicit --enable selection, only selected checkers may be stamped;
		// an unselected one is scoped out by the user, applicable or not.
		if len(selective) > 0 && !selective[c.GetName()] {
			continue
		}
		logger.InfoNoCtx(fmt.Sprintf("Check %q does not apply to this PR; recording it as passed in the stamp", c.GetLciStampKey()))
		projInfo.CheckerStatus[c.GetLciStampKey()] = true
	}
}

// buildCheckerErrMessage formats every Failure report in eventsMap into the
// runner's canonical "❌failure in <key> checker" block shown to the developer.
func buildCheckerErrMessage(eventsMap *syncmap.Map[string, []*checkerPkg.Report]) string {
	var msg strings.Builder
	eventsMap.Range(func(key string, events []*checkerPkg.Report) bool {
		for _, event := range events {
			if event.Status != checkerPkg.Success {
				fmt.Fprintf(&msg, "%sfailure in %s checker: Report: %s \n\tError:%s\n", emoji.CrossMark, key, event.Name, event.Err)
			}
		}
		return true
	})
	return msg.String()
}

// executePreflightCheckers runs the Preflight-mode checkers serially, before the
// serial/parallel phases. It returns whether any preflight report failed so the
// caller can abort the run before the heavy checkers execute.
func (r *Runner) executePreflightCheckers(allCheckers []checkerPkg.Checker, projInfo *projectinfo.ProjectInfo, eventsMap *syncmap.Map[string, []*checkerPkg.Report]) (cleanupQueue []cleanupFn, failed bool) {
	for _, checker := range allCheckers {
		if checker.GetExecutionMode() != checkerPkg.Preflight {
			continue
		}
		cleanupQueue = append(cleanupQueue, checker)
		t := time.Now()
		logger.InfoNoCtx("====================Executing preflight checker: " + checker.GetName())
		events := checker.Execute(projInfo)
		logger.InfoNoCtx(fmt.Sprintf("====================Completed preflight checker: %s in %s", checker.GetName(), time.Since(t).String()))
		eventsMap.Store(checker.GetLciStampKey(), events)
		for _, event := range events {
			if event.Status != checkerPkg.Success {
				failed = true
			}
		}
	}
	return cleanupQueue, failed
}

func (r *Runner) executeSerialCheckers(allCheckers []checkerPkg.Checker, projInfo *projectinfo.ProjectInfo, eventsMap *syncmap.Map[string, []*checkerPkg.Report]) (cleanupQueue []cleanupFn) {
	for _, checker := range allCheckers {
		// Only enqueue cleanup for checkers this phase actually runs, so each
		// checker's Cleanup runs exactly once across the whole run.
		if checker.GetExecutionMode() != checkerPkg.Serial {
			continue
		}
		cleanupQueue = append(cleanupQueue, checker)
		t := time.Now()

		logger.InfoNoCtx("====================Executing checkers: " + checker.GetName())
		events := checker.Execute(projInfo)
		logger.InfoNoCtx(fmt.Sprintf("====================Completed checker: %s in %s", checker.GetName(), time.Since(t).String()))
		eventsMap.Store(checker.GetLciStampKey(), events)
	}
	return cleanupQueue
}

func (r *Runner) executeParallelCheckers(allCheckers []checkerPkg.Checker, projInfo *projectinfo.ProjectInfo, eventsMap *syncmap.Map[string, []*checkerPkg.Report]) (cleanupQueue []cleanupFn) {
	var wg sync.WaitGroup
	for _, checker := range allCheckers {
		// Only enqueue cleanup for checkers this phase actually runs, so each
		// checker's Cleanup runs exactly once across the whole run.
		if checker.GetExecutionMode() != checkerPkg.Parallel {
			continue
		}
		// Prevent modifying reference of checker for parallel execution
		checker := checker
		cleanupQueue = append(cleanupQueue, checker)
		t := time.Now()

		logger.InfoNoCtx("====================Executing checkers: " + checker.GetName())
		ctx := context.WithValue(context.Background(), checkerCtxKey, checker.GetName())
		wg.Add(1)
		goroutine.RunWithCtx(ctx, func(ctx context.Context) {
			defer wg.Done()
			events := checker.Execute(projInfo)
			logger.InfoNoCtx(fmt.Sprintf("====================Completed checker: %s in %s", checker.GetName(), time.Since(t).String()))
			eventsMap.Store(checker.GetLciStampKey(), events)
		})
	}
	// set a timeout of 90 minutes for all checkers to complete as a max limit considering slow machines
	waitgroup.SafeWait(&wg, time.Minute*90)
	return cleanupQueue
}
