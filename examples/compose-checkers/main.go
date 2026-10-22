// Command compose-checkers shows how a project assembles the checker set it will
// hand to runner.NewRunner. It lists the built-in checkers alongside a custom one,
// printing each checker's name and the file globs that make it eligible — the
// same Checker interface the engine reads. This is how you inspect, add to, or
// trim the set before passing it to the runner. No GitHub access is needed.
package main

import (
	"fmt"
	"strings"

	"github.com/tetriz-ai/local-ci/checker"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

// docCheckerExample fails a PR that changes a *.md file without also touching CHANGELOG.md.
type docCheckerExample struct{}

func (docCheckerExample) GetName() string                         { return "changelog_updated" }
func (docCheckerExample) GetLciStampKey() string                  { return "changelog_updated" }
func (docCheckerExample) GetWorkflowNames() []string              { return []string{"changelog_updated"} }
func (docCheckerExample) GetExecutionMode() checker.ExecutionMode { return checker.Parallel }
func (docCheckerExample) Cleanup(*projectinfo.ProjectInfo) error  { return nil }
func (docCheckerExample) GetFileFilter(*projectinfo.ProjectInfo) []string {
	return []string{"**/*.md"}
}
func (docCheckerExample) Execute(*projectinfo.ProjectInfo) []*checker.Report {
	return []*checker.Report{{Name: "changelog_updated", Status: checker.Success}}
}

func main() {
	// This mirrors the built-in set in the local-ci CLI's defaultCheckers(); keep the
	// two in sync when adding or removing a built-in.
	set := []checker.Checker{
		checker.NewCommentReplyChecker(),
		checker.NewIssueLinkerChecker(),
		checker.NewProtoBreakingChecker(),
		checker.NewBufLintCommentsChecker(),
		checker.NewReadmeChecker(),
		checker.NewGovulncheckChecker(),
		checker.NewPnpmAuditChecker(),
		checker.NewLockfileSyncChecker(),
		checker.NewGolangciLintChecker(),
		docCheckerExample{}, // your own check, added to the set
	}

	fmt.Println("checker set (name — file globs):")
	for _, c := range set {
		fmt.Printf("  %-18s %s\n", c.GetName(), strings.Join(c.GetFileFilter(nil), ", "))
	}
}
