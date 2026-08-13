---
tags: [goals, module, tui, sqlite, tdd, currency]
---

# Goals module

 This module will be responsible for managing user's goals. It will have commands for create, list, details, edit, delete. All command will have shortcuts.

## Commands

list: "kakei goals" or "kakei g"
create: "kakei goals new" or "kakei g n"
edit: "kakei goals edit {ID?}" or "kakei g e {ID?}"
delete: "kakei goals delete {ID?}" or "kakei g d {ID?}"
details: "kakei goals {ID?}" or "kakei g {ID?}"

## Suggested structure

title: string
description?: string
target: int
currentValue: int
currencyType: string
type: string

also basic columns

## Rules

- all commands that uses CODE|ID will shows an account select if no code is provided
- it must use the bubbles package to create a great UX
- there will be 4 options as currencyType: Dollar, Euro, Brazilian Real, Bitcoin
- if the commands is used if the flag "-h" or "--help" it will show a small documentation of that command only
- the type is like: saving or paying bills.
- a transaction can be connected to one goal. Like paying a bill or saving for something new. But it is not required.

## Status: Implemented

Built and verified in the `goals-module` session. Six design questions went to
the user before any code; the answers and what they cost are in
[[decisions/0010-goal-progress-summed-from-the-ledger]].

- **`currentValue` is not a column.** A goal's progress is the sum of the
  transactions carrying its `goal_id`, worked out on every read, so a goal can
  never disagree with the ledger. Verified live: editing a linked transaction
  moved the goal by exactly that amount with no write to the goals table.
- **The kind flips the sign.** A saving goal counts income minus outcome, a
  paying one counts outcome minus income, so both climb toward a positive
  target. The flip lives in Go, beside the wording it drives; the SQL is the
  same for both.
- **A goal counts one currency.** Only transactions in it may be linked — the
  form offers no others, the store fills both currencies from the database
  before validating, and `Validate` refuses a mismatch. The currency freezes
  once anything is linked; the kind never does.
- **Goals are referenced by id**, like transactions and unlike every other
  module — the spec's own `{ID?}`. No code and no colour, so the details card
  is bordered in the goal's state instead: green once reached, red while it is
  going backwards.
- **Deleting a goal is never refused.** The transactions that fed it keep their
  money and lose the link, and the confirmation counts them first.
- The spec's `type` is stored as `kind`, for the reason
  [[decisions/0008-transaction-double-entry-tags-and-filters]] already settled;
  `title` is `name`, so `core.ValidateName` is reused rather than copied.
- Test suite per [[rules/tdd]]: schema, model, store, UI, command and seed
  layers each red-then-green, own SQLite file per subtest, substring assertions
  for anything lipgloss renders. `internal/goals` cannot import
  `internal/transactions` — that package imports this one — so its tests write
  linked transactions with raw SQL, the same way `internal/bills` does.

Files: `internal/db/migrations/007_goals.sql`, `internal/goals/{goal,store,ui}.go`,
`internal/transactions/{transaction,store,ui}.go`, `cmd/goals.go`,
`cmd/transactions.go`, `cmd/main.go`, `scripts/seed/main.go`.

Commits: `feat(goals): add the goals cli with progress read from the ledger` →
`feat(transactions): let a transaction name the goal it feeds` →
`feat(seed): add goal fixtures to the dev database`.

Links: [[decisions/0010-goal-progress-summed-from-the-ledger]] · [[decisions/0008-transaction-double-entry-tags-and-filters]] · [[decisions/0001-balance-as-int64-minor-units]] · [[rules/tdd]]
