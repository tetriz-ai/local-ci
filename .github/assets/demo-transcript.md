# Offline checker demo

This is a replay of captured stdout from the real offline example. It calls a custom
`checker.Checker` on synthetic diffs, without GitHub access or stamp writes. The
example reports both outcomes and exits successfully after demonstrating them.

```sh
go run ./examples/custom-checker
```

```text
clean diff: PASS
diff with a _wip.go file: FAIL — remove work-in-progress files before merge: pkg/service/scratch_wip.go
```
