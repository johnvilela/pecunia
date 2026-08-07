---
tags: [git, hooks, go, conventions]
---

Every commit in this repo passes through `.githooks/pre-commit` first.

## Setup (per clone)

```
chmod +x .githooks/pre-commit
git config core.hooksPath .githooks
```

`core.hooksPath` is per-clone, not inherited from the repo — anyone who clones has to run the `git config` line once themselves.

## What it checks

```sh
gofmt -l .      # fail if any file is unformatted
go vet ./...
```

Both ship with the Go toolchain — zero extra dependencies. Confirmed deliberately: user asked "gofmt is native? Because i want as little dep as possible" before this was accepted; verified with `command -v gofmt` pointing into the Go toolchain's own install path.

## Deferred / rejected

- golangci-lint — not installed, `go vet` covers the basics for now; add when vet stops catching enough.
- `go test ./...` in the hook — deferred until there were tests to run.
- staged-only checking — hook checks the whole repo; fine while the repo is small.

Links: [[rules/commit-messages]]