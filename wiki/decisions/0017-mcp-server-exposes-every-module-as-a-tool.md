---
tags: [mcp, ai, logs, tools, sqlite]
---

## Decision

`pecunia mcp` runs an MCP server over stdio (`github.com/modelcontextprotocol/go-sdk`), giving an AI agent read+write access to all nine modules. One tool per module, each with an `action` parameter mirroring the CLI's own verbs, so the tool surface needed no separate design from what the CLI already has:

- `pecunia_accounts` — list/get/create/update/delete/toggle_freeze; a balance change on update files a balance-adjustment transaction, exactly like `pecunia ac edit` ([[decisions/0015-balance-adjustments-as-a-hidden-kind]])
- `pecunia_credit_cards` — CRUD plus `bills`, `bill_charges`, `pay_bill`
- `pecunia_transactions` — list (every existing filter), get, create (installments, goals, recurring cycles), update/delete (with series scope), transfer
- `pecunia_categories`, `pecunia_goals`, `pecunia_recurring_bills`, `pecunia_budgets`, `pecunia_summary`, `pecunia_logs`

## Why

User: "Lets create a MCP, this way we give an AI agent ways to check the user information and give insights and ask questions." Everything routes through the stores that already exist — [[decisions/0012-summary-composes-existing-stores]]'s call, one level up — so frozen accounts, card limits, currency freezes and write-time bill refreshes all hold for an agent exactly as they do for the CLI. No parallel validation path to keep in sync.

## logs.Actor: a process-wide audit source

[[decisions/0014-logs-as-a-single-audit-table]] reserved `ai` as a `source` value with "nothing writes it yet." That comment is now retired: `logs.Actor` is a package variable defaulting to `user`; `pecunia mcp` sets it to `ai` once at startup. Every store call site that used to hardcode `logs.User` now stamps `logs.Actor` instead — one rename across the `accounts`, `cards`, `budgets`, `goals`, `categories`, `recurring` and `transactions` stores plus `cmd/categories.go`. `pecunia logs --source ai` is now a real filter, not a placeholder.

## Consequence

Tool descriptions tell the agent amounts are minor-unit integers ([[decisions/0001-balance-as-int64-minor-units]]), so it can read balances and due bills without a units mismatch. To use it: `claude mcp add pecunia -- pecunia mcp` (or the equivalent entry in another agent's config).

## Skipped

- **A dedicated `pay` action on recurring bills.** Paying is already a transaction naming `recurring` + `cycle`; the tool description says so instead of adding a second path.
- **Clearing a field to empty via update.** Patch semantics only — a field left out of the call is left alone, not blanked.

Both are one addition away if an agent trips on either.

## Verification

TDD per [[rules/tdd]]: `cmd/mcp_test.go` written red-first, full repo suite green, `gofmt`/`go vet` clean. A live stdio smoke test drove the real JSON-RPC handshake — `initialize`, `tools/list`, a `pecunia_accounts` create over the wire, `pecunia_summary` reflecting the new account — and confirmed the resulting audit row carries `source: ai` via `pecunia logs --source ai`.

Links: [[decisions/0014-logs-as-a-single-audit-table]] · [[decisions/0015-balance-adjustments-as-a-hidden-kind]] · [[decisions/0012-summary-composes-existing-stores]] · [[decisions/0001-balance-as-int64-minor-units]] · [[rules/tdd]] · [[sessions/c1d09b19-d5ad-4ed0-b929-4d689f8bd290]]