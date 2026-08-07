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

See also: the pre-commit hook at `.githooks/pre-commit` (gofmt + go vet) that
gates every commit.
