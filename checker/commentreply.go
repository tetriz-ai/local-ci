// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/pkg/errors"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/pkg/cmdlogger"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

const CommentReplyWFName = "comment_reply"

// codexBotLogin is the GitHub login of the Codex review bot. A review thread it
// roots is in scope for this gate even though it is a bot, unlike pure
// automation bots (github-actions, PR announcers) which never expect a reply.
const codexBotLogin = "chatgpt-codex-connector"

// commentReplyChecker fails the run when a reviewer's PR comment has no response
// from the author. It is a PREFLIGHT gate: it runs before the heavy checkers and
// aborts the run on failure, so an unanswered review comment is caught in
// seconds rather than after a full multi-minute local-ci run.
//
// "Responded" is deliberately lenient — a reply comment on the thread OR a
// resolved thread both satisfy it. The gate does not (and cannot reliably) judge
// reply quality; its job is that no reviewer feedback is silently missed. The
// failure message is what sets the bar for a substantive, grounded reply.
//
// Failure is reserved for the substantive case (a thread was fetched and is
// unanswered, or the PR exceeds the page the gate can inspect). Infrastructure
// problems — gh missing, an auth/network/API error, an unparseable response —
// degrade to a logged skip, never a run-aborting failure: the gate must not wedge
// all of local-ci over a transient GitHub hiccup, and local-ci already depends on GitHub
// being reachable via the prepper, so a real outage is caught there first.
type commentReplyChecker struct{}

func NewCommentReplyChecker() *commentReplyChecker {
	return &commentReplyChecker{}
}

func (c *commentReplyChecker) GetName() string {
	return CommentReplyWFName
}

func (c *commentReplyChecker) GetLciStampKey() string {
	return CommentReplyWFName
}

func (c *commentReplyChecker) GetWorkflowNames() []string {
	return []string{CommentReplyWFName}
}

// GetExecutionMode returns Preflight: the runner executes preflight checkers
// first and aborts before the serial/parallel checkers if one fails. Preflight
// checkers are also exempt from the stamp-based eligibility cache (they gate on
// external review state that changes without a new commit), so this gate is
// re-evaluated on every local-ci run, not skipped on an unchanged commit.
func (c *commentReplyChecker) GetExecutionMode() ExecutionMode {
	return Preflight
}

// GetFileFilter returns "**" so the gate runs on every PR regardless of which
// files changed — unanswered review feedback is independent of the diff.
func (c *commentReplyChecker) GetFileFilter(_ *projectinfo.ProjectInfo) []string {
	return []string{"**"}
}

func (c *commentReplyChecker) Cleanup(_ *projectinfo.ProjectInfo) error {
	return nil
}

func (c *commentReplyChecker) Execute(p *projectinfo.ProjectInfo) []*Report {
	if err := c.execute(p); err != nil {
		return []*Report{{Name: c.GetName(), Status: Failure, Err: err}}
	}
	return []*Report{{Name: c.GetName(), Status: Success}}
}

