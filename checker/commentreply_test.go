// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAuthor = "pr-author"
	testActing = "acting-user"
	testHuman  = "human-reviewer"
)

// thread builds a review thread from a resolved flag and an ordered list of
// (login, typename) comment authors. The first entry is the thread root.
func thread(resolved bool, line *int, path string, authors ...[2]string) ghReviewThread {
	t := ghReviewThread{IsResolved: resolved, Path: path, Line: line}
	for i, a := range authors {
		t.Comments.Nodes = append(t.Comments.Nodes, ghThreadComment{
			DatabaseID: int64(1000 + i),
			Author:     ghAuthor{Login: a[0], Typename: a[1]},
			Body:       "review comment body",
		})
	}
	return t
}

func user(login string) [2]string { return [2]string{login, "User"} }
func bot(login string) [2]string  { return [2]string{login, "Bot"} }

func TestFindUnansweredThreads(t *testing.T) {
	t.Parallel()
	responders := map[string]bool{testAuthor: true, testActing: true}
	lineNum := 42

	tests := []struct {
		name           string
		threads        []ghReviewThread
		threadsHasNext bool
		wantUnanswered []string // root author logins of the expected unanswered threads
		wantTruncated  bool
	}{
		{
			name:           "codex thread unresolved with no author reply is unanswered",
			threads:        []ghReviewThread{thread(false, &lineNum, "a.go", bot(codexBotLogin))},
			wantUnanswered: []string{codexBotLogin},
		},
		{
			name:           "resolved codex thread is answered",
			threads:        []ghReviewThread{thread(true, &lineNum, "a.go", bot(codexBotLogin))},
			wantUnanswered: nil,
		},
		{
			name:           "codex thread with an author reply is answered",
			threads:        []ghReviewThread{thread(false, &lineNum, "a.go", bot(codexBotLogin), user(testAuthor))},
			wantUnanswered: nil,
		},
		{
			name:           "codex thread answered by the acting user is answered",
			threads:        []ghReviewThread{thread(false, &lineNum, "a.go", bot(codexBotLogin), user(testActing))},
			wantUnanswered: nil,
		},
		{
			name:           "human reviewer thread unanswered is unanswered",
			threads:        []ghReviewThread{thread(false, &lineNum, "a.go", user(testHuman))},
			wantUnanswered: []string{testHuman},
		},
		{
			name:           "thread rooted by the author needs no self-response",
			threads:        []ghReviewThread{thread(false, &lineNum, "a.go", user(testAuthor))},
			wantUnanswered: nil,
		},
		{
			name:           "non-codex automation bot thread is out of scope",
			threads:        []ghReviewThread{thread(false, &lineNum, "a.go", bot("github-actions"))},
			wantUnanswered: nil,
		},
		{
			name:           "empty thread is skipped",
			threads:        []ghReviewThread{thread(false, &lineNum, "a.go")},
			wantUnanswered: nil,
		},
		{
			name: "mix: two unanswered, one resolved, one author-replied",
			threads: []ghReviewThread{
				thread(false, &lineNum, "a.go", bot(codexBotLogin)),
				thread(true, &lineNum, "b.go", user(testHuman)),
				thread(false, &lineNum, "c.go", user(testHuman), user(testAuthor)),
				thread(false, &lineNum, "d.go", user(testHuman)),
			},
			wantUnanswered: []string{codexBotLogin, testHuman},
		},
		{
			name:           "threads truncated flag propagates",
			threads:        []ghReviewThread{thread(true, &lineNum, "a.go", bot(codexBotLogin))},
			threadsHasNext: true,
			wantUnanswered: nil,
			wantTruncated:  true,
		},
		{
			name:           "nil line yields line zero, still unanswered",
			threads:        []ghReviewThread{thread(false, nil, "a.go", user(testHuman))},
			wantUnanswered: []string{testHuman},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pr := ghPullRequest{Author: ghAuthor{Login: testAuthor, Typename: "User"}}
			pr.ReviewThreads.Nodes = tt.threads
			pr.ReviewThreads.PageInfo.HasNextPage = tt.threadsHasNext

			unanswered, truncated := findUnansweredThreads(pr, responders)

			var gotAuthors []string
			for _, u := range unanswered {
				gotAuthors = append(gotAuthors, u.RootAuthor)
			}
			assert.Equal(t, tt.wantUnanswered, gotAuthors)
			assert.Equal(t, tt.wantTruncated, truncated)
		})
	}
}

