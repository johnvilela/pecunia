---
tags: [categories, module, tui, sqlite, tdd, seed]
---

# Category modules

This module will be responsible for managing user's categories. It will have commands for create, list, details, edit, delete. All command will have shortcuts. This will start seeded with some categories that the user can edit later.

## Commands

list: "kakei category" or "kakei ct"
create: "kakei category new" or "kakei ct n"
edit: "kakei category edit {CODE|ID?}" or "kakei ct e {CODE|ID?}"
delete: "kakei category delete {CODE|ID?}" or "kakei ct d {CODE|ID?}"
details: "kakei category {CODE|ID?}" or "kakei ct {CODE|ID?}"

## Suggested structure

id: int (incremental)
name: string
description?: string
code: string
updatedAt, createdAt

## Rules
- The color must be a pre-set of 12 colors to easily identify the accounts
- code is required and must have a random suggestion for the user
- all commands that uses CODE|ID will shows an account select if no code is provided
- it must use the bubbles and lipgloss package to create a great UX
- if the commands is used if the flag "-h" or "--help" it will show a small documentation of that command only

## Seed
When the user creates a new database it will be seeded with some categories, below is a list of suggestions but feel free to improve this:

- Home
- Utilities
- Food & Groceries
- Transport
- Health & Medical
- Restaurants
- Entertainment
- Personal Care
- Gifts
- Educational
- Love
- Family
- Pets
- Hobbies
- Work
- Salary
- Investment
- Debts & Loan
- Leisure

## Notes

Some rules will be add later when transactions module is created.

## Status: Implemented

Built and verified in the `category-module` session
([[sessions/2a7339bb-af86-47f1-a0fb-8fbc097dd9ea]]). Key implementation decisions beyond the
original spec:

- **The starter set is Go data seeded by `kakei setup`**, and `runSetup` no longer returns early
  on an existing database — it tops up instead, so a database that predates a module never needs
  `--force`. Codes are hand-written (`HOME1`, `FOOD1`, `SLRY1`) rather than generated. Full
  write-up, including the deferred `kind` column and the known hole: [[decisions/0007-category-starter-set-seeded-from-go]].
- **Colour is required**, though the spec's *Suggested structure* omits it — the Rules ask for the
  12-colour preset, and the colour is what makes `[HOME1]` readable at a glance in a 19-row list.
  Code length is 5, from `core.CodeLen`, matching accounts and cards.
- **No money on a category**, so the list table is `CATEGORY | DESCRIPTION` (description dimmed)
  rather than the accounts table's `ACCOUNT | BALANCE`, and the details card is the accounts card
  with the balance block removed.
- **`SuggestCode` moved into `internal/core`** as `core.SuggestCode(taken func(string) (bool, error))`.
  Categories was the third caller, which was the ceiling the `ponytail:` comment in
  `internal/cards/store.go` had named — a net deletion of two copies. The `huh` code-uniqueness
  validator closure is now the same shape at three copies and carries its own `ponytail:` note in
  `internal/categories/ui.go`; lift it if a fourth module arrives.
- **`core.ValidateName` guards `Create` and `Update`** from the first commit, so the
  [[gotchas/huh-form-skips-validators-on-eof]] class of bug was never reachable here. Pinned by
  `TestNameIsRequired` without needing a TTY.
- Test suite built per [[rules/tdd]]: schema, model, store, seed, UI and command layers each
  red-then-green, own SQLite file per subtest, substring assertions for anything lipgloss renders.

Files: `internal/db/migrations/004_categories.sql`, `internal/categories/{category,store,ui}.go`,
`cmd/categories.go`, `cmd/main.go`, `cmd/setup.go`, `internal/core/core.go`,
`internal/accounts/store.go`, `internal/cards/store.go`, `scripts/seed/main.go`.

Commits: `refactor(core): move the free-code search into core` → `feat: add category cli with
sqlite storage and tui` → `feat(setup): seed starter categories into a new database`.

Links: [[decisions/0007-category-starter-set-seeded-from-go]] · [[decisions/0005-internal-core-shared-kernel]] · [[gotchas/huh-form-skips-validators-on-eof]] · [[rules/tdd]] · [[sessions/2a7339bb-af86-47f1-a0fb-8fbc097dd9ea]]