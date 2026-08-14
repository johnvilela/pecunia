---
tags: [goals, module, tui, sqlite, tdd]
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

## Update (continues session 4b4dcd74-5218-4aa9-b73f-c1f8ae4e3279): reached mark and card view

Full session: [[sessions/4b4dcd74-5218-4aa9-b73f-c1f8ae4e3279]].

Two follow-up requests after the module first landed, both driven by the user looking at real output and both TDD'd like the rest:

- **A green `✓` after the name once a goal is reached.** Rendered through a `name(g)` helper rather than inlined in `Table`, so the picker and the transaction form's goal select carry it too. A paying goal that reaches zero gets the same mark, since `Reached()` covers both kinds.
- **`kakei g` now lists one card per goal, with its bar — the compact table moved behind `kakei g --resume`.** The bar is the point and does not fit a table row. Handled with a plain string comparison in `runGoals`, not a `flag.FlagSet`: one boolean does not need the ten-flag machinery `kakei t` has. The `✓` mark was extended to the detail card at the same time, since the card is now the default view.

`go build`/`go test ./...`/`gofmt`/`go vet` clean throughout. Commits: `feat(goals): mark a reached goal in the list` → `feat(goals): list goals as cards, table behind --resume` → `docs(wiki): record the goals module session` (the last one updated the session's own wiki page, which had stopped documenting itself four commits early).

Links: [[decisions/0010-goal-progress-summed-from-the-ledger]] · [[decisions/0008-transaction-double-entry-tags-and-filters]] · [[decisions/0001-balance-as-int64-minor-units]] · [[rules/tdd]] · [[sessions/4b4dcd74-5218-4aa9-b73f-c1f8ae4e3279]]

## Update: the target log

A target is a promise about the future, and the future moves — the case that
drove this: a R$5000.00 credit-card bill settles for R$3500.00 on an offer, and
the goal has to follow without losing the fact that it ever said R$5000.00.

`internal/db/migrations/008_goal_target_log.sql` adds `goal_target_log`
(`goal_id` CASCADE, `previous`, `target`, `note`, `created_at`). Four calls, all
the user's:

- **Target changes only.** Not freeform notes, not an audit trail of every edit.
- **Entered through `kakei g e`.** The form asks "Why?" on any edit; the store
  keeps the note only when the target actually moved, so there is no conditional
  field for huh to redraw underfoot.
- **`goals.target` stays the live value**, with the log beside it rather than
  derived from it. `Store.Update(g, note)` writes the row and its log entry in
  one SQL transaction, so a goal can never sit at a target its history does not
  account for.
- **Shown on `kakei g ID` only.** `Details(g, log)` takes the entries; the
  `kakei g` list passes nil, because a screen of goals each dragging its own log
  behind it is not a list.

Each entry stores `previous` as well as `target`, so a line explains itself
without walking back through the ones before it — and so the original target is
not lost despite creation writing no entry. Verified with the dev-seed fixture
(the "Quitar o Itaú" goal, cut from R$4120.00 to R$3500.00 with the note
"consegui um desconto à vista"), idempotent on reseed and a no-op on a database
missing the fixture.

Commit: `feat(goals): log every target change with its date and reason` (`806ea43`).

## Update: card layout — dates above the divider, reason on its own line, header renamed

Two follow-up requests from screenshots, right after the target log landed, both TDD'd.

- **The goal's own `created`/`updated` line moved above the target history**, with a divider between them shown only when a log exists (`rule()`, width computed from the widest line on the card once every line exists, floor `cardWidth - 4`). A goal whose target never moved renders exactly as before — no divider, no history block. Each entry's reason moved to its own line under its date-and-amounts line, so a long reason no longer stretches the card sideways. User's own framing: "The createdAt and updatedAt related to the goal must be above the logs... a line dividing and below it the logs if exists (the divider should only be shown if a log exists)." Commit: `feat(goals): put the goal's dates above a divided target history` (`a938775`).
- **The history header changed from `"target"` to `"updates"`** — plural, since it labels a list of entries. Commit: `feat(goals): head the target history with updates` (`dace007`).

A background subagent was also sent to update the wiki for these card changes; its outcome is not confirmed by the transcript. A follow-up `/git-commit` found nothing left to commit — everything from this stretch had already landed across `806ea43`, `b924855`, `a938775`, `dace007`.

Links: [[sessions/4b4dcd74-5218-4aa9-b73f-c1f8ae4e3279]] · [[decisions/0010-goal-progress-summed-from-the-ledger]] · [[rules/tdd]]