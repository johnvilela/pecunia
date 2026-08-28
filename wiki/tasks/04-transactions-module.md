---
tags: [transactions, module, tui, sqlite, tdd, balances, tags, filters]
---

# Transactions Module

This module will be responsible for managing user's transactions. It will have commands for create, list, details, edit, delete. All command will have shortcuts.

## Commands

list: "pecunia transactions {DATE || DATE RANGE || TAG || Title}" or "pecunia t"
create: "pecunia transactions new" or "pecunia t n"
edit: "pecunia transactions edit {ID?}" or "pecunia t e {ID?}"
delete: "pecunia transactions delete {ID?}" or "pecunia t d {ID?}"
details: "pecunia transactions {ID?}" or "pecunia t {ID?}"

## Suggested structure

id: int incremental
title: string
description?: string
categoryId?: cateogory id
accountId?: account id
creditCardId?: credit card id
value: int
type: income or outcome
tags: array of strings
date: date (YYYY-MM-DD)
updatedAt, createdAt

## Rules

- a transaction MUST be related to either a account or a credit card, never both at the same time and never without any.
- a transaction can have up to 5 tags on the array
- when related to an account it must update the balance of that account based on the type (income will increase, outcome will decrease)
- when related to a credit card it must update the balance of the credit card based on the type (income iwll increase, outcome will decrease)
- the tags input must be an autocomplete, when the user starts to type it will filter and suggest alternatives already used so it can be reused
- the list command should support filter by date, by range of date, by tag, by text search on the title, by category code, by account, by credit card
- the list will show a compact version while the details will show everything related

## Status: Implemented

Built and verified in the `transactions-module` session ([[sessions/2a7339bb-af86-47f1-a0fb-8fbc097dd9ea]]). Key implementation decisions beyond the
original spec, all written up in [[decisions/0008-transaction-double-entry-tags-and-filters]]:

- **A credit card's balance moves the other way from an account's.** The spec says an outcome
  decreases both, but a card's balance is what the open invoice owes, so taken literally a
  purchase would free up credit. Spending raises the card's debt; paying the bill lowers it.
  The model carries both directions as `Signed()` and `CardDelta()`.
- **Every write is one SQL transaction, and `Update` reverses the old row first.** That single
  shape covers changing the amount, flipping the kind, and moving a transaction to another
  account or from an account to a card. A credit card that declines at its limit refuses a
  transaction that would push it past, by reusing `cards.Card.ValidateBalance` — and the
  rollback means neither the row nor the balance survives the refusal.
- **Filters are flags, not positional.** `{DATE | RANGE | TAG | Title}` cannot express filtering
  by category, account or card, so every filter is a flag and the one positional argument is an
  id. A bare `pecunia t` is the current month, with a footer naming the scope.
- **The tags autocomplete is a filterable multi-select over the tags already in use**, plus a
  free-text input for new ones. `huh`'s input suggestions match the whole value, so they could
  only ever complete the first tag of a list.
- **`transaction_tags` is a join table**; a transaction has no code and no currency of its own,
  and `kind` is the spec's `type` renamed to read as Go.
- **Deleting an account or card that has transactions is refused** with a readable sentence via
  the new `core.FKErr`; deleting a category just unlabels them. Pinned by
  `TestDeleteWhileTransactionsPointAtIt` in both stores' test files, inserting a raw transaction
  row to avoid an import cycle back into `internal/transactions`.
- **`cmd/main.go` gained a shared `withConn` helper**, replacing the three near-identical
  per-module open/defer/close copies that had accumulated in `accounts.go`, `cards.go` and
  `categories.go`.
- **Dev seed fixtures use days-ago offsets instead of fixed month/day literals** — simpler, and
  it keeps the fixtures from bunching on whatever day the dev DB happens to be reseeded.
- Test suite built per [[rules/tdd]]: schema, model, store, UI and command layers each
  red-then-green, own SQLite file per subtest, substring assertions for anything lipgloss
  renders. The balance arithmetic is pinned across create, every shape of edit, and delete —
  and verified live against the reseeded dev DB: `NUCRD` (R$1238.50, three purchases, one
  payment) landed at R$502.40; `INTER` moved from R$4823.50 to R$10851.10.

Files: `internal/db/migrations/005_transactions.sql`,
`internal/transactions/{transaction,store,ui}.go`, `cmd/transactions.go`, `cmd/main.go`,
`cmd/{accounts,cards,categories}.go`, `internal/core/core.go`,
`internal/accounts/store.go`, `internal/cards/store.go`, `scripts/seed/main.go`.

Commits: `feat: add transactions cli with sqlite storage and tui` → `feat(accounts,cards): say
what is blocking a delete` → `feat(seed): add transaction fixtures to the dev database` →
`fix(transactions): tidy the list output and spread the seeded dates`.

Links: [[decisions/0008-transaction-double-entry-tags-and-filters]] · [[decisions/0006-credit-card-money-schedule-and-over-limit-model]] · [[decisions/0007-category-starter-set-seeded-from-go]] · [[rules/tdd]] · [[sessions/2a7339bb-af86-47f1-a0fb-8fbc097dd9ea]]