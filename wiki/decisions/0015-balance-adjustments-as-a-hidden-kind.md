---
tags: [transactions, accounts, cards, money, schema, sqlite, migrations, decisions]
---

## Decision

Closes gap #1 of [[decisions/0013-data-integrity-fixes-and-known-gaps]]: the
account balance was the one number a user could silently move out from under
the ledger. Now `kakei ac edit` never writes the balance — a changed balance is
**filed as an adjustment transaction**, and after creation only the ledger
moves a balance. Cards go further: `cc edit` lost its Balance field outright; a
card balance is ledger-only after creation, no adjustment mechanism at all.

**`KindAdjustment` is a hidden third kind.** No form offers it — the kind
select still shows two options — and it is the one kind whose **value is
signed**: an adjustment is its own direction, and inventing two kinds to say up
and down would put the sign back into a name. `Signed()` returns the value
verbatim for it, so `applyBalance`, delete-revert and rendering all follow.
`Amount()` now derives sign and colour from `Signed()` rather than the kind
(which also fixed the would-be `+R$-50.00` double sign).

**An adjustment counts toward nothing.** `validateAdjustment` (same shape as
`validateTransfer`) bars card targets, categories, goals, bill payments,
recurring links, installments and transfer groups — so the budgets, goals and
bills queries needed no change: they all match on columns an adjustment never
carries. The summary skips it the way it skips transfers: listed in the day's
ledger, absent from In/Out/MTD.

**Delete yes, edit no.** Deleting reverts the balance through the normal path.
Editing is refused in the store (either direction — nothing becomes an
adjustment either) *and* in the cmd layer with a message naming `kakei t d ID`;
without the store guard, huh's two-option select would silently rewrite a
stored adjustment to outcome on submit.

**The flow**: `ac edit` keeps its Balance field as the way to type the new
figure; the cmd layer diffs old vs new, asks an optional note
(`accounts.AdjustmentNote`), files the adjustment first — so a frozen account's
`refuseFrozen` aborts before anything is written — then updates the rest.
`accounts.Store.Update` and `cards.Store.Update` dropped `balance` from their
UPDATE and their trail diff; the filed adjustment is what lands on the
[[decisions/0014-logs-as-a-single-audit-table]] trail instead. Card limit edits
are now judged against the *stored* balance, not the caller's.

**Migration 013 is the repo's first table rebuild.** SQLite cannot
ALTER a CHECK, so `013_adjustments.sql` recreates `transactions` whole (kind
CHECK gains 'adjustment'; value CHECK becomes signed-for-adjustment-only),
copying rows **with explicit ids** — installment_group, transfer_group and
transaction_tags all point at them — and recreating the seven indexes. The
runner (`db.migrate`) now wraps the batch in `PRAGMA foreign_keys = OFF` … 
`foreign_key_check` … `ON`, because the pragma is a no-op inside a transaction
and, left ON, `DROP TABLE transactions` would fire transaction_tags'
`ON DELETE CASCADE` and wipe every tag. `TestAdjustmentRebuildPreservesData`
replays the pre-013 migrations by hand, fills the tables, and pins that the
rebuild preserves rows, ids, tags and referential integrity.

## Verification

TDD throughout, full suite green, `gofmt`/`go vet` clean per commit. The
populated dev database was migrated **in place** (no reseed): 47 transactions
and 49 tags before and after, `foreign_key_check` empty. Live after a reseed:
the seeded adjustment renders `-R$15.00` red with no double sign, `kakei s`
totals exclude it, `kakei t e` refuses it by name, and the trail carries it.

Commits: `feat(db)`, `feat(transactions)`, `feat(accounts)`, `feat(cards)` (+
`test(cards)` retargeting two balance-edit cases), `feat(summary)`,
`feat(seed)`.

Links: [[decisions/0013-data-integrity-fixes-and-known-gaps]] · [[decisions/0014-logs-as-a-single-audit-table]] · [[decisions/0001-balance-as-int64-minor-units]] · [[rules/tdd]]
