# local-ci architecture

How local-ci works inside: the checker system, the run lifecycle, and how to add a checker. Read this
before tracing the code from scratch. The user-facing side lives in the [README](README.md); this
document is the internals.

## What local-ci is

`local-ci` is a compiled Go binary (`cmd/local-ci`). It validates an open pull request — runs the checks the
diff triggers — and then **stamps** the head commit with which checks passed, so a GitHub merge
gate can skip redundant work. The stamp is **SHA-specific**: a commit pushed after the stamp
leaves the new head unstamped, so local-ci must be the last thing run before requesting merge.

The engine holds no fixed check list. A host program supplies its checker set to
`runner.NewRunner`, which is the plugin seam. `cmd/local-ci` is one such host; it builds its set in
`defaultCheckers()`.

## The run lifecycle

`main.go` -> `cmd/local-ci` (cobra, flag parsing) -> `runner.NewRunner(userInput, version, checkers)` ->
`runner.Run`. `Run` executes these phases in order:

1. **Project info** — `projectinfo.NewProjectInfo` builds the `ProjectInfo` carried through the
   whole run (repo paths, user input, and — once preppers fill them — `PrData`, `DiffData`, the
   last stamp).
2. **Preppers** (`prepper/`) — run first, in sequence. `githubDataPrepper` populates
   `ProjectInfo.PrData` (base/head branch, latest commit, all commit SHAs, PR files) from GitHub.
   Checkers read `PrData` rather than re-fetching. A prepper failure aborts the run.
3. **Checker selection** — `checker.Eligible(checkers, p)` filters the host's set down to the ones
   eligible for this PR (see "Which checkers run").
4. **Preflight checkers** — the `Preflight`-mode checkers run serially, **before** the heavy
   phases, and **abort the run on failure**. This is the fast-fail path: a cheap remote gate (for
   example `comment_reply`, the unanswered-review-comment gate) surfaces in seconds instead of
   after a full multi-minute run.
5. **Serial checkers** — `Serial`-mode checkers run one at a time.
6. **Parallel checkers** — `Parallel`-mode checkers run concurrently under a 90-minute cap.
7. **Stamp** — the stamper records each checker's pass/fail on the head commit.
8. **Postprocessors** (`postprocessor/`) — run last. Ones that are not `RunAlways` are skipped
   when a checker failed.

Outside the preflight abort, phases 5–6 do **not** short-circuit: every serial then every parallel
checker runs, all reports collect into an events map, and only then is pass/fail decided. A
failing serial checker does not stop the others.

## The Checker interface

A checker is a Go type in `checker/` implementing (`checker/checker.go`):

```go
type Checker interface {
    GetName() string
    GetLciStampKey() string
    GetWorkflowNames() []string
    Execute(p *projectinfo.ProjectInfo) []*Report
    Cleanup(p *projectinfo.ProjectInfo) error
    GetExecutionMode() ExecutionMode      // Serial | Parallel | Preflight
    GetFileFilter(p *projectinfo.ProjectInfo) []string
}
```

`Execute` returns `[]*Report`. A `Report` with `Status: Failure` carries an `Err`, and **that
error is the message shown to the developer** — the runner formats it into a failure block. So a
checker "instructs the developer" simply by returning a rich, actionable error (see
`issuelinker.go` for a clean example).

### Execution modes

- **`Serial`** — run one at a time, in the serial phase.
- **`Parallel`** — run concurrently in the parallel phase (the default for anything not cheap
  enough to gate on).
- **`Preflight`** — run first, serially, and abort the whole run on failure. Reserve this for
  checks that are both **cheap** (seconds) and worth failing the entire run before the heavy
  checkers spend minutes.

### GetFileFilter and eligibility

`GetFileFilter` returns glob patterns. A checker is **eligible** for a PR only when its filter
matches the PR's changed files (and its prior stamp is stale). Return `["**"]` to always run
regardless of the diff — this is what `issue_linker` and `comment_reply` do, because their concern
(a linked issue, an answered comment) is independent of which files changed.

## Which checkers run

Two layers decide the final set:

1. **The host's checker set** — the `[]checker.Checker` passed to `runner.NewRunner`. The engine
   builds none of its own.
2. **Eligibility filter** — `checker.Eligible` converts each checker to a bare workflow keyed on
   its `GetFileFilter`, and `filter.FilterEligibleWorkflows` drops any whose filter does not match
   the PR diff (and whose prior stamp is still valid). Every `Preflight` checker is force-included
   regardless. Then the `--enable` selection is applied.

Consequences worth knowing:

- **A bare `local-ci --pr <N>`** runs every eligible checker; `--enable-workflows=<name>` runs a subset;
  `--enable-workflows=all` forces every eligible one.
- **`local-ci --dry-run`** prints the eligible list without executing.
- **`local-ci --gh-action`** sets GitHub Action mode. It does not change which checkers are selected; a
  checker that should be inert in CI branches on `p.UserInput.GhActionMode` itself (for example,
  `comment_reply` skips in CI because the local run's stamp is what merge requires).

## How checkers get PR / GitHub data

Two channels:

- **Pre-fetched `ProjectInfo.PrData`** — populated by `githubDataPrepper` for every run. Read this
  for base/head branch, commits, and changed files.
- **Shelling out to `gh`** — for data the prepper does not carry (PR body, review threads). Guard
  with `exec.LookPath("gh")` and no-op gracefully if it is missing (mirroring `issuelinker`).
  Authentication is the ambient `gh` login. For anything with interpolated/untrusted values
  (paths, query bodies), use `cmdrunner.ExecuteCommandArgs` (argv passed verbatim, never
  shell-reparsed), not `ExecuteCommand`.

## Adding a checker — recipe

1. Add a type implementing the `Checker` interface. `issuelinker.go` is the cleanest template
   (full interface, `gh` shell-out, graceful `gh`-missing skip, instructive error). For a fetch
   that must always run, return `["**"]` from `GetFileFilter`.
2. Pick the execution mode: `Parallel` unless there is a reason otherwise; `Preflight` only for a
   cheap check worth aborting the whole run on.
3. Add the checker to the set your host passes to `runner.NewRunner` (for the shipped CLI, that is
   `defaultCheckers()` in `cmd/local-ci`). See [examples/custom-checker](examples/custom-checker/).
4. Write table-driven unit tests for the decision logic — keep the pure logic (given parsed data,
   what fails) in a function that does not touch `gh`, so it is testable without the network.

## The stamp

The stamper (`stamper/`) records results as a base64-encoded JSON `StampInfo` in a git note on the
head commit, pushed to `refs/notes/commits`. The note carries the commit SHA, the per-checker
pass/fail map, and the base branch. A merge gate reads the note and skips a check whose result the
stamp already records for the matching commit and base branch. The composite action in
`.github/actions/local-ci-checker/` is the reference reader.
