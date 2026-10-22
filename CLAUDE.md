# CLAUDE.md

## What this is

local-ci runs a project's CI checks locally against a pull request and stamps the head commit with
what passed, so a GitHub merge gate can skip the work it already covers. The engine holds no
fixed check list: a host program implements `checker.Checker` and passes its set to
`runner.NewRunner`.

## Layout

- `cmd/local-ci/` — the `local-ci` CLI (cobra); `main.go` builds the default checker set in `defaultCheckers()`.
- `cmd/deps/` — the `deps` tool (lists packages/dirs a diff impacts).
- `checker/` — the `Checker` interface, the `Eligible` filter, and the nine built-in checkers.
- `runner/`, `prepper/`, `stamper/`, `postprocessor/`, `projectinfo/`, `workflow/` — the run engine.
- `internal/` — vendored helpers (cmdrunner, logger, projectroot, …), not part of the public API.
- `examples/` — runnable Go programs; `scripts/run-examples.sh` runs and checks them.
- Tests live beside the code as `*_test.go`.

## Commands

Build, vet, and test (Go 1.25 floor, set in `go.mod`):

```sh
go build ./...
go vet ./...
go test -race -count=1 ./...
```

Lint with golangci-lint v2.11.4, the version pinned in the retained workflow, using `.golangci.yml`. Install that exact
version with `go install`. It lands in `$(go env GOPATH)/bin`, which must be on your PATH.

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4
golangci-lint run ./...
```

Run the examples and compare their documented outputs:

```sh
scripts/run-examples.sh
```

## Contract to keep

- The plugin seam is the public API: `checker.Checker`, `runner.NewRunner(userInput, version, checkers)`,
  and `checker.Eligible`. A host supplies the checker set; the engine builds none of its own.
- A checker reports pass or fail by returning a `*checker.Report`; a `Failure` carries the error
  shown to the developer. Pinned by the `checker` package tests.
- The stamp accepts a stamped commit and reports an unstamped one as absent, never as success.
  Pinned by `stamper.TestStampAcceptsStampedRejectsUnstamped`.
- Each checker's `Cleanup` runs exactly once per run. Pinned by `runner.TestCleanupEnqueuedOncePerChecker`.
- Changed-file paths are validated shell-safe before reaching a command. Pinned by
  `prepper.TestValidateAffectedFilesShellSafe`.
- Tests never reach a real credential store: `TestMain` in checker/postprocessor/prepper/stamper
  points HOME at a temp dir.

## Making a change

- A behaviour change needs a test that fails before it and passes after. New cases go in the
  package's `*_test.go`, table-driven.
- Do not silence the linter inline; a rule that must be off is off in `.golangci.yml` with a comment.
- CI runs the checks below; both automated and manual releases use scripts/build-release.py.
- Examples are checked by `scripts/run-examples.sh`. A new example is a folder under
  `examples/` (copy `examples/custom-checker/` as the template), linked from `examples/README.md`; the runner runs its README's `sh` block and
  compares stdout to the final `text` block, so keep example output stable (no timestamps or ids).
- A behaviour change that adds a capability gets a `CHANGELOG.md` line under `[Unreleased]`. A
  docs-only, test-only, or new-example change adds no engine capability, so it needs none.

## Do not

- Do not reach for a real GitHub token in a test; the built-in checkers shell out to `gh` and
  the `TestMain` guard blocks a real store.
- Do not move the Go floor below 1.25; `golang.org/x/tools` requires it.
- Do not edit files under `third_party/licenses/`; they are regenerated from the dependency set.
