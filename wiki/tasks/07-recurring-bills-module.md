---
tags: [recurring, bills, sqlite, tdd, git]
---

# Recurring bills module

The bills that come round every month: energy, Netflix, rent. A bill is a
*template* — what it costs, where it is paid from, what it is filed under, and
the two days that decide when it can be paid and when it is late. Paying one
records an ordinary transaction; the bill itself never holds money.

Spec came from the user by phone, refined over two rounds of questions before
any code.

## Commands

```
pecunia bill                board: this cycle for every active bill, urgent first
pecunia bill --all          archived bills too
pecunia bill new | n        create a bill
pecunia bill CODE           details: status, dates, last 12 payments, averages
pecunia bill CODE pay       record this cycle's payment
pecunia bill CODE edit | e
pecunia bill CODE delete | d
pecunia bill CODE archive   stop it counting as due; unarchive brings it back
```

Verb-first (`pecunia bill pay ENERG`) works too — the code may come on either
side, since `pecunia bill ENERG pay` is how the user says it out loud.

## Decisions

- **Package is `internal/recurring`, table `recurring_bills`.** `internal/bills`
  is already the credit-card statement — see
  [[decisions/0009-bills-as-rows-and-installments-as-transactions]]. The CLI verb
  `pecunia bill` was free (`pecunia cc bill` is the card one) and is what the user
  types, so only the package had to move out of the way.
- **Two day-of-month integers, monthly only.** `open_day` is when the bill can
  be paid, `due_day` when it is late. `cards.NextDate` clamps a day the month is
  too short for and rolls the due date into the next month when `due_day` is the
  smaller of the two — an energy bill opening on the 28th and due on the 5th is
  the normal shape, not a special case. Yearly and weekly bills are not covered.
- **No occurrence rows.** A cycle's status is worked out from the payments
  filed against the bill, the same call goals made in
  [[decisions/0010-goal-progress-summed-from-the-ledger]]: nothing to generate,
  nothing to backfill, and a status that can never drift from the ledger.
- **The cycle is stored on the payment**, as `YYYY-MM`. February's bill paid on
  3 March clears February and leaves March open. Without it a late payment
  leaves the month it was for overdue forever, which is exactly the month a
  bill module exists to catch. The pay form pre-fills the oldest unpaid cycle
  and lets it be overridden.
- **Paying opens the full transaction form, pre-filled** from the bill: title,
  expected amount, date, source, category, tags. Energy varies month to month,
  so the amount is a starting point rather than a fact — and anything else can
  still be corrected before it is written.
- **Archive, not just delete.** A cancelled Netflix stops counting as due and
  keeps its history readable. Delete stays, and unlinks its transactions
  rather than taking their money with it.
- **The expected amount is optional.** Rent is exact, energy is a guess, and a
  bill whose amount is unknown should not have to invent one.

## Status: Implemented

Built TDD per [[rules/tdd]] — schema, model, store, UI, command and seed layers
each red-then-green, own SQLite file per subtest, substring assertions for
anything lipgloss renders. `go build` / `go test ./...` / `gofmt` / `go vet`
clean. The design is written up in
[[decisions/0011-recurring-bills-derived-from-payments]].

Two things the live dev database caught that the tests had not:

- **An archived bill still read "overdue" on its own card.** It was off the
  board but not out of play. `StatusArchived` now short-circuits `Current`, so
  an archived bill owes nothing anywhere it is rendered.
- **A bill's own tag was dropped the first time it was paid.** The transaction
  form only pre-selects tags it has an option for, and its options are the tags
  already on some transaction. `payRecurring` puts the bill's tags on offer
  before opening the form.

Files: `internal/db/migrations/009_recurring_bills.sql`,
`internal/recurring/{bill,store,ui}.go`,
`internal/transactions/{transaction,store,ui}.go`, `cmd/recurring.go`,
`cmd/main.go`, `scripts/seed/main.go`.

## Update: board rendered as a lipgloss table, and a paid-this-month seed fixture

Full session: [[sessions/5321cd80-4dd0-4dea-85c3-391b008334d2]].

User, from a screenshot: "i think we should improve this using the bubbles package and the lipgloss. Maybe using a table." `Board` was rewired onto `lipgloss/table`, matching the border and header style `pecunia t` and `pecunia g --resume` already use:

