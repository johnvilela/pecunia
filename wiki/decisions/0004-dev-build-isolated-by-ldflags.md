---
tags: [go, build, sqlite, dev-tooling]
---

## Decision

`scripts/dev.sh` builds a `dev` binary (renamed from `pecunia-dev` per later user request) that reads/writes a seeded root-level SQLite database (`pecunia.dev.db`), fully isolated from the real one. Isolation happens at **link time**, not runtime:

```
-ldflags "-X pecunia/internal/db.DevDB=$ROOT/pecunia.dev.db"
```

`db.Path()` returns `DevDB` before it looks at `PECUNIA_DB` or the default config path, so a `dev`-built binary cannot open the real database even if `PECUNIA_DB` is set in the environment. `DevDB` is an empty string in every other build — a test asserts this, so nothing can quietly set it.

## Why

User: "create a script that will build the cli with a DEV mode. It will look for database on the root of the project with seeded data. This database should be added on @.gitignore." Later: "the dev cli should be called 'dev' and not 'pecunia-dev'."

## Details

- `scripts/seed/main.go` — 8 fixtures (BRL/USD/EUR/BTC, one frozen, one negative, one without a description), built with the same ldflags so the seeder can't wander off either. Inserts only missing codes, so hand-edits to the dev DB survive a rebuild.
- `scripts/dev.sh` builds unstripped, no `-trimpath` (traces stay readable), then seeds. `--reseed` deletes the DB first. Idempotent — a second run seeded 0 of 8.
- `.gitignore` covers the `dev` binary and `/pecunia.dev.db*` (the glob catches the `-wal`/`-shm` sidecar files). The database filename itself (`pecunia.dev.db`) was not renamed, only the binary was.
- The `INTER` code is in the seed data on purpose — it's the code that the validation bug (see [[gotchas/account-code-validation-vs-generation-alphabet]]) used to reject, now a regression fixture visible on every list.
- Verified live: ran `dev` with `PECUNIA_DB=/tmp/should-be-ignored.db` set, got dev data back, and that file was never created.

Links: [[decisions/0003-static-stripped-build-script]] · [[tasks/01-accounts-module]]