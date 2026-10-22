// Command custom-checker shows the local-ci plugin seam: a project defines its own
// check by implementing checker.Checker and hands it to the engine. Here the
// custom check fails a pull request whose diff adds a Go file whose name marks
// it as work-in-progress (a "_wip.go" suffix). The program runs the checker
// against two synthetic diffs so the pass and fail paths are both visible, with
// no GitHub access, and prints a stable result.
//
// To ship this check in your own build of local-ci, pass it alongside (or instead of)
// the built-ins to runner.NewRunner:
//
//	checkers := []checker.Checker{wipChecker{}, checker.NewGovulncheckChecker()}
//	runner.NewRunner(userInput, version, checkers).Run()
package main

import (
	"fmt"
	"strings"

	"github.com/tetriz-ai/local-ci/checker"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

// wipChecker fails when the PR adds a Go file whose name ends in "_wip.go".
type wipChecker struct{}

func (wipChecker) GetName() string        { return "no_wip_files" }
func (wipChecker) GetLciStampKey() string { return "no_wip_files" }
func (wipChecker) GetWorkflowNames() []string {
	return []string{"no_wip_files"}
}

// GetFileFilter returns the globs that make this checker eligible. "**" means it
// is considered on every PR; the engine still only runs it when the diff matches.
func (wipChecker) GetFileFilter(*projectinfo.ProjectInfo) []string { return []string{"**"} }

// GetExecutionMode places the checker in the parallel phase.
func (wipChecker) GetExecutionMode() checker.ExecutionMode { return checker.Parallel }

// Cleanup has nothing to undo.
func (wipChecker) Cleanup(*projectinfo.ProjectInfo) error { return nil }

// Execute returns a failing report when any newly added file ends in "_wip.go".
func (c wipChecker) Execute(p *projectinfo.ProjectInfo) []*checker.Report {
	var offenders []string
	if p.DiffData != nil && p.DiffData.OverallFilesChanges != nil {
		for _, f := range p.DiffData.OverallFilesChanges.NewFiles {
			if strings.HasSuffix(f, "_wip.go") {
				offenders = append(offenders, f)
			}
		}
	}
	if len(offenders) > 0 {
		return []*checker.Report{{
			Name:   c.GetName(),
			Status: checker.Failure,
			Err:    fmt.Errorf("remove work-in-progress files before merge: %s", strings.Join(offenders, ", ")),
		}}
	}
	return []*checker.Report{{Name: c.GetName(), Status: checker.Success}}
}

func main() {
	c := wipChecker{}

	clean := &projectinfo.ProjectInfo{DiffData: &projectinfo.DiffData{
		OverallFilesChanges: &projectinfo.AffectedFiles{NewFiles: []string{"pkg/service/handler.go"}},
	}}
	dirty := &projectinfo.ProjectInfo{DiffData: &projectinfo.DiffData{
		OverallFilesChanges: &projectinfo.AffectedFiles{NewFiles: []string{"pkg/service/handler.go", "pkg/service/scratch_wip.go"}},
	}}

	printResult("clean diff", c.Execute(clean))
	printResult("diff with a _wip.go file", c.Execute(dirty))
}

func printResult(label string, reports []*checker.Report) {
	r := reports[0]
	if r.Status == checker.Success {
		fmt.Printf("%s: PASS\n", label)
		return
	}
	fmt.Printf("%s: FAIL — %v\n", label, r.Err)
}
