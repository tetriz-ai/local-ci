# checker

Defines the `Checker` interface and the `Eligible` filter that selects, for a pull request, the
checks whose file globs match the diff and whose previous stamp is stale (serial, parallel, and
preflight modes).

The nine built-in checkers: `issue_linker`, `comment_reply`, `proto_breaking`, `buf_lint_comments`,
`readme_checker`, `govulncheck`, `pnpm_audit`, `lockfile_sync`, and `golangci_lint`. A host supplies
its own set to `runner.NewRunner`; the engine builds no fixed roster of its own.
