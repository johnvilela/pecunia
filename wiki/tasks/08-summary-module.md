---
tags: [summary, cli, reporting, tdd, git]
---

# Summary module

One screen that answers "where do I stand": what moved, what needs paying, what
the accounts and cards hold, and where the goals are. It owns no table and
writes no SQL of its own — every figure comes from a store that already exists,
which is why a summary can never disagree with the command it summarises.

User: "Now lets create the command to show the resume of the current day. Ask me
anything to refine this." Seven questions went back before any code; the answers
are the decisions below.

## Commands

```
kakei summary                          today
kakei s                                the alias
kakei summary --month                  this month
kakei summary --date 2026-08-10        that day
kakei summary --month --date 2026-07-04  that day's month
```

## Decisions

- **`summary`, not `resume`.** The user asked for "the resume of the current
  day", but `--resume` already means the compact goals table
  ([[tasks/06-goals-module]]), and reusing the word for a whole command would
  have made it two different things one letter apart.
- **Two orthogonal flags, one date format.** `--date` picks the day, `--month`
  widens whatever day was picked to its month. Arbitrary months fall out for
  free, and `--date` never has to accept a second, shorter spelling that would
  read as a day to anyone typing it.
- **`--month` re-scopes the same sections**, rather than switching to a
  spend-by-category breakdown. Asked directly; the answer was the same screen
  over a wider window. Nothing is capped — a month with 200 rows prints 200.
- **The full dashboard, not a snapshot.** Every section was asked about
  individually and every one was wanted: the window's transactions, its totals,
  balances, what is due, month-to-date, goals, and the week ahead.
- **Amounts are never summed across currencies.** One figure per currency,
  sorted, `·`-joined — the rule
  [[decisions/0010-goal-progress-summed-from-the-ledger]] set and the recurring
  board already followed. Wanting it a second time is what lifted it into
  `core.MoneyLine`; there is no rate anywhere in kakei to make one currency into
  another.
- **Static print, no TUI.** `kakei summary | head` works like every other list.
  A scrollable dashboard was offered and turned down.
- **What is due comes first**, above the transactions: it is the only section on
  the screen that asks for an action today.

## Status: Implemented

Full session narrative: [[sessions/b4318b40-e452-4a98-bf96-2a937fcccfdc]].

Built TDD per [[rules/tdd]] — renderer, collector and command each red-then-green,
own SQLite file per subtest, substring assertions for anything lipgloss renders.
`gofmt` / `go vet` / `go test ./...` clean. The design is written up in
[[decisions/0012-summary-composes-existing-stores]].

- **`Render` was written before `Collect`.** It pins the `Summary` struct to
  exactly what gets printed, which is the cheapest guard against a collector
  that gathers things nobody shows — and it tests from struct literals, so the
  multi-currency and boundary cases cost no database at all.
- **`bills.NewStoreAt` was added** (`internal/bills/store.go`, three lines). The
  statement store's clock was a field only its own package could reach, so a
  summary judging bills against a given day would have had its card statements
  invented against the wall clock — the one answer on the screen that disagreed
  with the rest. Existing tests stayed green untouched.
- **The week ahead cannot be read off `Bill.Current`.** It stands at the oldest
  cycle still unpaid, so rent settled for August says nothing about the cycle
  opening in five days — and rent is exactly the bill nobody wants surprised by.
  The window scans this month and next; seven days never span more than one
  boundary. A bill already in the due section is never repeated there.
- **A window that is over says nothing about what is due**, rather than "nothing
  due". The dev database caught this: `kakei summary --date 2026-08-11` claimed
  a clear board for a day whose bills were never read. Nothing is read for a
  past window, so nothing is claimed — which also skips the expensive per-card
  statement walk.
- **Month-to-date costs no extra query.** A day summary widens its one ledger
  read to the 1st and splits the rows in Go; a month summary already is the
  month, so the line is left out entirely rather than comparing a partial month
  against a full one.

Files: `internal/summary/{summary,ui}.go`, `internal/bills/store.go`,
`cmd/summary.go`, `cmd/main.go`.

## Update: the floating owed-total fix

A follow-up in the same session — user, from a screenshot of `kakei bill`: "The
still owed text is feeling strange there, like floating, this also happens on
the bills command." The fix moved the owed total into `recurring.Board`'s table
itself as its last row, and lifted the money-formatting `owedLine` used
(one figure per currency, sorted, sign in front of the symbol) into
`core.MoneyLine`. `internal/summary/ui.go` now formats its own totals through
`core.MoneyLine` too, in place of a private duplicate. Full write-up, since the
change originated on the recurring-bills side: [[tasks/07-recurring-bills-module]]
(`## Update: the owed total closes the table instead of hanging under it`).

Links: [[decisions/0012-summary-composes-existing-stores]] ·
[[decisions/0011-recurring-bills-derived-from-payments]] ·
[[decisions/0010-goal-progress-summed-from-the-ledger]] ·
[[decisions/0002-flat-cmd-package-layout]] · [[rules/tdd]] ·
[[sessions/b4318b40-e452-4a98-bf96-2a937fcccfdc]]

## Update: committed

Landed as `3937815` — `feat(summary): show where you stand for a day or a month`, wiki as `87f87e6` — `docs(wiki): record the summary module`. Committed on a resumed session, after and separately from [[tasks/07-recurring-bills-module]], on the user's request to split the two ("/git-commit separate the bill form the summary") since both had been built in the same uncommitted working tree and both touched `cmd/main.go`'s dispatch switch — the `summary` case was removed for the bill commit and restored for this one.

Links: [[tasks/07-recurring-bills-module]] · [[sessions/b4318b40-e452-4a98-bf96-2a937fcccfdc]]