---
tags: [omni, telegram, plugin, cli, skills]
---

## Decision

Pecunia answers Omni's plugin contract (github.com/johnvilela/omni, PLUGINS.md) itself — no separate plugin repo. `omni plugins install johnvilela/pecunia` works because an Omni plugin is a single binary named after the repo, published as a GitHub release, and pecunia's release.yml already produced the exact asset format (`pecunia_<version>_linux_<arch>.tar.gz` + `checksums.txt`; see [[decisions/0019-pr-only-master-with-ci-and-release-on-merge]]).

User: skills to teach Omni pecunia's philosophy, plus Telegram commands `/pecunia-resume` (with period filters like "last week"), `/pecunia-goals`, `/pecunia-bills`, `/pecunia-cc` that "return the data stored formatted to telegram, wont use LLM at all"; user picked `/pecunia-alerts`, `/pecunia-budget` and `/pecunia-add` as extras, and existing-4-plus-one for skills.

## The contract, as implemented

- `pecunia omni-manifest` → JSON (name/version/description, `mcp: {command: pecunia, args: [mcp]}`, `skills: true`, 7 commands). The MCP declaration wires [[decisions/0017-mcp-server-exposes-every-module-as-a-tool]] into Omni agent sessions with zero new code.
- `pecunia omni-skills <dir>` → writes all five embedded skills as `<name>/SKILL.md`, including the new `pecunia-omni` one.
- Telegram commands map to `pecunia omni <sub>` argv; Omni appends the user's trailing words whitespace-split, sends stdout as plain text (4096-char chunks, 60s timeout), shows the last stderr line on non-zero exit, and renders empty stdout as "✓ (no output)".

## Shape: one file, `pecunia omni <sub>`, not --plain flags

Everything lives in `cmd/omni.go` (+ test), three new dispatch cases in `cmd/main.go` ([[decisions/0002-flat-cmd-package-layout]]), kept out of the help text — machine faces. Manifest argv is free-form, so `["pecunia","omni","resume"]` beats retrofitting `--plain` onto five existing flag parsers. The lipgloss renderers in `internal/*/ui.go` are unusable here (Telegram is a proportional font: no ANSI, no tables, no alignment), so each subcommand has a plain line-based renderer beside a tiny `money()` helper — `core.MoneyLine` was almost right but styles its separator.

## Choices that will matter later

- **Alerts are one function, two callers**: `alertLines` (overdue recurring bills, budgets past cap, cards ≥90% of limit — fixed threshold, `ponytail:` comment) feeds both the resume's Alerts section and `omni alerts`, whose contract is *silence when all is well* so an Omni scheduled task only pings on trouble. Alerts are always judged against `time.Now()` even when the resume window is last month.
- **Period phrases** (`parsePeriod`): today, yesterday, week/this week, last week (weeks Monday–Sunday), month/this month, last month, YYYY-MM, YYYY-MM-DD; empty = current month. Maps onto `summary.Period` — `summary.Collect` already took arbitrary ranges. `periodLabel` spells a week window as its two ends because `periodTitle` only knows days and months.
- **`omni add` never guesses where money goes**: `AMOUNT TITLE... [@ACCT] [#CAT]`, and with several unfrozen accounts and no `@CODE` it errors listing the codes. Writes stay `logs.User` — a human typing in Telegram is the user, not the AI (unlike MCP writes, [[decisions/0017-mcp-server-exposes-every-module-as-a-tool]]). `core.ParseAmount` already accepts the comma decimal separator.
- **`pecunia-omni` is filtered out of `setup --skills`** (one `continue` in `installSkills`) — it teaches the Telegram commands, which only exist under Omni; `omni-skills` ships it. It repeats the shared hard-rules block verbatim and satisfies the pinned checks in `cmd/skills_test.go` ([[decisions/0022-setup-skills-installs-ai-agent-finance-skills]]).

Version bumped 0.4.0 → 0.5.0 in its own final commit; merging cuts the release Omni installs from.

## Verification

`go test ./...`, `gofmt`, `go vet` clean. Manual against the dev DB: all seven subcommands render Telegram-shaped output (checked resume with and without "last week", the add error paths, and `omni-skills` writing five dirs); `omni-manifest` prints valid JSON.

Built on branch `feat/omni-plugin`, off `origin/master`; not yet opened as a PR by the end of the session it was built in. Full build narrative, including the exploration/plan agents and the live verification steps: [[sessions/e22cf9b6-6521-4261-b791-0815460c124e]].

Links: [[decisions/0017-mcp-server-exposes-every-module-as-a-tool]] · [[decisions/0019-pr-only-master-with-ci-and-release-on-merge]] · [[decisions/0022-setup-skills-installs-ai-agent-finance-skills]] · [[decisions/0002-flat-cmd-package-layout]] · [[sessions/e22cf9b6-6521-4261-b791-0815460c124e]]