---
tags: [categories, seed, schema, setup, ux]
---

## Decision

The category module's starter set is **Go data seeded by `pecunia setup`**, not `INSERT` rows in
the migration — and `runSetup` now runs on every invocation instead of bailing out early.

**Where the seed lives.** `internal/categories.Starter` is a `[]Category` beside the store, and
`categories.Seed(*Store) (int, error)` inserts each one whose code is free, reporting how many it
added. `cmd/setup.go` calls it after `db.Open()`. The migration `004_categories.sql` is schema
only.

Rejected: putting the 19 `INSERT`s in the migration. It reaches every database exactly once for
free, including one created by a bare `pecunia ct`, and needs no Go — but the starter set stops
being assertable with `core.ValidateCode` / `core.ColorByName` without parsing SQL, and every
`newTestStore` in the repo would start life with 19 rows it has to `DELETE` before the case can
own its data. Keeping it in Go left the existing test helpers untouched.

**`runSetup` no longer returns early.** It used to print "nothing to do" and return when the
database existed without `--force`, never opening a connection. It now opens and seeds in every
branch. A user whose database predates a module would otherwise have to reach for `--force` —
backing up real data — just to pick up new starter rows. `Seed` skipping taken codes is what
makes that safe: running `setup` repeatedly is a top-up, not a reset.

**Idempotency is by code, not by row.** A starter category the user renamed keeps its edit; one
they deleted stays deleted, because its code is only free again if nothing else claimed it. That
is the intended contract — these are the user's rows from the first run on. There is no restore
command.

**Codes are hand-written, not generated.** `HOME1`, `FOOD1`, `SLRY1` — deterministic, so
`pecunia ct FOOD1` means the same thing on every install, and assertable in tests.
`core.RandomCode`'s reduced alphabet is generation-only, so hand-written codes containing `O`/`1`
would still validate ([[gotchas/account-code-validation-vs-generation-alphabet]]).

**Deferred: a `kind` column.** Income vs expense is not in the spec and
[[tasks/04-transactions-module]] references a category only as a 5-character code — a
transaction's own sign may make `kind` redundant. The upgrade path is already proven here:
`005_category_kind.sql` with a one-line `ALTER TABLE ... ADD COLUMN`, exactly what
`003_credit_card_over_limit.sql` did.

**No money on a category.** No `balance`, no `currency`. What a category is worth is the sum of
what is filed under it, which the transactions module works out. This is what makes the list
table `CATEGORY | DESCRIPTION` rather than the accounts table's `ACCOUNT | BALANCE`.

**No referential guard on delete.** `PRAGMA foreign_keys = ON` is already set in `db.Open()`, so
when the transactions migration adds `REFERENCES categories(id)`, SQLite starts refusing deletes
of in-use categories for free. Nothing was pre-built for it.

## Known hole

`db.Open()` creates the database file on *any* command, so someone whose very first command is
`pecunia ct` gets an empty list rather than the starter set — `setup` is what seeds, and they never
ran it. Handled in the message rather than the architecture: the empty list reads
`no categories yet — run: pecunia setup, or create one with: pecunia ct n`. Move the seed into the
migration if that turns out to bite.

Links: [[tasks/03-category-module]] · [[decisions/0005-internal-core-shared-kernel]] ·
[[gotchas/huh-form-skips-validators-on-eof]] · [[rules/tdd]]
