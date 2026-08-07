---
tags: [credit-card, module, tui, sqlite, tdd, money]
---

# Credit Card Module

This module will be responsible for managing user's credit cards. It will have commands for create, list, details, edit, delete. All command will have shortcuts.

## Commands

list: "kakei credit-card" or "kakei cc"
create: "kakei credit-card new" or "kakei cc n"
edit: "kakei credit-card edit {CODE|ID?}" or "kakei cc e {CODE|ID?}"
delete: "kakei credit-card delete {CODE|ID?}" or "kakei cc d {CODE|ID?}"
details: "kakei credit-card {CODE|ID?}" or "kakei cc {CODE|ID?}"

## Suggested structure

name: string
description?: string
code: string (length 5)
color: string
limit: int
balance: int
currencyType: string
closingDate: datetime
dueDate: datetime

also basic columns

## Rules
- The color must be a pre-set of 12 colors to easily identify the accounts
- code is required and must have a random suggestion for the user
- all commands that uses CODE|ID will shows an account select if no code is provided
- it must use the bubbles and lipgloss package to create a great UX
- there will be 4 options as currencyType: Dollar, Euro, Brazilian Real, Bitcoin
- if the commands is used if the flag "-h" or "--help" it will show a small documentation of that command only

## Notes

Some rules will be add later when transactions module is created.

## Status: Implemented

Built and verified across the `credit-card-module` session ([[sessions/ce07d7cb-4a82-4381-89cb-9ad513a7159d]]). Key implementation decisions beyond the original spec:

- **Shared kernel**: currency, palette, code, money and picker helpers were extracted out of `internal/accounts` into `internal/core` first, since the card module needed most of that surface. Full write-up: [[decisions/0005-internal-core-shared-kernel]].
- **Money and schedule model**: `closing_day`/`due_day` are day-of-month integers (1-31), not absolute dates; `balance` is the amount used on the open invoice, `Available()` is computed; column is `credit_limit` (`LIMIT` is a SQLite keyword); no freeze concept; codes unique per table, not shared with accounts; no FK to an account (deferred to the transactions module). Full write-up, including the mid-session UI correction and the over-limit-allowance flag: [[decisions/0006-credit-card-money-schedule-and-over-limit-model]].
- **`over_limit_allowed`** (default false) lets a specific card carry a balance past its limit; enforced in the store layer via `Card.ValidateBalance()`, not just the UI.
- **A real bug caught along the way**: a `huh` form returns without running its validators when stdin hits EOF under a pty, letting a blank-named row through — fixed in both the `cards` and `accounts` stores. See [[gotchas/huh-form-skips-validators-on-eof]].
- Test suite built per [[rules/tdd]]: schema, model, store, UI and command layers each red-then-green, plus seed fixtures covering every render branch (over limit, zero balance, no description, all four currencies, day edges, both settings of the over-limit flag).

Files: `internal/core/{core,ui}.go`, `internal/cards/{card,store,ui}.go`, `cmd/cards.go`, `internal/db/migrations/002_credit_cards.sql`, `internal/db/migrations/003_credit_card_over_limit.sql`.

Commits: `refactor: extract shared currency, palette and picker helpers into internal/core` → `feat: add credit card cli with sqlite storage and tui` → `fix: reject a blank name in the account and card stores` → `fix(cards): show used against limit instead of an ambiguous green available` → `feat(cards): add an over-limit allowance that caps the balance when off`.

Links: [[decisions/0005-internal-core-shared-kernel]] · [[decisions/0006-credit-card-money-schedule-and-over-limit-model]] · [[gotchas/huh-form-skips-validators-on-eof]] · [[rules/tdd]]