# Contributing

This guide covers local checks and the conventions a pull request is expected to follow.
The CI workflow runs these checks on pull requests and pushes to main. Run them locally before
requesting review.

## Build, test, and lint

local-ci builds with Go 1.25 or newer (the floor is set in `go.mod`). From a fresh clone:

```sh
go build ./...
go vet ./...
go test -race -count=1 ./...
```

Lint with golangci-lint v2.11.4 — the version pinned in the retained workflow — using the checked-in `.golangci.yml`.
Install that exact version with `go install`. It lands in `$(go env GOPATH)/bin`, which must be on
your PATH:

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4
golangci-lint run ./...
```

Run all three offline examples and compare their output with their READMEs:

```sh
scripts/run-examples.sh
```

## Conventions

- **Test behavior changes.** A behaviour change needs a test that fails before the
  change and passes after it.
- **Tests are table-driven**, matching the existing suite.
- **Do not silence the linter inline.** A rule that must be off is off in `.golangci.yml` with a
  comment explaining why.
- **Keep commits focused.** One logical change per commit, with a short imperative subject line.

## Licensing of contributions

No sign-off line and no separately signed agreement are needed. By opening a pull request you
agree that your contribution is released under the project's [LICENSE](LICENSE).

## Documentation and examples

Keep the Quick start command and expected output aligned with `examples/custom-checker/README.md`.
`scripts/run-examples.sh` vets the examples, checks that the index matches the folders, runs each
example and compares stdout with its README. It also compares the root Quick start's fixed
command and output with the custom-checker example. Root Markdown is never evaluated.
`--docs-only` checks the fixed README commands and output without running Go.

Binary download and source installation snippets are excluded from execution because they need
published network artifacts. Live PR snippets are excluded because they need authenticated GitHub
access and mutate the repository or GitHub. The `deps` usage snippet is excluded because it installs
a tool and its result depends on the checkout. Their exact commands are checked for drift; these
exclusions do not claim installation or live-workflow validation.

Examples must use synthetic inputs and run without
GitHub access; fetching Go dependencies during setup may require network access.

Check relative Markdown links and preview the README at desktop and mobile widths after an
asset or layout change. Regenerate and inspect the demo using [.github/assets/README.md](.github/assets/README.md)
when its example changes. Do not use real credentials, repository data or personal terminal state
in screenshots or captures.

## Releases

Mohit Patni maintains releases. The project follows Semantic Versioning; the public API and CLI
may change in minor releases before 1.0. Release artifacts and their SHA256 checksums are created
by the tag-triggered release workflow using the same builder as a manual release. Each archive
includes `SOURCE.json`, tying the artifacts to the exact commit and Go version. To build
without publishing, from a clean checkout of the exact commit to release, run:

```sh
GOTOOLCHAIN=go1.27.3 python3 scripts/build-release.py --version v0.1.0
```

The standard-library Python builder writes all six platform archives and checksums under
`dist/v0.1.0`. Each archive contains both CLIs and the legal and provenance files. Inspect the
archive contents, run the binaries for your native platform and verify checksums before uploading.
`local-ci --version` reports `0.1.0`; the tag and `SOURCE.json` use `v0.1.0`. Supported platforms and
download instructions are in [README.md](README.md#install-the-command-line-tools).
