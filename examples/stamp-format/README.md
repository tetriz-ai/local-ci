This example shows the shape of the stamp local-ci writes on a commit and a merge gate reads back. local-ci
stores a `projectinfo.StampInfo` as base64-encoded JSON in a git note; the program builds one,
encodes it the same way, and decodes it again, so you can see exactly what a gate inspects to
decide whether a commit's checks already passed. No git or GitHub access is needed.

```sh
go run ./examples/stamp-format
```

```text
git note (base64): eyJjb21taXRTaGEiOiIxYTJiM2M0ZDVlNmY3YThiOWMwZDFlMmYzYTRiNWM2ZDdlOGY5YTBiIiwiY2hlY2tzU3RhdHVzIjp7ImdvdnVsbmNoZWNrIjp0cnVlLCJwcm90b19icmVha2luZyI6dHJ1ZX0sInZlcnNpb24iOiJ2MC4xLjAiLCJiYXNlQnJhbmNoIjoibWFpbiJ9
commit:      1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b
base branch: main
proto_breaking passed: true
govulncheck passed:    true
```
