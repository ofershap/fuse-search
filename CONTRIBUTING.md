# Contributing

Issues and pull requests are welcome.

## Setup

Go 1.26 or newer. Standard library only; no other dependencies for the
library or tests. Regenerating the golden corpus needs Node (pinned
fuse.js 7.5.0 in testdata/gen/, committed lockfile) and is not part of the
normal test run.

## Checks

Run everything CI runs before opening a PR:

```bash
go vet ./...
gofmt -l .
go test -race ./...
```

## Rules

- Behaviour must match fuse.js 7.5.0 exactly: scores, match ranges and
  ordering compare with exact float64 equality on the differential corpus.
  If upstream changes, extend the corpus and regenerate goldens in the same
  change; intentional divergences get a pinned regression test.
- Keep the dependency tree empty.
- Claims in the README must match what the tests demonstrate.
