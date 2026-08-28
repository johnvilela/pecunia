---
tags: [credit-card, bills, installments, payments, schema, sqlite, dates]
---

## Decision

Two features, both settled with the user before any code: a credit card grows **bills**, and a
card purchase can be split into **installments**. Eight design questions were put to the user
up front; these are the answers and what they cost.

**A bill is a real row, not a computed view.** `card_bills(id, card_id, closes_on, due_on,
total, status)` with `UNIQUE (card_id, closes_on)`. The derived alternative was offered and
turned down. The unique key is what makes generation idempotent, and `ON DELETE CASCADE`
takes a card's bills with it.

**Bills are generated on read, not closed by hand.** `bills.Store.Ensure` walks the card's
closing dates from its earliest transaction to the cycle still taking charges and
`INSERT ... ON CONFLICT DO NOTHING`s each one, then refreshes every bill still marked `open`.
Every read path starts there, so a bill can never be missing when something looks for it and
there is no `pecunia cc close` to forget.

**The total is a snapshot, and the drift is shown rather than hidden.** The user chose the
frozen total over a live `SUM`. `Ensure` recomputes an open bill's total on every read and
stops the moment the cycle closes — the status leaving `open` *is* the freeze. That leaves a
closed bill's total able to go stale if a transaction inside it is edited afterwards, so
`bills.Details` compares it against `Store.LiveTotal` and prints `≠ the ledger now sums
R$500.00` when they disagree. The trade is stated on screen instead of being discovered.

**A payment is one transaction, on the account, naming the bill.** The user's own words:
"the bill can be paid from any account, it will generate a normal transaction but this
transaction has the id of the bill". So `transactions.pays_bill_id`, and `applyBalance` gains
one branch: an account outcome that names a bill also lowers that bill's card. This is the
only place in pecunia where a transaction moves a balance it does not name — and it buys back
more than it costs, because a payment is then not a card transaction at all and so can never
show up as spending on the next bill. No exclusion clause anywhere in the totals.

An earlier answer had chosen a linked *pair* of rows (account outcome + card income). The
user's follow-up corrected it to one row, which removed the pair-link column, the
double-count guard in `charges()`, and the orphaning problem when half a pair is deleted.

**Partial payments, and a stored status.** Many transactions may name one bill, so
`paid = SUM(value) WHERE pays_bill_id = ?`. `status` is a real column
(`open | closed | partial | paid`) with a CHECK, written by `Ensure` when a cycle closes and
by `bills.Refresh` whenever a payment lands, changes or goes away. `bills.StatusFor` is the
one function that decides which of the four it is, so the writer and every reader agree.

`Refresh` deliberately does **not** look at the clock: whether the cycle has closed is
already recorded in the status `Ensure` last wrote, so a payment can never accidentally
re-open or close a bill.

**`Owed()` is not `Remaining()`.** Found by running it: an open bill showed its running total
under a `LEFT` column and turned up in `pecunia cc pay`. An open bill is a running total, not a
debt. `Owed()` is `Remaining()` on a closed bill and zero on an open one; it is what the
column shows and what decides whether a bill is offered for payment.

**Installments are N real transactions, and the whole purchase hits the limit at once.** Five
rows a month apart, sharing `installment_group` (the first row's id) with `installment_seq`
and `installment_count` beside it. Every existing path — list, filters, edit, delete, balance
arithmetic, the bills themselves — works on them with no new logic. The full amount moves the
card's balance at purchase time, which is what a real issuer does to the limit, so
`checkLimit` refuses a series whole or not at all and the existing rollback makes the refusal
clean.

`SplitInstallments` puts the remainder on the **first** installment: R$1000 ÷ 3 is 333.34,
333.33, 333.33. The parts always sum back to the original, which is the property the test
pins rather than the shares themselves.

**The position is rendered, never stored in the title.** `Phone (3/5)` comes from
`ui.Position`, so the title stays what the user typed and an edit never has to strip it out.

**`cards.AddMonths`.** `time.AddDate(0, n, 0)` turns 31 January into 3 March, and an
installment series built on it drifts a day at a time. `AddMonths` shifts from the 1st and
puts the day back through the `clamp` that `NextDate` already uses.

**Edits and deletes ask their scope.** `ScopeOne | ScopeForward | ScopeAll`, prompted only
when the row is part of a series. A wider scope carries title, description, category, tags
and kind across; **amount and date stay per-row**, because each installment falls on its own
bill and re-splitting a live series is a different operation. Anything that is not a series
resolves to one row whatever scope was asked for, so callers that do not care never think
about it.

**`bill`, not `bills`.** `bills` is exactly five characters and `runCards` matches
subcommands before falling through to `{CODE|ID}`, so it would have made a card coded `BILLS`
unreachable. `bill` (4) and `pay` (3) cannot collide with a code. One verb covers all three
views: no argument is every card's bills, a `{CODE|ID}` is that card's, and a trailing
`YYYY-MM` is one cycle in detail.

**A new `internal/bills` package.** The import graph stays one-directional —
`transactions → bills → cards` — and `bills` reads the `transactions` table by raw SQL rather
than importing the package that imports it, the same trick `cards/store_test.go` already uses.

**A pinned clock in the bills store.** `Store.now` is a field. Every read there decides what
is still open by comparing against today, and a test that cannot pin that is a different test
every month.

## Known holes

- **A closed bill's total can go stale.** The user's explicit choice; the detail view flags it
  rather than repairing it.
- **Future cycles have no bill row.** `Ensure` stops at the cycle still taking charges, so
  installments 3, 4 and 5 of a five-way split are visible in `pecunia t --card X --all` but not
  as bills until those months arrive. Generating them would need a fifth status for a cycle
  that has not started.
- **A card's opening `balance`** — the amount seeded on the card itself, not from any
  transaction — belongs to no bill. Bills only ever sum transactions.
- **Editing a series does not re-split the amount.** Delete it and record it again.
- **Interactive paths need a human.** `cc pay`, the scope prompt and the installments field
  are `huh`; they are covered through the store, per [[rules/tdd]].

Links: [[tasks/05-installments-and-credit-card-bill]] · [[decisions/0006-credit-card-money-schedule-and-over-limit-model]] · [[decisions/0008-transaction-double-entry-tags-and-filters]] · [[decisions/0001-balance-as-int64-minor-units]] · [[rules/tdd]]
