---
tags: [go, cli, project-structure]
---

## Decision

All CLI commands live directly in `cmd/` (package `main`) — never in a `cmd/kakei/` subdirectory. The binary is named `kakei` by the build invocation, not by directory layout.

## Why

User, when starting the real CLI: "there should not be a @cmd/kakei/ file inside it because the bin will be called kakei. The commands should be on the root of cmd." The project's initial hello-world bootstrap had used `cmd/kakei/main.go`; it was deleted and replaced with a flat `cmd/` package once the actual CLI structure was built.

## Consequence

- Build with `go build -o kakei ./cmd` — the `-o` flag is required because the directory is named `cmd`, not `kakei`; a plain `go install` would name the binary `cmd`.
- `cmd/main.go` holds the ASCII banner, help text, and a switch-based dispatch on `os.Args[1]` — stdlib `flag`, no cobra/urfave, kept dependency-free (matches the user's earlier "as little dep as possible" preference from the git-hooks discussion, [[rules/git-hooks]]).
- Each subcommand is its own file at the root of `cmd/` (`setup.go`, `accounts.go`).

Links: [[tasks/01-accounts-module]]