func (c *commentReplyChecker) execute(p *projectinfo.ProjectInfo) error {
	if skip, reason := preFetchSkip(p.UserInput.GhActionMode, p.UserInput.PrNumber); skip {
		logStep(c, "Skipping: "+reason)
		return nil
	}
	if _, err := exec.LookPath("gh"); err != nil {
		logger.Warn(fmt.Sprintf("[%s] gh CLI not found, skipping comment-reply check: %v", c.GetName(), err))
		return nil
	}

	cmdRunner, err := cmdrunner.NewCmdRunner(p.RepoPath, cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		// Cannot construct the runner — infrastructure, not a review-state
		// verdict. Skip rather than wedge the whole run.
		logger.Warn(fmt.Sprintf("[%s] could not create cmd runner, skipping: %v", c.GetName(), err))
		return nil
	}
	defer func() {
		if closeErr := cmdRunner.Close(); closeErr != nil {
			logger.ErrorNoCtx(fmt.Sprintf("closing command runner: %v", closeErr))
		}
	}()

	repoName := p.RepoName
	owner := p.RepoOwner
	if repoName == "" || owner == "" {
		logger.Warn(fmt.Sprintf("[%s] repo owner/name unavailable, skipping comment-reply check", c.GetName()))
		return nil
	}

	// The acting login (whoever runs local-ci) is counted as a valid responder
	// alongside the PR author, so a teammate driving someone else's PR can
	// answer threads too. Failure to resolve it is non-fatal.
	actingLogin := resolveActingLogin(c, cmdRunner)

	logStep(c, fmt.Sprintf("Fetching review threads for PR #%d", p.UserInput.PrNumber))
	pr, err := fetchPullRequestThreads(cmdRunner, owner, repoName, p.UserInput.PrNumber)
	if err != nil {
		// gh auth/network/API error or an unparseable/GraphQL-error response:
		// infrastructure, not a review-state verdict. Skip with a visible
		// warning rather than fail closed and abort the whole run.
		logger.Warn(fmt.Sprintf("[%s] could not fetch PR review threads, skipping: %v", c.GetName(), err))
		return nil
	}

	if skip, reason := postFetchSkip(pr.State, pr.Author.Login); skip {
		logStep(c, "Skipping: "+reason)
		return nil
	}

	responders := map[string]bool{pr.Author.Login: true}
	if actingLogin != "" {
		responders[actingLogin] = true
	}

	unanswered, threadsTruncated := findUnansweredThreads(pr, responders)
	if threadsTruncated {
		// The PR has more review threads than pagination covers. That is a limit
		// of this gate, not an author failure, so we do NOT fail on it — we warn
		// (visible, not silent) and still gate on whatever we did fetch. Any
		// unanswered thread found in the fetched pages below still fails.
		logger.Warn(fmt.Sprintf("[%s] PR #%d has more than %d review threads; checked the first %d and cannot confirm the rest — not failing on the unchecked remainder",
			c.GetName(), p.UserInput.PrNumber, maxThreadPages*100, maxThreadPages*100))
	}
	if len(unanswered) == 0 {
		logStep(c, "All checked reviewer comments have a response")
		return nil
	}

	return fmt.Errorf("%s", unansweredThreadsMessage(owner, repoName, p.UserInput.PrNumber, pr.URL, unanswered))
}

// preFetchSkip decides the skips knowable before any GitHub call: the gate is an
// author-facing local check (CI has no interactive author, so it skips under
// --gh-action — the local run's stamp is what merge requires), and it needs an
// open PR to have anything to check.
func preFetchSkip(ghActionMode bool, prNumber int) (skip bool, reason string) {
	switch {
	case ghActionMode:
		return true, "comment-reply gate is an author-facing local check (--gh-action)"
	case prNumber == 0:
		return true, "no PR number in context"
	default:
		return false, ""
	}
}

// postFetchSkip decides the skips knowable only after fetching the PR: a
// closed/merged PR has nothing to gate, and an unresolvable author login means
// the reply signal cannot be computed (skip rather than flag every thread).
func postFetchSkip(prState, prAuthorLogin string) (skip bool, reason string) {
	switch {
	case prState != "" && prState != "OPEN":
		return true, "PR is not open"
	case prAuthorLogin == "":
		return true, "PR author login unavailable"
	default:
		return false, ""
	}
}

// resolveActingLogin returns the login of whoever is running local-ci, or "" if it
// cannot be resolved (non-fatal — the PR author is still checked).
func resolveActingLogin(c *commentReplyChecker, cmdRunner *cmdrunner.CmdRunner) string {
	out, err := cmdRunner.ExecuteCommandArgs("gh", []string{"api", "user", "-q", ".login"}, "NO_COLOR=1")
	if err != nil {
		logStepD(c, fmt.Sprintf("could not resolve acting gh login: %v", err))
		return ""
	}
	return strings.TrimSpace(ansiEscapeRe.ReplaceAllString(out, ""))
}

// reviewThreadsQuery fetches the PR author plus a page of review threads with
// their resolution state and comment authors — the fields that determine whether
// the author has responded. Mirrors the shape used by PR-review tooling. Threads
// are paginated 100 at a time via $after (see fetchPullRequestThreads); comments
// are capped at 100 per thread with pageInfo.hasNextPage flagging the rare
// >100-comment overflow.
const reviewThreadsQuery = `query($owner:String!,$name:String!,$num:Int!,$after:String){
  repository(owner:$owner,name:$name){
    pullRequest(number:$num){
      author{ login }
      url
      state
      reviewThreads(first:100, after:$after){
        pageInfo{ hasNextPage endCursor }
        nodes{
          isResolved
          path
          line
          comments(first:100){
            pageInfo{ hasNextPage }
            nodes{ databaseId author{ login __typename } body }
          }
        }
      }
    }
  }
}`

// maxThreadPages caps thread pagination so a malformed cursor cannot loop
// forever. 20 pages = 2000 threads, far beyond any real PR; hitting the cap
// leaves HasNextPage true, which execute treats as a warning — it still gates on
// the threads it fetched and does not fail merely because the PR has more.
const maxThreadPages = 20

