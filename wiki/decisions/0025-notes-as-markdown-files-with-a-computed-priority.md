---
tags: [notes, markdown, editor, score, sqlite]
---

## Decision

`pecunia notes` / `pecunia n` is a module for free-form intent — "get a better health care", "renegotiate the Itaú card" — built as **one markdown file per note plus one SQLite row indexing it**. The file is the record: YAML front matter (`title`, `priority`, `status`, `target`, `tags`, `accounts`, `cards`, `goals`) and the body. The row (`014_notes.sql`: `notes`, `note_tags`, `note_links`) holds what a list, a filter or a score needs, plus the counters the file cannot keep (`read_count`, `edit_count`, `last_read_at`) and the file as last indexed (`mtime`, `body_hash`).

User: "This could be saved as a markdown file and use the sqlite just to store metadata … as time passes by we should have a small algorithm that will increase/decrease the priority … To edit and create a note we should use the native text editor (on my case nvim …) … we must already open the file to edit focused on the body." Clarified mid-planning: "The note can have a initial LOW priority but gain priority to something like HIGH over time, the inverse can also happen."

## The score: base priority never rewritten, effective level computed on read

Settled with the user against two alternatives (the algorithm rewriting the priority field; a bare 0–100 number). The owner's `priority: low|medium|high|critical` stays theirs. `notes.Explain` (pure, no DB) scores 0–100 on every read:

| part | rule |
|---|---|
| base | low 20, medium 40, high 60, critical 80 |
| target | overdue +30; ≤7d +25; ≤30d +15; ≤90d +8; ≤365d +3 |
| engagement | `min(15, reads + 2×edits)` |
| activity | `min(20, 2×tx30 + (tx90−tx30)/3)` — distinct transactions on the linked accounts/cards/goals, one `note_links ⋈ transactions` query per list |
| decay | 2 per full week idle past the first 30 days, cap 35; idle = since max(last read, updated, created) |

`done`/`dropped` score 0 and leave the default list. `Level` buckets 0–29/30–54/55–74/75–100 back to the four words, each base inside its own bucket, so an untouched note reads as filed. Display: `LOW → HIGH 74`; a closed note shows its base alone. Lists sort by score desc, then target asc (empty last), then id. `--priority` filters the **effective** level (the list is sorted by it; the base stays visible in the column); `--min-score` is the finer knob. Worked cases pinned in `score_test.go`: LOW→HIGH 74, HIGH→LOW 25, CRITICAL idle bottoms at MEDIUM 45.

## The file format: a hand-rolled front matter subset, canonical on every save

No YAML library. `internal/notes/frontmatter.go` reads exactly what an editor produces — `key: value`, quoted or not, `[a, b]` and block `- a` lists, `#` comments — and refuses the rest with the line number: unknown keys (catches `priorty:`), duplicates, unclosed lists. Pecunia always writes the canonical form (`Render`): all eight keys in order, lists inline, `target: 2026-12-16 # in 3 months`. **A target phrase is resolved once, when saved, and kept beside the day as the comment**, so "in 3 months" never drifts on the next edit. Grammar (`ParseWhen`): ISO, `DD/MM/YYYY`, `YYYY-MM` (last day), today/tomorrow, `next week|month|year`, `in N days|weeks|months|years`; months clamp through `cards.AddMonths`.

## Rows and files kept in step by one store

`notes.NewStore(db, dir)` writes both — `Create` inserts the row (placeholder path, since the name `<id>-<slug>.md` needs the id), writes the file inside the same tx and rolls the row back if the write fails; `Update` rewrites the file at its original name (never renamed on a title change); `Delete` commits the row then removes the file. `ResolveLinks` (codes → ids via `accounts`/`cards`/`goals` stores) **must run before `Begin`** — `db.Open` sets `SetMaxOpenConns(1)`, so a cross-store lookup under an open tx deadlocks. Import graph stays acyclic: nothing imports `notes`; it imports `transactions` for `NormalizeTags`/`ParseTags`/`DateLayout` the way `recurring` does.

Directory: `notes/` beside the database; `PECUNIA_NOTES` overrides; a dev build uses `<repo>/pecunia.dev.notes/` and ignores the env, mirroring `db.Path` ([[decisions/0004-dev-build-isolated-by-ldflags]]).

## External edits: lazy re-index on read, `sync` for the rest

`List`/`Get` stat each file; a changed mtime re-parses it and updates the row (edit counted and logged only when something actually moved — a save that changed nothing but the mtime just updates the mtime). The lazy pass never writes the file. A missing or unparseable file marks the row (`Problem`, `!` in the table, the reason under it) and leaves it. `pecunia n sync` re-parses every row regardless of mtime, writes files back canonical when they differ (that is how a hand-typed phrase becomes a day), adopts orphan `*.md` files as new notes (renamed, the old file removed), and reports missing files without dropping rows.

## The editor flow

`$VISUAL` → `$EDITOR` → `vi`. `editorArgs` splits the value and adds the cursor jump by basename: `+N` for vi/vim/nvim/nano/micro/emacs/kak…, `path:N` for hx/zed, `--wait --goto path:N` for VS Code, `-w path:N` for subl; unknown editors just get the path. `new` writes `RenderDraft` (hint comments beside each key) to `draft-<nanos>.md` in the notes dir, opens the editor on the body line, and creates nothing if the file came back byte-identical; a parse error offers `core.Confirm` to reopen, and declining leaves the draft for `sync` to adopt once fixed. `edit` opens the real file; unchanged counts as a read (`Touch`), changed goes through `Update`. The one untestable line is `OpenEditor`'s `cmd.Run()`; `cmd/notes.go` holds `var openEditor` that `cmd/notes_test.go` swaps for a fake, so the whole create/edit orchestration runs in tests. Flags on `new` (`-p`, `-t`, `--tags`, `--status`, `--account/--card/--goal`, `--no-edit`) are validated **before** the editor opens; `parseInterleaved` lets flags sit before, after or between the title words.

## Also in this change

- `pecunia n ID` renders the body with `charmbracelet/glamour` (user's pick over plain text; new dependency, lipgloss moved to a pseudo-version it needs). `--path ID` prints the file for `nvim $(pecunia n --path 3)` and counts no read.
- MCP `pecunia_notes` (list/get/create/update/delete/sync); `get` does not bump `read_count` — an agent polling is not the owner reading. Omni `/pecunia-notes [level|words]` (manifest now 9 commands, coach still last). `logEntities` gained `note`. Seed: four fixtures showing both directions of the score. Version 0.6.0 → 0.7.0.

## Verification

TDD per [[rules/tdd]] at every layer (schema, model, front matter, grammar, score, store, files/sync, editor, ui, cmd, mcp, omni, seed), `gofmt`/`go vet`/`go test ./...` clean per commit. Live on a reseeded `dev`: create through a stand-in editor script, edit unchanged/changed, every list flag, an external edit picked up lazily, `sync` adopting an orphan and rewriting a phrase, `pecunia l --entity note`, `omni notes`, manifest count, and `PECUNIA_NOTES` ignored by the dev build.

Links: [[decisions/0008-transaction-double-entry-tags-and-filters]] · [[decisions/0014-logs-as-a-single-audit-table]] · [[decisions/0017-mcp-server-exposes-every-module-as-a-tool]] · [[decisions/0023-pecunia-is-an-omni-plugin]] · [[decisions/0004-dev-build-isolated-by-ldflags]] · [[rules/tdd]]