- Four columns — **BILL** (code in its color, then name), **AMOUNT** (right-aligned so the figures line up), **STATUS** (mark + word, colored by state), **WHEN** (just the date half — `when()` was split out of `state()` so the status word isn't repeated in the column; `state()` itself, `status — when`, stayed for the details card and the picker).
- Sort order (most urgent first, tie-broken by due date then code) and the per-currency owed footer are unchanged; the footer is now indented to sit under the table's own cells instead of under the border.
- `bubbles` was deliberately not brought in for the board itself — it is static output and `pecunia b | grep` has to keep working, the same reason `pecunia t` and `pecunia g --resume` already render through `lipgloss/table`. `bubbles/list` is untouched and still drives the picker whenever the code is left off (`pecunia bill pay`). An interactive board (arrow to a bill, enter to pay) was named as a possible follow-up, not built.

User: "on the seed. Add a scenario where the bill is paid too." A sixth fixture, `INTNT` (Internet, Vivo Fibra, R$129.90, opens 1st, due 10th), was added along with a new `PaidNow` fixture flag that settles the current cycle in addition to past ones — the seeded board now shows all five states (`upcoming`, `open`, `overdue`, `paid`, `archived`) at once. Payment dates clamp to today when open-day+2 hasn't happened yet, so nothing seeded is ever dated into the future. The fixture-coverage test was tightened from `len(states) >= 3` (which is how a missing `paid` state had slipped through undetected) to demanding every one of the five states by name, and a new case asserts no seeded payment is future-dated — both written red-first per [[rules/tdd]].

All 11 packages green, `gofmt`/`vet` clean after each change.

Links: [[decisions/0011-recurring-bills-derived-from-payments]] · [[decisions/0006-credit-card-money-schedule-and-over-limit-model]] · [[decisions/0008-transaction-double-entry-tags-and-filters]] · [[tasks/04-transactions-module]] · [[rules/tdd]] · [[sessions/5321cd80-4dd0-4dea-85c3-391b008334d2]]

## Update: the owed total closes the table instead of hanging under it

User, from a screenshot of `pecunia bill`: "The still owned text is feeling
strange there, like floating, this also happens on the bills command."

It was a line printed after `t.Render()`, indented two spaces — attached to
nothing, and at a left margin no column shared. It is now the table's last row:
the figure lands in the AMOUNT column it is a total of, and the label is dim and
right-aligned so it leans against that figure rather than starting where a
bill's name starts. `StatusOpen`/`StatusOverdue` are still the only states
counted, so an upcoming or archived bill is no more owed than it was.

`owedLine` is gone. What it did minus its hardcoded label is now
`core.MoneyLine` ([[decisions/0012-summary-composes-existing-stores]]) — one
figure per currency, sorted, `·`-joined, sign in front of the symbol — because
[[tasks/08-summary-module]] needed the same rule four times over and two copies
of "never add centavos to satoshis" is one too many. `Board` is what
`pecunia summary` renders its DUE and NEXT 7 DAYS sections with, so both screens
got the fix from one change.

Written red-first per [[rules/tdd]]: the case that pinned it asserts the total
sits on a line that starts with the table's own border, which is what "floating"
meant in a form a test can check.

Links: [[decisions/0012-summary-composes-existing-stores]] · [[tasks/08-summary-module]] · [[rules/tdd]]

## Update: committed, split from the summary work

Full session (resumed): [[sessions/b4318b40-e452-4a98-bf96-2a937fcccfdc]].

Resolves the open question this page and [[sessions/5321cd80-4dd0-4dea-85c3-391b008334d2]] both left hanging — three separate "commit this" requests whose outcome went unconfirmed. On a later, resumed session the user asked to "/git-commit separate the bill form the summary", since the recurring-bills module and the summary module ([[tasks/08-summary-module]]) had been built back to back in the same uncommitted working tree. Landed as `46a7efa` — `feat(bill): add recurring monthly bills tracked from their payments`, wiki as `d4b4735` — `docs(wiki): record the recurring bills module`. Because `cmd/main.go` carried both modules' dispatch cases, the summary case was removed, the bill commit made, then the case restored before the summary work was committed separately. `core.MoneyLine` and the owed-total fix ([[decisions/0012-summary-composes-existing-stores]]) rode in this commit rather than the summary one, since `internal/recurring/ui.go` calls `core.MoneyLine` and `internal/recurring/` had no prior commit to diff against.

Links: [[sessions/b4318b40-e452-4a98-bf96-2a937fcccfdc]] · [[sessions/5321cd80-4dd0-4dea-85c3-391b008334d2]] · [[decisions/0012-summary-composes-existing-stores]] · [[tasks/08-summary-module]]

## Update: fixture windows retiled to cover every board state on any day

Session: [[sessions/85b098e4-8278-4b05-8279-fbda23de2fcd]].

The dev-seed test `TestSeedRecurring` (the fixture-coverage case) failed depending on the day of the month: on days 29-30 of a 31-day month no fixture's window produced an `open` bill (the 22-28 window had gone overdue, the day-31 fixture hadn't opened yet), and on days 1-10 no fixture was `overdue` (seeded bills only owe for the month they are created in — past cycles don't exist for them). The test had only carved out an exception for the month's literal last day.

Root-cause fix, not a day-29 patch: retiled every fixture's open/due window in `scripts/seed/main.go` so all five states render on any day of any month:

- **ALUGL** (rent) due on the 1st → overdue from the 2nd through month-end
- **ENERG** opens the 1st (was the 5th) → open days 1-15
- **WATER** due the 21st (was the 20th), **NFLIX** due the 30th (was the 28th) → together with ENERG, open covers days 1-30
- **SEGUR** opens the month's last day (upcoming)

The test now excepts only the two dates that are mathematically impossible: upcoming on the month's last day (already excepted), and overdue on the 1st (new — no due date lands before the 1st).

Verified: full suite green, `gofmt`/`go vet` clean, and a scratch script simulated the window arithmetic over every day of 2026-2028 (short months, the 2028 leap year included) confirming zero coverage gaps.

Committed on branch `fix/seed-recurring-tiling` as `068e79a` — `fix(seed): tile recurring windows so every state renders daily` — then opened as PR #5, a CI prerequisite for [[decisions/0022-setup-skills-installs-ai-agent-finance-skills]]'s PR #6 (both touch `scripts/seed`, and master's suite was red on this test on several days of the month). The user approved and merged PR #5.

Note: an existing dev `pecunia.dev.db` keeps the old windows, since the seeder skips codes that already exist — delete and reseed to see the new coverage locally.

Links: [[sessions/85b098e4-8278-4b05-8279-fbda23de2fcd]] · [[decisions/0011-recurring-bills-derived-from-payments]] · [[decisions/0022-setup-skills-installs-ai-agent-finance-skills]] · [[rules/tdd]]

## Update: a duplicated day offset re-broke the future-date clamp

Session: [[sessions/e22cf9b6-6521-4261-b791-0815460c124e]].

PR #7's CI failed on `TestSeedRecurring/nothing_is_dated_into_the_future`: "Internet is dated 2026-09-05, which has not happened yet" (today was 2026-09-04). Root cause in `seedRecurringPayments`: `paidOn` was already computed with `paidOn.AddDate(0, 0, 2)` and clamped (`if paidOn.After(now) { paidOn = now }`), but the `Transaction` literal's `Date` field re-applied `paidOn.AddDate(0, 0, 2)` a second time when formatting — pushing the date past `now` and defeating the clamp it had just passed.

Triggered by the `INTNT` (Internet) fixture (`OpenDay: 1, PaidNow: true`): its current cycle opens the 1st, +2 = the 3rd (not after "now" on the 4th, so the clamp didn't fire), then the second +2 pushed it to the 5th — tomorrow. Date-dependent, so it only started failing once the calendar lined up wrong — the same shape of bug as the fixture-window gap above, in a different function.

Fix: drop the second `AddDate(0, 0, 2)` — `Date: paidOn.Format(recurring.DateLayout)`. One-line diff, verified against `go test ./scripts/seed/` and the full suite. Committed `b05fd8c` — `fix(seed): drop double day offset that dated payments into the future`, pushed straight to the open PR's branch (`feat/setup-skills`), which turned the PR's CI green.

Links: [[sessions/e22cf9b6-6521-4261-b791-0815460c124e]]