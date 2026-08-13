---
tags: [goals, schema, progress, currency, tui, sqlite]
---

## Decision

A goal stores what it is aiming at and never where it has got to. There is no
`current_value` column: progress is `SUM(CASE WHEN kind = 'income' THEN value
ELSE -value END)` over the transactions carrying its `goal_id`, worked out on
every read. The spec's *Suggested structure* asks for `currentValue`; it was
dropped deliberately.

## Why

[[decisions/0008-transaction-double-entry-tags-and-filters]] names the one hole
in the accounts model: "if a balance is ever edited by hand through `kakei ac e`,
the sum of the transactions no longer explains it." A stored goal total would be
a second copy of that hole, and a worse one — an account balance is authoritative
because money really is in the account, but a goal is nothing except what was
filed against it. Computing it means the goal cannot disagree with the ledger,
and there is nothing to reconcile, refresh or repair. Verified live: editing a
linked transaction from R$1200.00 to R$2000.00 moved `Notebook novo` from
R$2100.00 to R$2900.00 with no write to the goals table at all.

The cost is one subquery per listed goal, which is why `transactions_goal`
exists.

## Details

**The sum is one correlated subquery inside `columns`**, not a join with a
`GROUP BY`. That keeps `List` and `Get` the same plain `SELECT` — with a
`GROUP BY`, a `Get` for an id that is not there comes back as one all-NULL row
instead of no rows, which is a trap rather than a saving. `List` is still one
query for every goal and every progress.

**The kind flips the sign, and it flips it in Go.** SQL always returns income
minus outcome as `Goal.Net`; `Goal.Progress()` negates it for a `paying` goal, so
a goal for saving and a goal for paying down both read as climbing toward a
positive target. Keeping the flip in Go leaves the SQL identical for both kinds,
puts the rule beside `Verb()` — the other thing the kind decides — and lets every
view case downstream be a struct literal with no database.

**A goal carries its own currency, and only matching transactions may link.**
Nothing in kakei converts between currencies ([[decisions/0001-balance-as-int64-minor-units]]),
so a mixed-currency total would be meaningless. Three layers hold it up, each
closing a different door:

| Layer | What it does |
|---|---|
| `FormData.goalOptions(currency)` | Only same-currency goals appear in the select |
| `Store.fillGoalCurrency` | Overwrites both currencies from the database before validating |
| `Transaction.Validate` | Compares them and refuses |

The fill is what makes the check real. A `Transaction`'s `Currency` is inherited
from its target by the read joins and is *not* populated on the write path, so
`Validate` alone would pass a hand-built `Transaction{Goal: Ref{ID: 3}}` with an
empty currency straight through. The comparison is strict — an empty
`GoalCurrency` fails too, because that is exactly what a caller who only knew the
id leaves behind. Same reasoning as
[[gotchas/huh-form-skips-validators-on-eof]]: the form is not a guard.

**A goal's currency freezes once anything is linked.** `Store.Update` refuses the
change and says how many transactions are holding it, and the form drops the
currency field entirely rather than offering something that will be refused. The
kind is *not* frozen: flipping it inverts the progress, which is honest rather
than surprising.

**Id-only, no code, no colour.** Every other module except transactions has a
5-character code. Goals follow transactions instead, which is what the spec asks
for. It also kept the `huh` code-uniqueness validator at three copies rather than
tripping the fourth-copy lift the `ponytail:` note in
`internal/categories/ui.go` names. The details card is bordered in the goal's
*state* — green once reached, red while it is going backwards — since there is no
colour of its own to use.

**Deleting a goal is never refused.** `goal_id` is `ON DELETE SET NULL`, the same
call `category_id` makes: a goal is a label on the transactions that fed it, so
losing it unlinks them rather than taking money that really moved. That makes the
confirmation the only place the cost can be said, so it counts them — "3
transaction(s) keep their money and lose the link."

**`Name`, not `Title`.** The spec says `title`. `core.ValidateName` already
exists and `transactions.ValidateTitle` is a hand-copy of it whose own comment
asks for a `core.Required` lift if a third field needs the same sentence. Naming
the field `Name` reuses the existing validator, matches accounts, cards and
categories, and needs no lift. Same call [[decisions/0008-transaction-double-entry-tags-and-filters]]
made about `kind` over `type`, which the goals table follows too.

**The goal is a label on a series.** An installment purchase carries the goal on
every row, and an edit spreads it exactly as far as the scope says — `ScopeAll`
links the whole series, `ScopeOne` links just that installment.

## Known holes

- **`kakei cc pay` does not feed a paying goal.** `PayBill` builds its own
  `Transaction` in the store and sets no goal, so a bill payment has to be edited
  afterwards to name one. A `cc pay --goal` would close it.
- **No goal column in `kakei t`.** The goal shows on the details card and filters
  with `--goal ID`, but the list table has none — most rows would have it empty.
  `TestTableHasNoGoalColumn` pins that as a decision rather than an oversight.
- **`Percent` can overflow** on a goal whose target is a handful of satoshis with
  most of a Bitcoin filed against it (`Progress()*100/Target` past ~9.2e16).
  `cards.usagePct` has the same shape and the same non-guard.
- **Interactive paths need a human.** `g n`, `g e` and `g d` open a `huh` form,
  the picker or the confirm prompt. They are covered through the store, per
  [[rules/tdd]].

Links: [[tasks/06-goals-module]] · [[decisions/0008-transaction-double-entry-tags-and-filters]] · [[decisions/0001-balance-as-int64-minor-units]] · [[decisions/0005-internal-core-shared-kernel]] · [[gotchas/huh-form-skips-validators-on-eof]] · [[rules/tdd]]
