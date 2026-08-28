---
tags: [testing, go, conventions, tdd]
---

**ALWAYS** use TDD for a new feature or module in this repo. No exceptions —
this is not "when convenient".

## The cycle

1. **Red** — write the failing test first. Run it. Watch it fail for the reason
   you expect. A test that passes before the code exists is testing nothing.
2. **Green** — write the least code that makes it pass.
3. **Refactor** — clean up with the test still green.

Never write implementation code first and backfill tests. If a test was written
after the code, it only proves the code does what it does.

## Rules for the tests themselves

- **Real SQLite file, never `:memory:`.** The schema, the `UNIQUE` index and the
  `CHECK` constraints are half of what is worth testing, and only the real
  migration path builds them.
- **Every case gets its own database.** The helper pattern is
  `t.Setenv("PECUNIA_DB", filepath.Join(t.TempDir(), "pecunia.db"))` then
  `db.Open()`, called **inside** the subtest — not in the parent, or the cases
  go back to sharing state.
- **No case depends on another.** Order-independent, individually runnable with
  `go test -run 'TestX/case'`. One failure must not hide the rest.
- **Sequential.** `t.Setenv` forbids `t.Parallel` in the same test. Isolation
  comes from a separate file per case, not from concurrency. Only add
  `db.OpenAt(path)` and go parallel if the suite gets slow enough to notice.
- **Subtests, not one long scenario.** `t.Run` per behaviour, table-driven where
  the cases are the same shape.
- Assert on **substrings** for anything lipgloss renders — the ANSI escapes
  around the text change with the terminal profile, the text does not.
- TTY-bound code (`huh` forms, `bubbletea` pickers: `accounts.Form`, `Confirm`,
  `Pick`) cannot run in tests. Test what it calls into instead, and drive the
  command paths that never open a form.

## Layout

Tests live beside the code, in the same package: `internal/accounts/store.go` →
`internal/accounts/store_test.go`. Same package, so unexported helpers are
testable without an export-for-testing dance.

Stdlib `testing` only. No testify, no mocks, no fixtures framework.

Links: [[rules/git-hooks]] · [[rules/commit-messages]]
