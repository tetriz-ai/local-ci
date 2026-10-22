# Examples

The examples are ordered from a custom check to checker composition and the stamp format.
Each is a complete Go program using the public packages and synthetic inputs. They need Go 1.25
or newer; after Go dependencies are downloaded, execution requires no GitHub account, credentials
or network calls. They call library code directly, rather than running `local-ci --pr` or writing notes.

Run every example from the repository root:

```sh
scripts/run-examples.sh
```

The runner vets the examples, checks this index against the folders, and compares each program's
stdout with the final `text` block in its README. Output is deterministic and needs no normalization.
The CI workflow invokes the same runner on pushes and pull requests.

| You want | Example |
|---|---|
| To write your own check (implement `checker.Checker`) | [custom-checker](custom-checker/) |
| To assemble and inspect a checker set | [compose-checkers](compose-checkers/) |
| To read the stamp a merge gate inspects | [stamp-format](stamp-format/) |
