---
tags: [logs, audit, module, sqlite, tdd, cli]
---

# Logs module

An audit trail: every logical action in kakei — create, edit or delete, by the
user or by kakei itself — writes one log row. Spec settled through Q&A before
any code, the way [[tasks/07-recurring-bills-module]] was.

## The row

- `source` — who caused it: `user`, `system`, or `ai` (reserved for the planned
  MCP integration; nothing writes it yet).
- `action` — `created`, `edited` or `deleted`.
- `entity` + `entity_id` — what it happened to. One of: account, card,
  category, transaction, transfer, goal, recurring, budget, card_bill.
- `changes` — on an edit only: the fields that moved, as JSON keyed by field
  with old and new values (`{"name":{"old":"Cash","new":"Wallet"}}`). Created
  and deleted rows carry entity and id alone.
- `created_at`.

## The rules (user-confirmed)

- **Everything logs** — including system actions: card bills generated on
  read, the starter categories seeded by `kakei setup`.
- **But each logical action logs exactly once.** A transfer is one entry, not
  two transactions; an installment purchase is one entry however many rows it
  wrote; a bill payment is one entry. Side-effects inside an action — balances
  moved, bill totals refreshed, tags rewritten — are never logged separately.
- The trail outlives what it describes: deleting an account keeps its rows.

## The command

`kakei logs` / `kakei l` — last 10, newest first. Flags: `--entity`, `--id`
(requires `--entity`), `--action`, `--source`, `--from`/`--to` (both
inclusive), `--limit`.

Design write-up: [[decisions/0014-logs-as-a-single-audit-table]].
