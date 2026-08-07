---
tags: [credit-card, money, schema, ux]
---

## Decision

The credit-card domain model, settled during planning and then corrected once mid-session on user feedback:

**Schedule.** `closing_day` / `due_day` are `INTEGER CHECK (x BETWEEN 1 AND 31)` — day-of-month, not absolute `TEXT` dates. A card closes on the same day every month; an absolute date would be stale the moment the month rolls over. Clamping a day like 31 to a shorter month happens only at compute/display time (`NextDate`), never at storage. No cross-field validation: a due day before the closing day just means "next month", which is the normal case.

**Money.** `balance` = the amount already used on the open invoice (mirrors `accounts.balance`: a stored quantity, minor units, scale from currency). `Available()` is computed, never stored: `Limit - Balance`. The column is named `credit_limit`, not `limit` — `LIMIT` is a SQLite keyword.

**No freeze.** The spec has no freeze verb for cards, so `Store.List` is a plain `ORDER BY name`, no `is_frozen` machinery.

**Codes are unique per table.** `kakei cc INTER` and `kakei ac INTER` can coexist — no shared namespace between `accounts` and `credit_cards`.

**No FK to an account.** Deferred — the spec's suggested structure has no account field, and the notes defer rules to the transactions module. `PRAGMA foreign_keys = ON` is already set repo-wide, so adding `account_id` is a one-line `ALTER TABLE` migration whenever it's actually needed.

**Color semantics — corrected mid-session.** The first version colored `Available()` green when positive, red when negative — the same rule as `accounts.balanceColor`. User: "A cc will never be positive, thus it will never be green. On the last cc card i don't know if i have 1550 avaliable to use or if the current used value it 1550." Redesigned:
- Table header `AVAILABLE` → `USED / LIMIT` — names both numbers so nothing has to be inferred from position.
- No green anywhere on a card, ever (pinned by a test that checks every balance against the green hex).
- Red only once `Balance > Limit` (over the limit) — not a gradient, a binary threshold.
- Detail card spells out the state instead of showing a signed number: `R$1120.00 over the limit`, not `R$-1120.00 available`.
- Usage bar gained a percent, **truncated, not rounded** — a card reads `100%` only once it is actually full, never a rounded-up 99.6%.

A follow-up user question ("Why only ITAU1 is red on this screenshot?") confirmed this was the rule working as designed: every other seeded card had comfortable room (next-highest usage was 26%, ITAU1 was at 137%), not a card being missed.

**Over-limit allowance.** User: "we need to add a flag on the cc table to tell that this card can be used above the limit. By default is will be set false." Added `over_limit_allowed INTEGER NOT NULL DEFAULT 0` via migration `003_credit_card_over_limit.sql`. Enforced as a real constraint, not just a display flag: `Card.ValidateBalance()` runs inside both `Store.Create` and `Store.Update` in `internal/cards/store.go`, refusing any balance over the limit unless the flag is true — and also refuses turning the flag off while the card is currently over its limit. The form asks for the allowance before the balance, so the balance field's validator can consult it. Marked in the table and detail card with an `↑` beside the limit it qualifies, itself uncolored (the mark says "may go over", not "is over" — a card can carry the allowance and still be nowhere near its limit).

## Why

All four base items came from the initial planning pass (schedule, money, no-FK); the color redesign and the over-limit flag came from direct user correction during the build. Full session narrative: [[sessions/ce07d7cb-4a82-4381-89cb-9ad513a7159d]].

## Verification

The over-limit rule immediately caught real data: the seed fixture `ITAU1` (R$4120 used against a R$3000 limit) could not exist with the flag off under the new guard, so it became `OverLimitAllowed: true`. The enforcement was proven by temporarily disabling the guard in `Card.ValidateBalance` and confirming the relevant tests failed, then restoring it. `gofmt`/`go vet`/`go test ./...` clean throughout.

Links: [[tasks/02-credit-card-module]] · [[decisions/0005-internal-core-shared-kernel]] · [[decisions/0001-balance-as-int64-minor-units]]