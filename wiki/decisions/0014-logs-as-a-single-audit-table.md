---
tags: [logs, audit, schema, sqlite, json, transactions, decisions]
---

## Decision

One `logs` table (migration `012_logs.sql`) records every logical action as one
row: source, action, entity, entity_id, changes, created_at. Built from
[[tasks/11-logs-module]]; groundwork for the balance-reconcile fix
([[decisions/0013-data-integrity-fixes-and-known-gaps]]) and the planned AI
integration.

**One row per logical action, not per SQL row.** A transfer logs once as entity
`transfer` under its group id; a 6-installment purchase logs once; a bill
payment logs once (it funnels through `transactions.Create`, so no extra hook
exists for it). `applyBalance`, `bills.Refresh`, tag rewrites and group
backfills are an action's mechanics and never log. Deleting either leg of a
transfer logs one `transfer deleted` — `scoped()` already expands a leg to
both, so `transactions.Delete` branches on `targets[0].IsTransfer()`.

**`source` says who caused it** — `user`, `system`, or `ai` (reserved; nothing
writes it yet). System covers `bills.Ensure` generating card bills on read and
`categories.Seed` laying down the starters. `categories.Create` is the one
write with two authors, so it takes the source as a parameter; every other call
site hardcodes its constant.

**Changes are JSON, and only what moved.** The first `encoding/json` in the
repo — deliberately, since machine-readable output is a named prerequisite for
the chatbot ([[decisions/0013-data-integrity-fixes-and-known-gaps]]). The diff
is hand-listed fields through `logs.F`/`logs.Diff`, not reflection: the structs
carry computed fields (`Budget.Spent`, `Goal.Net`, joined `Ref` names) that
reflection would log as spurious edits, and its failure mode is silent junk in
the trail. `Diff` drops equal pairs, which also makes a no-op edit or a
same-state archive log nothing, for free.

**Schema choices.** No foreign key on `entity_id` — the trail outlives what it
describes, or a delete would erase its own record. No CHECK on `entity` — the
next module should not need a migration to log; the vocabulary is enforced at
the `--entity` flag, where a typo can actually happen. `source` and `action`
are CHECKed: closed sets. `goal_target_log` and `budget_amount_log` stay
untouched beside it — value history with meaning of its own, not an audit
trail.

**Hooks live in the stores**, at each top-level action. `logs.DB` is the Exec
half of the `bills.DB` shape, so a write already inside a `*sql.Tx` passes the
tx and its log rolls back with it — a refused installment series or a frozen
account leaves no trail. Single-statement writes log via a second Exec on the
same handle (marked `ponytail:` in `accounts.Create`): one connection and a CLI
lifetime keep the gap negligible. `bills.Ensure` logs only when
`RowsAffected() == 1`, since its insert is `ON CONFLICT DO NOTHING` and every
read path runs it.

`kakei logs` / `kakei l` lists newest first, default 10; filters `--entity`,
`--id` (requires `--entity` — an id is only an id of something), `--action`,
`--source`, `--from`/`--to`, `--limit`. `--to` compares
`created_at < date(?, '+1 day')` so the named day is included despite the
stored time of day.

## Verification

TDD throughout, one red→green cycle per module hook, full suite green,
`gofmt`/`go vet` clean per commit. Live on a reseeded dev build: `kakei s`
generated card bills logged as `system`; the seeder's freeze, archive and
target edits appeared with only the moved fields; every filter and both
validation errors checked at the terminal.

Commits: `feat(logs)` ×7 — table+package, command, accounts/cards, categories+
seed, transactions/transfers, goals/recurring/budgets, card bills. Also
`test: survive being run late in the month`, fixing two date-dependent tests
found failing on Aug 27 (a bill-cycle assumption and a seed fixture that could
never render "upcoming" late in the month — a `SEGUR` fixture opening on the
clamped 31st now covers it every day but the month's last).

Links: [[tasks/11-logs-module]] · [[decisions/0013-data-integrity-fixes-and-known-gaps]] · [[decisions/0010-goal-progress-summed-from-the-ledger]] · [[rules/tdd]]
