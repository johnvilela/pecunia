---
tags: [git, commits, conventions]
---

Every commit in this repo is a **single-line** Conventional Commits / commitlint
subject. No body, no description, no trailers.

## Format

```
<type>(<scope>): <subject>
```

Scope optional — one lowercase noun (`cmd`, `db`, `hooks`, package name). Omit
when the change spans many areas.

Types: `feat`, `fix`, `refactor`, `perf`, `test`, `docs`, `style`, `build`,
`ci`, `chore` (chore is last resort — prefer a real type).

Subject: imperative mood, lowercase, ≤72 chars, no trailing period. Says
what/why, not the file list.

## Hard rules

- One line only. Never a body or multi-paragraph message.
- Never `Co-Authored-By` or any other trailer.
- Never `--no-verify`, `--amend`, `--no-gpg-sign`, `-i`.
- Stage explicit paths (`git add <file> ...`) — never `git add -A` / `git add .`.
- Pre-commit hook fails → fix the root cause, re-stage, new commit. No bypass.

## Examples

- `feat: initialize kakei go cli with gofmt/vet pre-commit hook` (root commit `159372d`)
- `fix(cmd): preserve exit code on unknown command`
- `refactor(db): replace switch dispatch with command registry`

## Splitting commits that share a file

Two modules built back to back in the same uncommitted working tree ([[tasks/07-recurring-bills-module]] and [[tasks/08-summary-module]]) had both edited `cmd/main.go`'s dispatch switch. User request: "/git-commit separate the bill form the summary". Handled by editing `cmd/main.go` down to just the first module's `case`, staging and committing that module's code, then its own `docs(wiki)` commit, then restoring the second module's `case` and repeating for it. A helper one module needs that the other introduced (`core.MoneyLine`, added while fixing the recurring-bills board but required by `internal/recurring/ui.go` to compile) rides in with whichever module can't build without it, not with the module that happened to add it.

## Splitting docs commits by prior ownership

A later `/git-commit` request — "/git-commit but divide different features into their own commit" — found three different things staged at once: six `wiki/` files that had been modified by an earlier session (retagging, cross-links, "Update: committed" notes) and this session's own two new task docs (budgets, transfers). Handled as three commits: the six pre-existing files first (not this session's work, so not folded into either feature), then the budgets doc, then the transfers doc — each `docs(wiki)`. Same rule as the section above, applied to documentation instead of code: a commit's contents are one concern, and who touched the file and why is what decides the boundary, not which command happened to stage it.

A related instance from the same session ([[sessions/c2b6cbbe-5735-4790-abac-4c4b5a60aca7]]): four data-integrity fixes ([[decisions/0013-data-integrity-fixes-and-known-gaps]]) landed as four separate commits by request ("one commit per fix") even though they were built in the same sitting and touch overlapping files (`internal/db/db.go` twice, `internal/accounts` and `internal/cards` together) — the split follows the fix, not the file.

See also: the pre-commit hook at `.githooks/pre-commit` (gofmt + go vet) that
gates every commit.