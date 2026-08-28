---
tags: [upgrade, cli, release, migrations, http]
---

## Decision

User: "lets create a command called 'kakei upgrade'. It will check if there is a new version, if so it will show the changelog and ask the user if he wants to update the CLI. After updating, it will run the migrations to update the database. There should be a flag '-y' run without asking anything, just showing the changelog."

`cmd/upgrade.go`, first HTTP client and first `httptest` use in the repo — stdlib only, no new deps (per [[rules/git-hooks]]'s "as little dep as possible").

## How it works

1. GET `api.github.com/repos/johnvilela/kakei/releases` (public repo, unauthenticated; 5-minute-timeout client covers the download too).
2. `releasesSince` filters drafts/prereleases and keeps releases semver-greater than the compiled-in `version` ([[decisions/0019-pr-only-master-with-ci-and-release-on-merge]]), newest first. Real numeric compare (`versionLess`), not string `!=` — a dev build ahead of the latest release reads as up to date, never as a downgrade offer.
3. **Changelog = every release body between current and latest** (user's choice over latest-only), printed before any prompt — `-y` skips only the confirmation, never the changelog.
4. Confirm via `core.Confirm` with the new affirmative-label parameter ("Yes, upgrade"). No TTY and no `-y` → huh returns false → quiet no-op, same contract as setup's MCP offer.
5. Asset `kakei_<ver>_<GOOS>_<GOARCH>.tar.gz` → temp file **next to the target binary** (same filesystem) → `untarBinary` (gzip+tar, chmod 0755) → `os.Rename` over the target. Rename-over is mandatory: writing into a running binary is ETXTBSY on Linux. Target is `os.Executable` + `filepath.EvalSymlinks`, so a symlinked install replaces the real file and registered MCP paths ([[decisions/0018-mcp-install-writes-agent-config-files]]) stay valid. Permission errors carry a "try again with sudo" hint.
6. Post-swap: exec `<new binary> migrate`. Migrations live embedded in the **new** binary — the old process can't apply them.

## The `migrate` command

Three lines: `withConn` → `db.Open()` already applies pending migrations on every open ([[decisions/0001-sqlite-file-db]] territory; see `internal/db/db.go` `migrate`), so the command just opens and prints. It exists so upgrade has something deterministic to exec.

## Migrate exec failure = defer, not fail

Live-tested the bootstrap edge: a fake 0.0.1 build upgraded against the real v0.1.0 release — swap fine, but v0.1.0 predates `migrate`, exit 2. Since `db.Open()` migrates on the next run anyway, a failed exec prints "they will apply on the next kakei run" and the upgrade still exits 0. Can't happen in the wild (any upgrade-capable binary only moves to migrate-capable ones), but the graceful path is tested (`TestUpgrade/a_failed_migrate_exec_defers...`).

## Confirm grew an affirmative-label parameter

`core.Confirm(title, description, affirmative)` — it hardcoded "Yes, delete", which the setup MCP-hook prompt had been showing for an install question. 7 delete sites pass "Yes, delete", setup now "Yes, install", upgrade "Yes, upgrade".

## Tests

TDD per [[rules/tdd]]. `cmd/upgrade_test.go`: tables for `versionLess`/`releasesSince`, `untarBinary` round-trip (mode 0755 asserted, missing-entry error), and `httptest` end-to-end — the served tarball's `kakei` entry is a shell script that writes a marker when exec'd with `migrate`, proving the whole chain: changelog printed for all pending versions, binary replaced, migrations exec'd, no temp litter. Seams: package vars `releasesURL` and `selfExe`.

Also live end-to-end against the real GitHub API twice (see above): changelog, download, swap, `version` prints the released 0.1.0 afterwards.

## Version bumped to 0.2.0

In this PR, so merging releases the feature as v0.2.0.

## Skipped

- Checksums/signature verification of the downloaded tarball — the release pipeline doesn't produce them yet (deferred in [[decisions/0019-pr-only-master-with-ci-and-release-on-merge]]); add both sides together.
- Windows (no assets exist) — the missing-asset error names the platform.

Links: [[decisions/0019-pr-only-master-with-ci-and-release-on-merge]] · [[decisions/0018-mcp-install-writes-agent-config-files]] · [[rules/tdd]] · [[rules/git-hooks]]