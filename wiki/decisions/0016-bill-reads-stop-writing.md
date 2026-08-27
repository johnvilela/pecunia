---
tags: [bills, credit-card, transactions, sqlite, performance, decisions]
---

## Decision

Closes gap #2 of [[decisions/0013-data-integrity-fixes-and-known-gaps]]: every
bill read (`kakei s`, `kakei cc bill`, `kakei bg`) wrote — `bills.Ensure` ran
its insert loop and `refreshOpen` rewrote `total, status, updated_at` for every
open bill, unconditionally, on every read. Root cause: writes kept *payments*
fresh (`refreshBills` → `bills.Refresh`) but a card *charge* never updated its
bill's total, so reads had to catch up. Two fixes:

**Reads write only when something changed.** `refreshOpen` computes the total
and status first and skips the UPDATE when both match the stored row. `Ensure`
starts with one SELECT for the current cycle's row — its standing means every
earlier cycle stands too, since this loop is the only creator and fills
forward — and skips generation entirely when found. A read now writes only on
the first read after a closing date passes: the new cycle's row, and the old
bill's open→closed flip. The world changed; kakei records it.

**A charge refreshes its own bill inside the write tx.** `bills.Charged(db,
cardID, date)` — the charges' counterpart to `Refresh` — resolves the covering
bill (`closes_on = cards.NextDate(date, closing_day)`; a charge on the closing
day belongs to that bill), recomputes the live total, and writes only if it
moved and only while the bill is open. `transactions.chargedBills` mirrors
`refreshBills`, called from Create (each installment row), Update (old and new
rows — a moved date touches two bills) and Delete; account rows (payments,
adjustments, transfer legs) are skipped.

**`Charged` never creates a bill row — the load-bearing rule.** An installment
lands up to 59 months out (`MaxInstallments` 60), and future `card_bills` rows
would break `bills.Open` (which trusts the newest open bill to be the current
one), swell `refreshOpen`'s per-read loop, and let a future `closes_on` fool
`Ensure`'s guard into skipping intervening cycles. A charge whose bill does not
exist yet just waits: `Ensure` creates the row when its cycle arrives, and
`refreshOpen` computes the total on that same, legitimately writing, read.
Closed bills stay frozen either way — [[decisions/0009-bills-as-rows-and-installments-as-transactions]]'s
freeze is untouched.

## Verification

TDD both steps. The staleness tests stamp `updated_at = '2000-01-01'` and
count surviving sentinels across reads (datetime-second granularity makes a
plain before/after compare a false pass). Live on a reseeded dev build:
`kakei s`, `cc bill` and `bg` run back-to-back leave `card_bills`
byte-identical, and `kakei l --entity card_bill` grows only when a bill really
appears. Commits: `fix(bills): stop reads writing when nothing changed`,
`fix(bills): refresh a charge's bill at write time, not on read`.

Links: [[decisions/0013-data-integrity-fixes-and-known-gaps]] · [[decisions/0009-bills-as-rows-and-installments-as-transactions]] · [[decisions/0014-logs-as-a-single-audit-table]] · [[rules/tdd]]
