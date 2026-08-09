---
tags: [transactions, schema, balances, tags, filters, tui, sqlite]
---

## Decision

Transactions are the first module whose writes reach past their own table. A transaction moves
the balance of the account or credit card it names, so the store runs every write inside one SQL
transaction and `Update` reverses the old row before applying the new one.

**A card's balance runs the other way from an account's.** The task spec says an outcome
*decreases* the balance in both cases. Taken literally on a credit card that makes buying
groceries free up credit, because `Card.Available() = Limit - Balance` and a card's balance is
what the open invoice owes ([[decisions/0006-credit-card-money-schedule-and-over-limit-model]]).
So the model carries two deltas: `Signed()` is what an account moves by, and `CardDelta()` is its
negation — spending raises the debt, paying the bill lowers it. Verified end to end: NUCRD seeded
at R$1238.50 with three purchases and one payment lands at R$502.40.

**One SQL transaction per write, and `Update` reverses first.** `applyBalance(tx, t, sign)` takes
`+1` to apply a transaction and `-1` to take it back, so `Update` is `applyBalance(old, -1)` then
`applyBalance(new, +1)`. That one shape covers an edit that changes the amount, flips the kind,
moves the transaction to another account, or moves it from an account to a card — all of which
otherwise need their own arithmetic.

**A card can refuse a transaction.** After moving a card's balance, the store reads the card back
inside the same transaction and calls `cards.Card.ValidateBalance()`. Reusing module 02's rule
rather than restating it is what keeps one answer in the codebase about what a limit means. The
rollback is what makes the refusal clean: a rejected transaction leaves neither a row nor a moved
balance.

**Exactly one target, enforced by the schema and by the form's shape.** The table carries
`CHECK ((account_id IS NULL) <> (card_id IS NULL))`, and the form offers accounts and cards in
**one** select rather than two fields plus a validator — so "never both, never neither" is not a
rule anything has to check. Frozen accounts are left out of the options: a frozen account is out
of play, and filing new money through it is almost always a slip. It is still reachable by
editing an existing transaction, which is deliberate — nothing should trap a row on an account
that was frozen after the fact.

**Deleting a target is refused; deleting a category is not.** `category_id` is
`ON DELETE SET NULL`, because a category is a label and losing one is not losing the transaction.
`account_id` and `card_id` are left at SQLite's default `NO ACTION`, which refuses the delete —
a transaction must always have exactly one target. `core.FKErr` turns SQLite's
`FOREIGN KEY constraint failed (787)` into a sentence the caller supplies, so
`kakei ac d INTER` says the account still has transactions. This amends what
[[decisions/0007-category-starter-set-seeded-from-go]] predicted the foreign keys would do to
category deletes.

**Tags are a join table.** `transaction_tags(transaction_id, tag)` with `ON DELETE CASCADE` and
the pair as the primary key, which is also what stops a tag being listed twice on one row. Reads
collapse it with a `group_concat` sub-select so a listed transaction still costs one query.
`NormalizeTags` lowercases — `Food` and `food` have to be the same tag or the autocomplete is
useless — sorts, dedupes, and strips commas, which is what makes splitting `group_concat`'s
output safe.

**The autocomplete is a filterable multi-select plus a free-text input.**
`huh.NewInput().Suggestions()` matches against the *whole* input value, so it can only complete
the first tag in a comma-separated list. `huh.NewMultiSelect[string]().Filterable(true)` over the
tags already in use does what the spec asked for — `/` then type to narrow, space to pick, capped
at `MaxTags` — and a plain input beside it takes tags that do not exist yet. The multi-select is
left out of the form entirely when no tag exists, because an empty select is worse than no
select.

**Filters are flags, not positional.** The spec's `kakei t {DATE | RANGE | TAG | Title}` cannot
express filtering by category, account or card — a bare word is ambiguous between a code and a
title. Every filter is a `flag.FlagSet` flag; the one positional argument is an id. `--category`,
`--account` and `--card` take a `{CODE|ID}` and go through each module's own `Resolve`, so
`--category food1` works the way `kakei ct food1` does. The flag set's own usage dump goes to
`io.Discard`: it would print the flags a second time in single-dash spelling, next to the error
`report()` already prints.

**A bare `kakei t` is this month.** Years of history scrolling past is not a list. The footer is
what keeps the scope from being a secret — `August 2026 — 5 transaction(s). Widen with: kakei t
--all, or kakei t --month 2026-07`. Any explicit filter replaces the default rather than adding
to it. An empty month and an empty ledger get different messages: one says how to widen, the
other says how to start.

**A transaction has no code and no currency.** Every other module has a 5-character code; a
transaction is referenced by id only, which is what the spec asks for and what a row nobody names
out loud actually needs. Currency is inherited from whichever target is set, so the amount's
minor-unit scale is never a question the form has to ask.

**`kind`, not `type`.** The spec calls it `type`. `Type` is a poor Go field name and SQLite has no
preference, so one name in both places beat two. `value` is stored positive with `kind` carrying
the sign, exactly as the spec's structure reads.

**Reads denormalise through joins.** The list query `LEFT JOIN`s categories, accounts and credit
cards, and `Transaction` carries a `Ref{ID, Code, Name, Color}` for each. That is what lets
`internal/transactions/ui.go` render a row without importing the other three packages for
display, and what keeps a 16-row list at one query instead of 49.

## Known holes

- **Nothing reconciles.** If a balance is ever edited by hand through `kakei ac e`, the sum of the
  transactions no longer explains it. There is no `kakei t check` and no ledger invariant — the
  balance is authoritative and the transactions move it, which is enough until it is not.
- **`kakei t` with no id opens no picker.** A bare `kakei t` lists, the same as `kakei ct` does,
  so the picker is only reachable through `edit` and `delete`. That is the spec's own collision
  between `list: kakei t` and `details: kakei t {ID?}`, resolved the way module 03 resolved it.
- **Interactive paths need a human.** `t n`, `t e` and `t d` open a `huh` form, the picker or the
  confirm prompt, none of which run without a TTY. They are covered through the store, per
  [[rules/tdd]].

Links: [[tasks/04-transactions-module]] · [[decisions/0006-credit-card-money-schedule-and-over-limit-model]] · [[decisions/0007-category-starter-set-seeded-from-go]] · [[decisions/0005-internal-core-shared-kernel]] · [[gotchas/huh-form-skips-validators-on-eof]] · [[rules/tdd]]
