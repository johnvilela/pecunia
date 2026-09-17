---
tags: [backup, s3, age, systemd, toml, encryption]
---

## Decision

`pecunia backup` copies the database and the notes to somewhere else, on a schedule. One package, `internal/backup`, and one command file, `cmd/backup.go`. Settled with the user in one Q&A round before any code:

- **Scheduler: a systemd user timer that pecunia installs**, not a daemon and not Omni. `pecunia backup run` is a one-shot; `pecunia backup schedule 2/day` writes `~/.config/systemd/user/pecunia-backup.{service,timer}` and runs `systemctl --user enable --now`. Without `systemctl` on PATH the command prints the crontab line instead and still saves the schedule.
- **Native Go clients, S3 first.** This PR ships `local` (a directory) and `s3`; Dropbox and Google Drive come as one file each behind the same `Provider` interface (`Put(name, srcPath)`, `Get`, `List`, `Delete`), in their own PRs, because each needs an OAuth app the user has to create.
- **`backup.toml` beside the database, 0600**, secrets included — the timer runs unattended, nobody is there to type them. `PECUNIA_BACKUP_PASSPHRASE`, `PECUNIA_BACKUP_S3_ACCESS_KEY`, `PECUNIA_BACKUP_S3_SECRET_KEY` override the file. A dev build uses `pecunia.dev.backup.toml` in the repo, mirroring `db.Path` ([[decisions/0004-dev-build-isolated-by-ldflags]]).
- **Schedule grammar is `N/day` or `N/week`** (`daily`, `weekly` accepted), the user's own wording. `N/day` spreads N runs from 03:00 (`2/day` = 03:00 and 15:00); `N/week` spreads N days from Monday at 03:00 (`3/week` = Mon, Wed, Fri). `Every.OnCalendar()` and `Every.Cron()` render both forms; the tests pin every case.
- **Encryption optional, off by default**, with [age](https://age-encryption.org) (`filippo.io/age`, scrypt recipient) when a passphrase is set — so the `age` CLI can open an archive without pecunia. Retention (`keep N`), a `restore` command and the `local` provider are all in the first PR; the user asked for all four.

## The archive

`pecunia-<UTC stamp>.tar.gz`, or `.tar.gz.age`: `pecunia.db` at the root and `notes/**` beside it, nothing else — never `backup.toml`, whose credentials would then travel to the very bucket they open. The database goes in as a **`VACUUM INTO` snapshot**, never a file copy: with WAL on ([[decisions/0013-data-integrity-fixes-and-known-gaps]]) the newest rows sit in `pecunia.db-wal`, and a copy of the main file alone would silently miss them; `VACUUM INTO` reads through the WAL under a read transaction. `archive_test.go` proves it with a row still in the WAL. `Extract` refuses any entry that is not `pecunia.db` or under `notes/`, and any `..`.

## S3 by hand

`s3.go` is the four requests the provider needs (PUT, GET, DELETE, ListObjectsV2 with continuation) and a SigV4 signer, ~300 lines, no SDK — the AWS SDK v2 is dozens of modules for what fits in one file, and the hand-rolled form works unchanged against MinIO, R2 and B2 when `endpoint` is set (path-style URLs; virtual-hosted on AWS itself). `TestSigV4` pins the signer to the two worked examples in the S3 docs, keys and all. **Gotcha met while writing it**: the AWS docs page is a JS shell that redirects to Welcome.html when curled; the published signature vectors were confirmed from five independent open-source signers on GitHub (`gh api search/code`) rather than from the page.

## Restore

`Restore` does every step that can fail before it touches anything live: download, decrypt, unpack into a temp dir *beside* the database (same filesystem, so the final move is a rename), `PRAGMA integrity_check` on the copy. Then `pecunia.db`, `-wal` and `-shm` move together to `pecunia.db.<stamp>.bak[-wal|-shm]` — the WAL must travel with its file or SQLite would replay the old one into the restored database — and the notes directory to `notes.<stamp>.bak`. Nothing is deleted. A failed restore leaves the live data untouched; the tests check that with a wrong passphrase and with a garbage archive.

## Tests

`internal/backup` is TDD throughout ([[rules/tdd]]): a `providerSuite` run against both providers, a fake S3 in `httptest` that checks the content hash and paginates, real SQLite files for every archive case. `backup.Systemctl` and `backup.HaveSystemd` are the seams the command tests swap so no test installs a real timer.

## Status: merged as PR #12 (2026-09-17), v0.8.0

Branch `feat/backup`, PR #12 "feat(backup): back the database and notes up to a directory or S3", CI green, approved and merged by the user. Dropbox follows in its own PR on `feat/backup-dropbox`; Google Drive after that.

Links: [[decisions/0025-notes-as-markdown-files-with-a-computed-priority]] (what the notes directory is) · [[concepts/remote-access-to-canonical-sqlite]] (backup is not sync; the canonical copy stays where it is) · [[sessions/ef2f43c3-8c61-4064-b047-63b0197a9abc]]