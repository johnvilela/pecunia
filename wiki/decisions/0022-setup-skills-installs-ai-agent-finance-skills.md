---
tags: [mcp, ai, skills, setup, cli]
---

## Decision

`pecunia setup --skills` installs four markdown "skill" files — instructions teaching an AI agent how to use pecunia's MCP tools ([[decisions/0017-mcp-server-exposes-every-module-as-a-tool]]) well — into every supported agent's skill-discovery path. User: "together with the init/setup command, lets add a flag (—skills) to install some AI skills to help manage finances, i think 4 skills are necessary: one to review the data and give a financial overview with some tips and alerts, one to help create budget based on data stored, one to help import data from pdf/json/csv files (organizing everything), one to help identify money leaks and improve financial health."

## Two fixed roots cover all four agents

Researched live (August 2026 state) rather than assumed: all four agents pecunia already targets for MCP install ([[decisions/0018-mcp-install-writes-agent-config-files]]) now support the Agent Skills standard (`<name>/SKILL.md`, YAML frontmatter `name` + `description`):

| Agent | Skill path |
|---|---|
| claude-code | `~/.claude/skills/<name>/SKILL.md` only — does not read `~/.agents/skills/` |
| codex | `$HOME/.agents/skills/<name>/SKILL.md` (its older `~/.codex/prompts/*.md` is deprecated) |
| gemini | `~/.gemini/skills/`, or the `~/.agents/skills/` alias — no TOML wrapping needed |
| opencode | `~/.config/opencode/skills/`, plus `~/.claude/skills/` and `~/.agents/skills/` |

Writing each skill to exactly two roots — `~/.agents/skills/` (codex, gemini, opencode) and `~/.claude/skills/` (claude-code; opencode reads it too) — covers the whole roster. This collapsed the design: no agent picker, no per-agent format transform, no degrade path. One file format, two fixed destinations, plain byte copy.

## Implementation

- `cmd/skills/*.md` — four complete SKILL.md files (frontmatter included), embedded via `//go:embed skills/*.md` in `cmd/skills.go`, the same embed pattern `internal/db/db.go` uses for migrations. Named `pecunia-overview`, `pecunia-budget`, `pecunia-import`, `pecunia-health` — the `pecunia-` prefix avoids clobbering a user's own generic `budget`/`import` skill and satisfies opencode's naming regex.
- `skillDests(home, name string) []string` returns the two destination paths — a pure function, same shape as `mcp_install.go`'s `agentArgv`. `installSkills()` reads every embedded file and writes it to both roots.
- **Plain overwrite, no merge, no backup.** The files are pecunia-owned instruction files; overwriting keeps re-runs idempotent and lets an upgrade refresh their content.
- `--skills` on `pecunia setup` installs without prompting, mirroring `pecunia mcp install AGENT`'s explicit-flag behavior. `runSetup` signature changed to `runSetup(force, skills bool)`.
- **Plain interactive `setup` now offers skills right after the existing MCP offer**, chained as run-and-continue instead of the MCP offer's old early `return`. Backing out of the MCP agent picker (`core.ErrCancelled`) no longer aborts setup — it falls through to the skills offer instead. Both offers still drop `core.Confirm`'s error, so a no-TTY run (scripts, `curl | sh`, tests) silently declines both, unchanged from [[decisions/0018-mcp-install-writes-agent-config-files]]'s contract.
- Version bumped 0.3.0 → 0.4.0 in the same PR, so merging it cuts the release per [[decisions/0019-pr-only-master-with-ci-and-release-on-merge]].

## The shared hard-rules block

Every skill repeats the same rules verbatim near its top, since skills load standalone with no cross-references between them:

- amounts are integers in minor units — cents; satoshis for BTC. Format for display, never show raw integers.
- never sum or compare amounts across currencies — pecunia has no exchange rates. Report per currency, always.
- read before advising; confirm with the user before every write (creates, updates, deletes) — reads need no confirmation.
- respond in the user's language, whatever it is.
- every write is audited as `source: ai` — the user can see it with `pecunia_logs`.

## The four skills

- **pecunia-overview** — read-only. Reads `pecunia_summary`, `pecunia_accounts`, `pecunia_credit_cards` (+ bills), `pecunia_budgets`, `pecunia_goals`, `pecunia_recurring_bills`, and the current month's `pecunia_transactions`. Reports balances per currency, in vs out, due/overdue bills and card statements, budgets vs actual, goal progress; flags overdue bills, over-cap budgets, negative/shrinking balances, spending spikes; tips grounded in the actual data. Makes no writes.
- **pecunia-budget** — reads 2-3 months of transactions grouped by category plus existing budgets, proposes a per-category cap from typical spend (with breathing room, per currency), shows the full plan and gets explicit approval before writing any `pecunia_budgets` create/update — never overwrites an existing cap without asking.
- **pecunia-import** — parses a PDF/CSV/JSON bank export, converts amounts to minor units, maps rows to existing accounts/cards (asking when ambiguous) and categories, checks the date range for duplicates before writing, treats a movement between the user's own accounts as one `transfer` rather than two entries, handles installment purchases, shows a preview table and gets approval before writing.
- **pecunia-health** — reads 3+ months of transactions, recurring bills, card bills and budgets; hunts money leaks (untracked recurring charges, categories growing month over month, frequent small charges, card fees/interest, forgotten-subscription patterns); quantifies and ranks each by monthly cost per currency; proposes fixes (cancel, cap with a budget, register as a recurring bill, set a savings goal) — writes only with explicit approval.

## Tests

`cmd/skills_test.go`, pure style matching `mcp_install_test.go`: `TestSkillDests` pins the two destination paths; `TestSkillFiles` walks the embedded files and asserts frontmatter `name` matches the filename and the naming regex, `description` is non-empty and within the 1024-char limit, and the body mentions at least one `pecunia_` tool and the phrase "minor units" — a guard against the money rule silently drifting out of a skill on a future edit.

## Verification

`go test ./...`, `gofmt`, `go vet` clean (CI depended on [[tasks/07-recurring-bills-module]]'s seed-tiling fix merging first, since both touch `scripts/seed`). Manual: `HOME=$(mktemp -d) pecunia setup --skills` wrote 4+4 `SKILL.md` files; a second run overwrote cleanly; a no-TTY plain `setup` declined both offers silently.

## Status at session end

Shipped as two commits on branch `feat/setup-skills`, opened as **PR #6**. [[tasks/07-recurring-bills-module]]'s seed-tiling fix (PR #5) was a CI prerequisite — merged first, then `feat/setup-skills` was rebased onto master and came back green. Left open for the user's own review, not merged — merging it will cut release v0.4.0.

Links: [[decisions/0017-mcp-server-exposes-every-module-as-a-tool]] · [[decisions/0018-mcp-install-writes-agent-config-files]] · [[decisions/0019-pr-only-master-with-ci-and-release-on-merge]] · [[tasks/07-recurring-bills-module]] · [[rules/tdd]] · [[rules/commit-messages]] · [[sessions/85b098e4-8278-4b05-8279-fbda23de2fcd]]