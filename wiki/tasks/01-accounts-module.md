---
tags: [accounts, module, tui, sqlite, tdd]
---

# Accounts Module

This module will be responsible for managing user's accounts. It will have commands for create, list, details, edit, delete, freeze. All command will have shortcuts.

## Commands

list: "kakei accounts" or "kakei ac"
create: "kakei accounts new" or "kakei ac n"
edit: "kakei accounts edit {CODE|ID?}" or "kakei ac e {CODE|ID?}"
delete: "kakei accounts delete {CODE|ID?}" or "kakei ac d {CODE|ID?}"
freeze: "kakei accounts freeze {CODE|ID?}" or "kakei ac f {CODE|ID?}"
details: "kakei accounts {CODE|ID?}" or "kakei ac {CODE|ID?}"

## Suggested structure

name: string
description?: string
code: string (length 5)
color: string
balance: int
currencyType: string
isFrozen: bool

also basic columns

## Rules
- The color must be a pre-set of 12 colors to easily identify the accounts
- code is required and must have a random suggestion for the user
- all commands that uses CODE|ID will shows an account select if no code is provided
- it must use the bubbles package to create a great UX
- there will be 4 options as currencyType: Dollar, Euro, Brazilian Real, Bitcoin
- if the commands is used if the flag "-h" or "--help" it will show a small documentation of that command only

## Status: Implemented

Built and verified in the `accounts-module-schema` session. Key implementation decisions beyond the original spec:

- Schema delivered as numbered migration files (`internal/db/migrations/001_accounts.sql`), embedded via `//go:embed` and applied on every `db.Open()` — tracked in a `schema_migrations` table, idempotent, so existing DBs self-heal.
- Balance encoding: see [[decisions/0001-balance-as-int64-minor-units]] — a single INTEGER column in minor units, exponent per currency, no floats.
- Delete: hard delete with a confirm prompt, not soft delete.
- Freeze: toggles on repeated calls, no separate freeze/unfreeze subcommands.
- Create uses a `huh` form; edit/delete/freeze account selection uses a `bubbles/list` picker (bubbletea program, alt screen) — this split was an explicit user choice when asked huh-vs-raw-bubbles.
- 12-color palette and the original 5-char code generation alphabet excluded visually ambiguous characters (`O`/`0`, `I`/`1`) — this restriction is now generation-only, see the code-validation update below.
- Code uniqueness enforced both at the DB (`UNIQUE` constraint) and in the form validator.

Files: `internal/db/db.go`, `internal/db/migrations/001_accounts.sql`, `internal/accounts/account.go`, `internal/accounts/store.go`, `internal/accounts/ui.go`, `cmd/accounts.go`.

Deps added: `github.com/charmbracelet/huh`, `bubbles`, `bubbletea`, `lipgloss`, `modernc.org/sqlite`.

## Update (session kakei-19): test suite, bugfixes, UI redesign, dev tooling, commit

Full session narrative: [[sessions/2d27f8ef-e996-46a1-80f7-d9457f69527b]].

- **Test suite** written per [[rules/tdd]]: `store_test.go`, `account_test.go`, `ui_test.go`, `cmd/accounts_test.go`, all subtests with isolated per-case SQLite files. Final count: 158 subtests green. Coverage: accounts 67%, cmd 58% (remainder is TTY-bound code: `Form`, `Confirm`, `Pick`, untestable headless, noted in-file).
- **Real bug caught by the tests**: `Store.Update` on a missing id silently returned `nil` instead of an error — `kakei ac e` on a deleted account printed "updated". Fixed to return `ErrNotFound`, same as `Delete`.
- **Code validation bug fixed**: `ValidateCode` used to reuse the reduced generation alphabet and rejected a valid user-typed code ("INTER"). See [[gotchas/account-code-validation-vs-generation-alphabet]].
- **List table redesign** (screenshot-driven feedback): two columns only — `[CODE] Name` (code colored, `❄` inline if frozen) and balance (green if positive, red if negative, uncolored at zero). Currency column removed (symbol already shown in the balance string).
- **Frozen accounts** hidden from the default list, shown only with `--all`/`-a`, sorted last (`ORDER BY is_frozen, name` in `Store.List`) and rendered dimmed. Filtering itself lives in `cmd`'s `listAccounts(showAll)`, not the store, since `kakei ac f` must still see frozen accounts to unfreeze them. Footer messages report how many are hidden, or that everything is frozen.
- **Detail view redesign**: bordered card colored in the account's color, no field labels. Order: code (bold, `❄` if frozen) → name → description (dropped if empty) → balance (green/red) with currency code → `created`/`updated` dates with icons (`✚` created, `#` updated — simplified from an earlier icon per user request). Status line and numeric ID removed from the card. Color-swatch row dropped as redundant with the border.
- **Dev tooling**: `scripts/dev.sh` + `scripts/seed/main.go`, binary named `dev`, DB isolation via ldflags. Full decision: [[decisions/0004-dev-build-isolated-by-ldflags]].
- **Committed**: `/git-commit` → `b6da3ff` — "feat: add accounts cli with sqlite storage, tui and dev tooling", 32 files. This is the first commit carrying the accounts module; everything since the bootstrap commit had been sitting uncommitted.

Verified end-to-end against the seeded dev DB throughout: list table, details card, freeze/--all behavior, piped output all correct. `go vet`, `go test`, `gofmt` clean at every step.

Links: [[decisions/0001-balance-as-int64-minor-units]] · [[decisions/0004-dev-build-isolated-by-ldflags]] · [[rules/tdd]] · [[gotchas/account-code-validation-vs-generation-alphabet]]
