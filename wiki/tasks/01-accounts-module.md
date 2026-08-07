---
tags: [accounts, cli, tui, sqlite]
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
- List rendering: a static styled table via `lipgloss/table` — stays pipeable (`kakei ac | cat` works), no alt-screen.
- Freeze: toggles on repeated calls, no separate freeze/unfreeze subcommands.
- Create uses a `huh` form; edit/delete/freeze account selection uses a `bubbles/list` picker (bubbletea program, alt screen) — this split was an explicit user choice when asked huh-vs-raw-bubbles.
- 12-color palette and the 5-char code alphabet exclude visually ambiguous characters (`O`/`0`, `I`/`1`).
- Code uniqueness enforced both at the DB (`UNIQUE` constraint) and in the form validator.

Files: `internal/db/db.go`, `internal/db/migrations/001_accounts.sql`, `internal/accounts/account.go`, `internal/accounts/store.go`, `internal/accounts/ui.go`, `cmd/accounts.go`.

Deps added: `github.com/charmbracelet/huh`, `bubbles`, `bubbletea`, `lipgloss`, `modernc.org/sqlite`.

Verified end-to-end against a seeded DB: list table, details by code and by id, freeze toggle (case-insensitive code), per-command `-h`, unknown ref → exit 1, piped output all correct. `go vet`, `go test`, and `gofmt` all clean.

**Not verified headless**: the create form, the edit/delete/freeze picker, and the delete confirm — they need a real TTY. Under no TTY they fail cleanly (`could not open a new TTY`, exit 1) rather than panicking; still need a terminal run to confirm the actual UX.

No commit was made for this module by the end of the session.