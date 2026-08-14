---
tags: [transfers, transactions, sqlite, tdd, planning]
---

# Transfers

Money moving between two accounts you own. It is not income and it is not an
expense — nothing was earned and nothing was consumed — which is exactly why
recording it as an ordinary pair of transactions today inflates both totals and
makes a month read worse and better than it was.

User: "Lets work with Transfers. This should be another category of
transactions. Ask me anything about it." Five design questions went back before
any code; the answers are the decisions below.

## Status: Implemented

Built TDD on the plan below, which was written first and followed. What the
build found that the plan did not:

- **The group cannot exist when the legs are validated.** It is the outgoing
  leg's id, which does not exist until the row does — so `IsTransfer()` is false
  at exactly the moment the transfer rules need to fire. `Store.Transfer` sets
  the group to a placeholder for the validation pass and clears it again before
  writing, the same problem `installment_group` has and solves the same way.
- **The installment guard already refused a transfer leg**, with "only a credit
  card purchase can be split into installments" — a transfer leg is an account,
  so the existing rule fires before the new one and already says the right
  thing. The new rule stays as the guard for a hand-edited row.
- **The movement belongs in the SOURCE column, not CATEGORY.** The first draft
  put the arrow where a category would go, since a transfer has none. It reads
  wrong under a header saying CATEGORY. It goes in SOURCE — which is where the
  account already goes — and the category column stays empty, which is the
  truth.
- **`scoped` is a free function, not a method**, so the sibling lookup a delete
  needs had to be one too. `legsOfTx` takes the `*sql.Tx`; `legsOf` is the
  pooled twin for reads.
- **The no-category rule really did keep budgets out of it.** No change to that
  module, as predicted — and `TestTransfersNeverLandInABudget` now guards it
  from over there, because the rule that makes it true lives in another package.

Verified live against the seeded dev database: both legs listed with opposite
arrows, the fee shown on the details card, and the summary totalling neither leg
while still listing both. One transfer fixture was added to `scripts/seed`,
carrying a R$5.00 fee so that branch has something to render.

Not built, as planned: collapsing the pair into one listed row, cards on either
end, stored exchange rates, a goal on the outgoing leg, and repeating transfers.

## Commands

```
kakei transactions transfer      | t tr        record a transfer
kakei transactions edit {ID}     | t e {ID}    edit one, both legs together
kakei transactions delete {ID}   | t d {ID}    delete one, both legs together
kakei transactions --transfers                 list only transfers
```

## Decisions

- **Two rows, and each one names the other end.** A transfer is an outcome on
  the source and an income on the destination, sharing a `transfer_group` — the
  id of the outcome row, exactly the shape `installment_group` already has
  ([[decisions/0009-bills-as-rows-and-installments-as-transactions]]). The store
  joins the sibling back in on every read, so either row on its own already
  carries the counterparty: the origin is recorded, never inferred, and never
  costs a second query.

  Asked directly, with the one-row alternative on the table. Two rows keep
  `applyBalance`, the `(account_id IS NULL) <> (card_id IS NULL)` CHECK and
  `Signed()` all untouched — a single row with from/to columns would have broken
  every one of them, plus every renderer that assumes one target.

- **The outcome row is the origin record.** It is the leg that names where the
  money came from, and it is the one whose id the group is. Given a transfer,
  the source is a fact of the row rather than a convention about ordering.

- **Accounts only.** Asked and confirmed. A transfer moves money between things
  that hold money; paying a credit card stays `kakei cc pay`, which already
  settles the right bill and already keeps the payment off the next statement.
  Two ways to pay a card would be two ways to get it wrong — and a card payment
  that named no bill would leave the balance and the bill status disagreeing.

- **Two amounts, both typed.** Asked and confirmed. R$500.00 leaves and $92.00
  arrives; the implied rate is used and never stored, because there is no rate
  anywhere in kakei and this is not where one starts. The same mechanism covers
  a fee without a field for one: R$500.00 out and R$495.00 in, same currency, is
  a R$5.00 TED fee, and the renderer says so.

- **No category, and that is what keeps budgets right.** Asked and confirmed. A
  category that never counts toward anything is a lie, and a transfer counts
  toward nothing.

  It is also the whole budget fix, for free: `budgets`' spend subquery matches
  on `t.category_id = b.category_id`, and a NULL category matches no budget ever
  ([[tasks/09-budgets-module]]). **No change to the budgets module at all** —
  the rule enforces itself.

- **A transfer may feed a goal, on the destination leg only.** Asked, and this
  is the reading of it: money *arriving* somewhere is what counts toward a goal.
  Moving R$500.00 into the savings account is progress on saving; the same
  movement must not climb a goal for paying a card down, which is what letting
  the outcome leg carry one would do. The goal's currency must match the leg it
  is on, which is the rule `transactions.Validate` already enforces.

- **Both legs go together, always.** Unlike an installment series there is no
  "just this one": half a transfer is money vanishing from the ledger. Editing
  either leg edits both, deleting either deletes both, and the balances reverse
  on both.

- **Both legs are listed.** `kakei t` shows the transfer twice, once per
  account, each with its arrow. Honest to the ledger, costs no collapse logic,
  and it is what makes `--account NUBON` show the money leaving without having
  to reason about a row that is only half about NUBON.

