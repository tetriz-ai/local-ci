This example shows how a project assembles the checker set it hands to `runner.NewRunner`. It
lists local-ci's built-in checkers alongside a custom one and prints each checker's name and the file
globs that make it eligible — the same `Checker` interface the engine reads. Use this to inspect,
add to, or trim the set before passing it to the runner. No GitHub access is needed.

```sh
go run ./examples/compose-checkers
```

```text
checker set (name — file globs):
  comment_reply      **
  issue_linker       **
  proto_breaking     **.proto
  buf_lint_comments  **.proto
  readme_checker     **/README.md, **/*.go, **/*.yml, **/*.yaml, **/*.json, **/*.proto
  govulncheck        **.go, **/go.mod, **/go.sum
  pnpm_audit         **/package.json, **/pnpm-lock.yaml
  lockfile_sync      **/package.json, **/pnpm-lock.yaml
  golangci_lint      **.go, **/.golangci.yml, **/.golangci.yaml, **/.golangci.toml, **/.golangci.json
  changelog_updated  **/*.md
```
