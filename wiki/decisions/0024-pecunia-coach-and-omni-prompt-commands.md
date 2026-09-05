---
tags: [omni, mcp, ai, coach, plugin-commands]
---

## Decision

Two-repo feature built in one session: pecunia gained `/pecunia-coach`, a financial-coaching command that runs as a real LLM agent turn inside Omni — not a plain exec. That required extending Omni's plugin contract first: Omni plugin commands could previously only exec a binary and relay its stdout, with no model call anywhere in the path.

## Why

User: `/pecunia-coach` should use the LLM Omni is configured with, read the day's situation (bills due/overdue, card spend, account balances, goals), ask 3-5 questions, generate a plan stored via Omni's own plan feature, update that plan on re-run instead of creating a second one, offer a twice-daily check-in routine, and support `--forget` to wipe the plan and disable the routine.

Two Explore agents ran concurrently, one per repo, before any code:

- **Pecunia side**: the whole Omni face is `cmd/omni.go` (manifest + Telegram dispatch) and `cmd/mcp.go`'s nine tools. Omni's own `pluginReply` execs the manifest's `argv` and relays stdout with a 60s cap — no LLM involved in a plugin command at all. Every ready-made data source a coach could reuse was mapped: `internal/summary.Collect`, `collectAlerts`/`alertLines`, the `plainX` renderers in `cmd/omni.go`, and the domain math already living in each module (`recurring.Bill.Current`, `budgets.Budget.Pace/Drift/Status`, `goals.Goal.Progress`). One real gap found: no spend-by-category aggregate query exists anywhere in pecunia.
- **Omni side**: `/agent` sessions are the only place an LLM turn actually runs (`runClaudeAgent`/`runCodexAgent` in `server/agent.go`, with MCP/skills wired per-workspace at install time). A **Plan** is a markdown page in the *memoria global wiki* at `omni-bot/plans/<slug>.md`, frontmatter `status: active|done`, written by the LLM via `TOOL:plan_save`; a `#long`-tagged plan registers a daily cron. A **Routine** is what Omni calls a cron — `crons(id, schedule, kind, text)`, kinds `message|prompt|agent` — created by the LLM via `TOOL:cron_add/edit/delete`. Conclusion: plugins have no direct API to either; only `/agent`, `/plan`, `/task` sessions can reach them.