func TestFindUnansweredThreadsCarriesReplyTarget(t *testing.T) {
	t.Parallel()
	responders := map[string]bool{testAuthor: true}
	line := 7
	pr := ghPullRequest{Author: ghAuthor{Login: testAuthor}}
	pr.ReviewThreads.Nodes = []ghReviewThread{thread(false, &line, "svc/handler.go", bot(codexBotLogin))}

	unanswered, _ := findUnansweredThreads(pr, responders)

	require.Len(t, unanswered, 1)
	assert.Equal(t, int64(1000), unanswered[0].RootComment, "reply target is the root comment databaseId")
	assert.Equal(t, "svc/handler.go", unanswered[0].Path)
	assert.Equal(t, 7, unanswered[0].Line)
}

func TestPreFetchSkip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		ghActionMode bool
		prNumber     int
		wantSkip     bool
	}{
		{name: "gh-action mode skips (author-facing local check)", ghActionMode: true, prNumber: 42, wantSkip: true},
		{name: "no PR number skips", ghActionMode: false, prNumber: 0, wantSkip: true},
		{name: "open PR in local mode proceeds", ghActionMode: false, prNumber: 42, wantSkip: false},
		{name: "gh-action takes precedence even with a PR", ghActionMode: true, prNumber: 0, wantSkip: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			skip, reason := preFetchSkip(tt.ghActionMode, tt.prNumber)
			assert.Equal(t, tt.wantSkip, skip)
			if tt.wantSkip {
				assert.NotEmpty(t, reason)
			} else {
				assert.Empty(t, reason)
			}
		})
	}
}

func TestPostFetchSkip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		prState       string
		prAuthorLogin string
		wantSkip      bool
	}{
		{name: "open PR with author proceeds", prState: "OPEN", prAuthorLogin: "alice", wantSkip: false},
		{name: "empty state (field absent) with author proceeds", prState: "", prAuthorLogin: "alice", wantSkip: false},
		{name: "merged PR skips", prState: "MERGED", prAuthorLogin: "alice", wantSkip: true},
		{name: "closed PR skips", prState: "CLOSED", prAuthorLogin: "alice", wantSkip: true},
		{name: "missing author login skips (cannot compute reply signal)", prState: "OPEN", prAuthorLogin: "", wantSkip: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			skip, reason := postFetchSkip(tt.prState, tt.prAuthorLogin)
			assert.Equal(t, tt.wantSkip, skip)
			if tt.wantSkip {
				assert.NotEmpty(t, reason)
			} else {
				assert.Empty(t, reason)
			}
		})
	}
}

func TestSnippet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "first non-empty line", body: "\n\nfirst real line\nsecond", want: "first real line"},
		{name: "trims whitespace", body: "   padded   ", want: "padded"},
		{name: "empty body", body: "", want: ""},
		{name: "truncates long line", body: strings.Repeat("x", 150), want: strings.Repeat("x", 100) + "…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, snippet(tt.body))
		})
	}
}

func TestUnansweredThreadsMessageBar(t *testing.T) {
	t.Parallel()
	msg := unansweredThreadsMessage("example-org", "example-repo", 123, "https://github.com/example-org/example-repo/pull/123",
		[]unansweredThread{{Path: "a.go", Line: 10, RootAuthor: codexBotLogin, RootComment: 555, Snippet: "handle the nil case"}})

	// The instruction must push for a grounded reply, not a one-word ack, and
	// give a copy-paste reply command targeting the root comment.
	assert.Contains(t, msg, "one-word acks")
	assert.Contains(t, msg, "grounded")
	assert.Contains(t, msg, "in_reply_to=555")
	assert.Contains(t, msg, "a.go:10")
	assert.Contains(t, msg, "re-run local-ci")
}