## Schema

One migration, no new table.

```sql
-- 011_transfers.sql
--
-- Money moving between two accounts you own: an outcome on the source and an
-- income on the destination, sharing this group. The group is the id of the
-- outcome row -- the leg the money left from -- so the origin of a transfer is
-- a fact of the data rather than a convention about which row came first.
--
-- Two rows rather than one because that is what keeps every existing rule
-- intact: each leg still names exactly one target, still carries a positive
-- value with the direction in its kind, and still moves one balance.
--
-- NULL on every transaction that is not a transfer, which is nearly all of them.
ALTER TABLE transactions ADD COLUMN transfer_group INTEGER;

-- Every read of a transfer is the sibling lookup: same group, other id.
CREATE INDEX transactions_transfer_group ON transactions (transfer_group);
```

Nothing constrains a group to exactly two rows — SQLite cannot say it — so the
store is the only thing that ever writes one, and it writes both or neither.

## Model

```go
func (t Transaction) IsTransfer() bool { return t.TransferGroup != 0 }

// Counterpart is the other leg, joined in on read: its account and its value,
// which is not this leg's value when the currencies differ or a fee was taken.
type Counterpart struct {
    Ref      Ref
    Value    int64
    Currency string
}
```

`Validate` gains the rules a transfer leg has to hold to:

- no category — it counts toward no budget, so it may not claim one
- no `pays_bill_id` and no `recurring_id` — settling a bill is not a transfer
- not an installment — there is nothing to spread
- an account, never a card
- a goal only on the income leg, and only in that leg's currency

## Store

A dedicated path rather than bending the installment one. The two share a
column shape and nothing else: installments share everything but date and
value, and the legs of a transfer share almost nothing but the title, the
description, the date and the tags.

```go
type Transfer struct {
    Title, Description, Date string
    Tags                     []string
    From, To                 Ref   // accounts, never cards
    FromValue, ToValue       int64 // each in its own side's minor units
    Goal                     Ref   // optional, on the destination leg
}

func (s *Store) Transfer(t *Transfer) error       // writes both legs, or neither
func (s *Store) UpdateTransfer(t Transfer) error  // both legs together
```

Delete reuses the existing `Delete`, with `scoped` taught that a transfer row
brings its sibling whatever the `Scope` asked for — deleting is symmetric, both
balances reverse, and there is nothing on either leg to preserve.

`columns` gains one more outer join, on `transfer_group`, beside the five it
already carries.

## What has to stop counting transfers

The point of the whole feature, and the only two places it matters:

- **`summary.collectLedger`** — skip transfer legs when totalling `In`, `Out`
  and `MTD`. Without this the feature changes nothing: the pair still inflates
  both directions, which is the bug.
- **`transactions.Table` / `Details`** — render the arrow and the counterparty
  instead of the category column a transfer does not have, and show the fee when
  the two legs differ in the same currency.

**Budgets need no change**, per the no-category decision above. Goals need none
either: a transfer that names a goal is counted by the sum goals already runs.

## UI

The form asks From and To before either amount, so each amount's validator
already knows the currency to read it at — the ordering `cards.Form` and
`budgets.Form` both use. The second amount is pre-filled to match the first and
is editable, which is what covers both a fee and a cross-currency move without a
conditional field huh would have to redraw underfoot.

Frozen accounts are not offered on either end: they are out of play everywhere
else, and a transfer into one is money parked somewhere the app says is closed.

A listed transfer reads as its movement rather than as a category:

```
DATE        TITLE            AMOUNT       ACCOUNT
2026-08-14  Transferência    -R$500.00    NUBON → INTER
2026-08-14  Transferência     R$500.00    INTER ← NUBON
```

## Tests

TDD as [[rules/tdd]] has it, with the cases that would otherwise be found in
production:

- `transaction_test.go` — `Validate` refuses a transfer with a category, with a
  bill, with a card, as an installment, and with a goal on the outcome leg.
- `schema_test.go` — the group column and its index exist; a leg may be deleted
  only through the store.
- `store_test.go` — both legs written or neither; the balances move both ways
  and sum to zero across the pair in one currency; a cross-currency transfer
  moves each side by its own amount; editing one leg edits both; deleting one
  deletes both and reverses both balances; a rolled-back transfer leaves no row
  and no moved balance.
- `summary_test.go` — a transfer is in neither `In` nor `Out` nor `MTD`, and the
  ledger still lists both legs.
- `budgets/store_test.go` — a transfer between two accounts never lands in a
  budget, which is the regression guard on the no-category decision.
- `goals/store_test.go` — a transfer's income leg feeds its goal.
- `ui_test.go` — the arrow, the counterparty and the fee render.

## Out of scope

- **Collapsing the pair into one listed row.** Both legs are listed; see the
  decision above. Revisit if the ledger reads too heavy.
- **Cards on either end**, and any second way to pay a statement.
- **Stored exchange rates.** The two amounts are typed and the rate is never
  kept — the same call every other module has made.
- **A transfer feeding a goal from its outcome leg.**
- **Scheduled or repeating transfers.** A standing transfer into savings is a
  real thing and is a recurring-bills-shaped feature, not this one.
