This example shows the local-ci plugin seam: a project defines its own check by implementing the
`checker.Checker` interface. The check here fails a pull request that adds a work-in-progress Go
file (one whose name ends in `_wip.go`). To stay offline, the program calls the checker's `Execute`
directly against a clean diff and a diff that adds such a file, so both the pass and fail paths are
visible with no GitHub access — it does not run the full engine or write a stamp. See
[main.go](main.go) for how to hand the checker to `runner.NewRunner` in your own build of local-ci, which
is what runs it for real.

```sh
go run ./examples/custom-checker
```

```text
clean diff: PASS
diff with a _wip.go file: FAIL — remove work-in-progress files before merge: pkg/service/scratch_wip.go
```