A Plan agent then verified every cited fact against both repos before finalizing an implementation plan, and made three corrections to the first draft: no approval-gate mirroring is needed, since agent sessions are already yolo by design (Omni's own decision page: "Agent sessions stay yolo by design — their tools run inside the vendor CLI and are not interceptable"); the plans-directory question resolves via a context block Omni appends to the session's first message, not a placeholder mechanism; and `--forget` should identify its own crons by a fixed text marker (`[pecunia-coach]`) rather than tracked ids. It flagged one accepted blind spot: `applyAgentTools`'s confirmation lines replace the raw `TOOL:` lines in Omni's own chat history, but the vendor CLI's own transcript keeps the original — so a coach session cannot reliably self-audit which cron ids it just created *within the same run*. Cross-run `--forget` still works, since every run gets a fresh job list from the context block.

## Omni side: prompt-type plugin commands (branch `feat/prompt-commands`, PR #2)

- `pluginCommand` gained a `Prompt` field alongside `Argv`, validated as exactly-one-of at manifest install time — a plugin command declares one or the other, never both, never neither. The struct stays duplicated between `server/plugin.go` (read path) and `cli/plugins.go` (install/validate path), matching the project's existing no-shared-package pattern.
- `pluginReply` branches: a `Prompt`-declared command starts a fresh agent session — mirrors the `/agent` command's own path (`newSession(true, provider)` → `ensureAgentDir()` → `enqueue`) — instead of exec'ing, so the reply is async via the session queue rather than capped at the 60s exec timeout, reaching up to the 15-minute agent timeout instead.
- The session's first message: the declared `Prompt` + `Owner's message: <raw trailing words>` (not word-split, unlike the exec path, so punctuation in a quick update survives) +, when memoria is set up, a line naming the plan-pages directory + the always-appended scheduled-jobs block (current cron list plus the `TOOL:cron_add/edit/delete` contract).
- `applyAgentTools` (`server/media.go`) extended to also honor `cron_add`/`cron_edit`/`cron_delete` lines in an agent's reply (it previously only handled `task_start` and `send_file`), reusing the existing tool executors — no new gate, since agent-session tools are already ungated by design.
- Version bumped to v0.25.0; `PLUGINS.md` gained a "Prompt commands" section; the `plugin-system` wiki decision got an appended section.
- Pushed and opened as **PR #2** ("feat: prompt-type plugin commands run agent sessions").

## Pecunia side: /pecunia-coach (branch `feat/coach-command`)

- New `pecunia_situation` MCP tool (`cmd/mcp.go`), read-only, returning one plain-text snapshot assembled by `situationDo`/`plainSituation` (new `cmd/coach.go`): today's cash flow and month-to-date, alerts, accounts, cards with open-statement/limit-usage, recurring bills' due/overdue status, goals, budgets. Composed entirely from existing renderers (`plainSummary`, `plainBills`, `plainCC`, `plainGoals`, `plainBudgets`, `collectAlerts`) — no new formatting logic, the same "compose existing stores" call [[decisions/0012-summary-composes-existing-stores]] already made for `pecunia summary`.
- `collectCC` was extracted out of `runOmniCC` in `cmd/omni.go` into its own function so `coach.go` can reuse it without duplicating the loop.
- `omniCmd` was converted from positional struct literals to named fields (needed room for the new field) and grew an 8th manifest entry, `pecunia_coach`, declared with `Prompt: coachPrompt` and no `Argv` — manifest name `pecunia_coach` per Omni's `a-z0-9_` rule, the user types `/pecunia-coach` and Omni normalizes the hyphen.
- The coach prompt instructs the agent to: call `pecunia_situation` first, never invent figures, never sum or compare amounts across currencies; keep exactly one plan page at a fixed path (`<plans dir>/pecunia-coach.md`, never another slug); on first run, present the situation, interview the user 3-5 questions one at a time, then write the page (frontmatter `tags: [omni-bot, plan, pecunia-coach]`, `status: active`); on later runs, read the page, diff against the fresh snapshot, update its progress section, give 1-3 concrete tips; offer a twice-daily check-in reminder via `TOOL:cron_add` (kind `message`, text prefixed with the `[pecunia-coach]` marker so future runs can recognize and manage it); treat trailing words after the command as a quick update folded into the plan, and only write anything back into pecunia for a completed fact the user states and confirms — any such write goes through the existing MCP tools, so it lands on pecunia's own audit trail as source `ai` like every other MCP-driven write ([[decisions/0017-mcp-server-exposes-every-module-as-a-tool]]); `--forget` deletes the plan page and emits one `TOOL:cron_delete` per listed job whose text starts with the marker.
- Skill `cmd/skills/pecunia-omni.md` and `README.md` updated to mention the command; it requires Omni ≥ v0.25.0, since older Omni's manifest validation rejects a command with no `argv`.
- Version bumped 0.5.0 → 0.6.0.
- Live-verified against a rebuilt `dev` binary: `./dev omni-manifest` showed all 8 commands, the new one with no argv and a non-empty prompt; a manual MCP JSON-RPC smoke test (`initialize` → `tools/call pecunia_situation`) was run against `./dev mcp`. `gofmt`/`go vet`/`go test ./...` stayed clean through the build.
- Committed locally in three commits (`feat(coach): pecunia_situation MCP tool and the /pecunia-coach prompt command`, `docs: document /pecunia-coach in the readme`, `chore: bump version to 0.6.0`). The session then polled a background memoria-consolidation job, applied it, and committed the resulting wiki pages (decision 0024, this session's own page, the index) as a fourth commit — `docs(wiki): consolidate coach session — decision 0024, session page, index` — before pushing the branch and opening **PR #9**, "feat(coach): /pecunia-coach LLM financial coach". `gh pr checks 9 --watch` completed with exit code 0, confirming CI green.

## Sequencing dependency

Omni's v0.25.0 (PR #2) has to merge and the running server has to upgrade before pecunia's PR #9 (`feat/coach-command`) can be merged and its manifest installed anywhere — its `Prompt` field fails validation on older Omni.

## Update: both PRs' CI confirmed green; merging left to the user

After PR #9 opened, the session committed one more wiki-page update, pushed again, and re-ran `gh pr checks 9 --watch` — still exit code 0. The session's closing report to the user stated both PRs' CI came back green: omni **PR #2** ("feat: prompt-type plugin commands run agent sessions", v0.25.0) and pecunia **PR #9** ("feat(coach): /pecunia-coach LLM financial coach", v0.6.0) — resolving the earlier open question about PR #2's own CI outcome.

**Merging PR #2 was blocked by the permission classifier** — the agent could not merge it, so the rollout is manual and order-dependent, per the session's own closing instructions to the user: merge omni PR #2 first (releases v0.25.0) → upgrade the running Omni server → merge pecunia PR #9 (releases v0.6.0) → `omni plugins install johnvilela/pecunia` → try `/pecunia-coach` in Telegram. Older Omni rejects pecunia's new manifest (the `Prompt` field) at install time, which is why the order matters.

## Known holes

- **In-session cron-id blind spot**: a coach run cannot reliably self-audit the cron ids it just created within that same run, since `applyAgentTools`'s confirmation text replaces the raw `TOOL:` lines in Omni's history while the vendor CLI's own transcript keeps the original. Cross-run `--forget` is unaffected — it reads a fresh job list every time.
- `--forget` and the single-plan rule are enforced by the LLM following the prompt, not by code — a misfire leaves a stray plan page or cron the user can clean up via `/crons` or a re-run.
- Without memoria configured, the coach has no plans-dir line and runs stateless — the prompt explicitly covers this case.

Links: [[decisions/0023-pecunia-is-an-omni-plugin]] · [[decisions/0017-mcp-server-exposes-every-module-as-a-tool]] · [[decisions/0012-summary-composes-existing-stores]] · [[decisions/0002-flat-cmd-package-layout]] · [[sessions/dbafef3b-f6da-491c-a20f-2d21feaf35fd]]