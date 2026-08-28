---
tags: [credit-card, bills, installments, payments, module, sqlite, tdd]
---

# Credit Card Bill & Installments

A credit card should generate a bill based from the last closing date to the next closing date. It should include all
transactions related to that credit card created on that period. The user must be able to see the closed bills and to
create a transaction to pay that bill (a transaction related to a bill).

With the bills created we should be able to create transactions that can be divided into installments, just for credit card
transactions. Image that i bought a phone divided into 5x and it cost me 1k, i must be able to create one single transaction related
to card X and tell it is divided by 5, from the first transaction date it will repeat on the next 4 bills.

## Commands

```
pecunia credit-card bill                  every card's bills, newest first
pecunia cc b CODE|ID                      that card's bills
pecunia cc b CODE|ID 2026-07              one cycle in detail, with its charges
pecunia cc pay [CODE|ID]                  pay a bill, in full or in part
pecunia t n                               the form now asks for installments
```

## Status: Implemented

Built and verified in the `installments-and-bills` session. Eight design questions went to the
user before any code; the answers and what they cost are in
[[decisions/0009-bills-as-rows-and-installments-as-transactions]].

- **A bill is a real row**, generated on read from the card's closing day —
  `UNIQUE (card_id, closes_on)` is what makes that idempotent, so there is no closing step to
  run. `total` is a snapshot frozen when the cycle closes, and `status` is a stored column
  (`open | closed | partial | paid`).
- **Paying a bill is one ordinary outcome on the account that paid**, carrying
  `pays_bill_id`. That account's balance drops and the card's debt drops with it; because the
  payment is not a card transaction, it can never show up as spending on the next bill. Partial
  payments are just several of them.
- **Installments are N real transactions**, a month apart, sharing a group, with the whole
  amount hitting the card's limit at once the way a real issuer takes it. Every existing path
  works on them unchanged. The odd cents ride on the first; `(3/5)` is rendered, never stored
  in the title.
- **Editing or deleting one asks the scope** — this one, this and the ones after it, or the
  whole series. Title, description, category, tags and kind carry across; each installment
  keeps its own date and amount.
- **`bill`, not `bills`** — five characters would have shadowed a card coded `BILLS`.
- Two things the live run turned up and fixed: an open bill was showing its running total as
  money "left" and was being offered for payment, which `Bill.Owed()` now rules out.
- Test suite per [[rules/tdd]]: schema, cycle math, store, UI and command layers each
  red-then-green, own SQLite file per subtest, and a pinned clock in the bills store because
  every read there is about dates. Verified live against the reseeded dev DB — NUCRD's July
  bill closed at R$290.00, two fifths paid from INTER leaving R$174.00 and status `partial`,
  and a hand-edited charge inside it produced the `≠ the ledger now sums R$500.00` line.

Files: `internal/db/migrations/006_card_bills.sql`, `internal/bills/{bill,store,ui}.go`,
`internal/transactions/{transaction,store,ui}.go`, `internal/cards/card.go`, `cmd/cards.go`,
`cmd/transactions.go`, `cmd/main.go`, `scripts/seed/main.go`.

## Update (session pecunia-f1): month name on the bill table

Full session: [[sessions/b04280f1-f379-437b-bf05-df83f355e2f4]].

- `pecunia cc bill`'s table gained a `MONTH` column between `CARD` and `CLOSES`, derived from
  `ClosesOn`'s month name — a cycle closing 2026-09-10 reads `September`. Changed
  `internal/bills/bill.go` and `internal/bills/ui.go`, pinned by a new case in
  `internal/bills/ui_test.go`.
- Also added to the bill picker, per the follow-up "add month to the picker too".
- Explicitly left out for now: year in the MONTH column (CLOSES already carries it) and month
  on picker/details rows beyond what was asked.
- `go build ./...` and `go test ./...` clean.

Links: [[decisions/0009-bills-as-rows-and-installments-as-transactions]] · [[decisions/0006-credit-card-money-schedule-and-over-limit-model]] · [[decisions/0008-transaction-double-entry-tags-and-filters]] · [[tasks/04-transactions-module]] · [[rules/tdd]]