type ghAuthor struct {
	Login    string `json:"login"`
	Typename string `json:"__typename"`
}

type ghThreadComment struct {
	DatabaseID int64    `json:"databaseId"`
	Author     ghAuthor `json:"author"`
	Body       string   `json:"body"`
}

type ghReviewThread struct {
	IsResolved bool   `json:"isResolved"`
	Path       string `json:"path"`
	Line       *int   `json:"line"`
	Comments   struct {
		PageInfo pageInfo          `json:"pageInfo"`
		Nodes    []ghThreadComment `json:"nodes"`
	} `json:"comments"`
}

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type ghPullRequest struct {
	Author        ghAuthor `json:"author"`
	URL           string   `json:"url"`
	State         string   `json:"state"`
	ReviewThreads struct {
		PageInfo pageInfo         `json:"pageInfo"`
		Nodes    []ghReviewThread `json:"nodes"`
	} `json:"reviewThreads"`
}

type ghGraphQLError struct {
	Message string `json:"message"`
}

type ghGraphQLResp struct {
	Data struct {
		Repository struct {
			PullRequest ghPullRequest `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
	// Errors carries GraphQL logical errors. gh api can exit 0 while returning
	// these (bad repo/PR resolution, field errors), so they must be inspected —
	// a zero exit is not proof of a good response.
	Errors []ghGraphQLError `json:"errors"`
}

// fetchPullRequestThreads fetches the PR and ALL its review threads, paginating
// until GitHub reports no more pages so a PR with >100 threads is fully
// evaluated. The accumulated PR's reviewThreads.pageInfo.HasNextPage is true
// only if pagination was cut short — the safety cap was hit, or GitHub reported
// another page without a usable cursor. The caller (execute) treats that as a
// warning, not a failure, and still evaluates whatever threads were fetched.
func fetchPullRequestThreads(cmdRunner *cmdrunner.CmdRunner, owner, repoName string, prNumber int) (ghPullRequest, error) {
	var pr ghPullRequest
	cursor := ""
	for page := range maxThreadPages {
		resp, err := fetchReviewThreadPage(cmdRunner, owner, repoName, prNumber, cursor)
		if err != nil {
			return ghPullRequest{}, errors.Wrapf(err, "fetching review threads page %d", page+1)
		}
		pagePR := resp.Data.Repository.PullRequest
		if page == 0 {
			// PR-level fields (author, url, state) are identical on every page;
			// take them from the first and accumulate thread nodes onto it.
			pr = pagePR
		} else {
			pr.ReviewThreads.Nodes = append(pr.ReviewThreads.Nodes, pagePR.ReviewThreads.Nodes...)
		}
		pr.ReviewThreads.PageInfo = pagePR.ReviewThreads.PageInfo
		if !pagePR.ReviewThreads.PageInfo.HasNextPage {
			break
		}
		cursor = pagePR.ReviewThreads.PageInfo.EndCursor
		if cursor == "" {
			// hasNextPage true but no cursor to advance — treat as truncated
			// (fail closed) rather than loop forever on the same page.
			break
		}
	}
	return pr, nil
}

// fetchReviewThreadPage runs one gh api graphql call for a single page of review
// threads. cursor is the `after` value ("" for the first page). It inspects the
// GraphQL top-level errors array because gh api can exit 0 while returning
// logical errors.
func fetchReviewThreadPage(cmdRunner *cmdrunner.CmdRunner, owner, repoName string, prNumber int, cursor string) (ghGraphQLResp, error) {
	args := []string{
		"api", "graphql",
		"-f", "owner=" + owner,
		"-f", "name=" + repoName,
		"-F", fmt.Sprintf("num=%d", prNumber),
		"-f", "query=" + reviewThreadsQuery,
	}
	if cursor != "" {
		args = append(args, "-f", "after="+cursor)
	}

	out, err := cmdRunner.ExecuteCommandArgs("gh", args, "NO_COLOR=1")
	if err != nil {
		return ghGraphQLResp{}, errors.Wrap(err, "gh api graphql")
	}

	var resp ghGraphQLResp
	clean := ansiEscapeRe.ReplaceAllString(out, "")
	if err := json.Unmarshal([]byte(clean), &resp); err != nil {
		return ghGraphQLResp{}, errors.Wrap(err, "parsing graphql response")
	}
	if len(resp.Errors) > 0 {
		return ghGraphQLResp{}, fmt.Errorf("graphql returned errors: %s", firstGraphQLError(resp.Errors))
	}
	return resp, nil
}

func firstGraphQLError(errs []ghGraphQLError) string {
	if len(errs) == 0 {
		return ""
	}
	return errs[0].Message
}

// unansweredThread is a reviewer-rooted thread with no response from a responder.
type unansweredThread struct {
	Path        string
	Line        int
	RootAuthor  string
	RootComment int64
	Snippet     string
	Truncated   bool
}

// findUnansweredThreads returns the reviewer-rooted threads with no response.
//
// In scope: threads whose FIRST comment is by a human (__typename User) or the
// Codex bot. A thread rooted by a responder (the PR author / acting user) needs
// no self-response and is skipped. Responded = the thread is resolved OR any
// comment on it is by a responder. threadsTruncated is true when the PR has more
// review threads than pagination covered — the caller must not treat an empty
// result as proof the PR has no unanswered threads, only that there are none
// among the threads fetched; the caller warns about the gap rather than failing.
func findUnansweredThreads(pr ghPullRequest, responders map[string]bool) (unanswered []unansweredThread, threadsTruncated bool) {
	threadsTruncated = pr.ReviewThreads.PageInfo.HasNextPage
	for _, t := range pr.ReviewThreads.Nodes {
		if len(t.Comments.Nodes) == 0 {
			continue
		}
		root := t.Comments.Nodes[0]

		inScope := root.Author.Typename == "User" || root.Author.Login == codexBotLogin
		if !inScope {
			continue
		}
		if responders[root.Author.Login] {
			continue
		}
		if t.IsResolved {
			continue
		}

		responded := false
		for _, cm := range t.Comments.Nodes {
			if responders[cm.Author.Login] {
				responded = true
				break
			}
		}
		if responded {
			continue
		}

		line := 0
		if t.Line != nil {
			line = *t.Line
		}
		unanswered = append(unanswered, unansweredThread{
			Path:        t.Path,
			Line:        line,
			RootAuthor:  root.Author.Login,
			RootComment: root.DatabaseID,
			Snippet:     snippet(root.Body),
			Truncated:   t.Comments.PageInfo.HasNextPage,
		})
	}
	return unanswered, threadsTruncated
}

// snippet returns the first non-empty line of a comment body, trimmed to a
// readable length for the failure listing.
func snippet(body string) string {
	const maxLen = 100
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > maxLen {
			return line[:maxLen] + "…"
		}
		return line
	}
	return ""
}

// unansweredThreadsMessage is the instruction shown to the agent/author on
// failure. It sets the bar for a grounded reply — not a one-word ack — and
// gives a copy-paste reply command per thread. Called only when there is at
// least one unanswered thread; the over-cap case is a warn-and-pass in execute,
// not a failure.
func unansweredThreadsMessage(owner, repoName string, prNumber int, prURL string, unanswered []unansweredThread) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d review thread(s) on this PR have no response from you.\n\n", len(unanswered))
	b.WriteString("Every reviewer comment must get a response before merge — whether you address it, ")
	b.WriteString("or it is stale / not applicable / won't-fix. This ensures no feedback is silently missed.\n\n")
	b.WriteString("Do NOT reply with one-word acks (\"done\", \"ack\"). Write a grounded one-or-two-line reply: ")
	b.WriteString("say what changed and where, referencing the code and what this PR does — or, if you are not ")
	b.WriteString("acting on it, explain why in a line (stale because…, not applicable because…).\n\n")

	b.WriteString("Unanswered threads:\n")
	for _, t := range unanswered {
		loc := t.Path
		if t.Line > 0 {
			loc = fmt.Sprintf("%s:%d", t.Path, t.Line)
		}
		if loc == "" {
			loc = "(PR-level review thread)"
		}
		fmt.Fprintf(&b, "  • %s — %s: %q\n", loc, t.RootAuthor, t.Snippet)
		fmt.Fprintf(&b, "      reply:  gh api repos/%s/%s/pulls/%d/comments -F in_reply_to=%d -f body='<your grounded reply>'\n",
			owner, repoName, prNumber, t.RootComment)
		if t.Truncated {
			b.WriteString("      (this thread has >100 comments; a later reply may exist beyond the first page)\n")
		}
	}
	b.WriteString("\nA reply comment OR resolving the thread both count. After responding to each, re-run local-ci.\n")

	if prURL != "" {
		fmt.Fprintf(&b, "PR: %s\n", prURL)
	}
	return b.String()
}
