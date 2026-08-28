---
tags: [mcp, install, cli, config, opencode]
---

## Decision

Follow-up to [[decisions/0017-mcp-server-exposes-every-module-as-a-tool]], same session. User: "on the 'pecunia mcp' does it install the MCP? If it doesnt: we may add a flag on the 'pecunia setup' that install the MCP on the selected AI agent (codex, claude-code, opencode...)." A plan was written before any code (`~/.claude/plans/on-the-pecunia-mcp-iridescent-pine.md`, outside the repo).

The installer landed as `pecunia mcp install <agent>`, plus a step at the end of `pecunia setup` that offers to run it — not a `setup` flag as first floated. `cmd/mcp_install.go` plus edits to `cmd/mcp.go`, `cmd/main.go` and `cmd/setup.go`.

## Two install strategies

- **Agents with their own MCP-add CLI** (claude-code, codex, gemini checked live for a `scope` flag via `--help`; cursor also exercised) — install by shelling out to that agent's own `mcp add` command at user/global scope, built from an argv (`TestAgentArgv`). A missing CLI is reported as `"<agent> not found on PATH — is <agent> installed?"` rather than a raw exec error.
- **opencode**, which has no add CLI — `pecunia mcp install opencode` merges an `mcp` entry directly into `$XDG_CONFIG_HOME/opencode/opencode.json` (`TestOpencodeMerge`). Live-tested against a config that already had unrelated keys (`theme`, an existing `mcp.other` remote entry) — both preserved, pecunia's own entry added alongside. Live-tested a second time against a config containing a `//`-style JSONC comment: the merge **refuses to touch it** and prints a paste-ready snippet instead, rather than risk corrupting a file it can't safely re-serialize. (This supersedes an earlier read of the same test as "the merge survives non-strict JSON" — the actual behavior is refuse-and-suggest, not silently write through the comment.)

## Other details

- No agent named on the command → an interactive picker over the supported agents (`core.Pick`).
- What gets registered is the **absolute path of the running binary** — a dev build's install therefore points an agent at the `dev` binary and its isolated `pecunia.dev.db` ([[decisions/0004-dev-build-isolated-by-ldflags]]), not at the real `pecunia`.
- `pecunia setup` now ends with "Hook pecunia up to an AI agent?" — declines silently with no TTY (verified with `echo | pecunia setup`, exits clean), so scripted/piped setup runs are unaffected.
- Live-verified: `pecunia mcp install claude-code` produced a `✔ Connected` entry in `claude mcp list` — Claude Code actually spawned the server and completed the MCP handshake. `codex` also installed and connected live. Both test entries were removed afterward since they pointed at a scratch build, not the real binary.

## Verification

`go test`/`gofmt`/`go vet` clean. Unit tests cover the argv table (`TestAgentArgv`) and all three opencode-merge cases (`TestOpencodeMerge`). Committed together with the MCP server itself as a single commit, `d132395` — `feat(mcp): add mcp server with per-module tools and agent installer` — via a plain `/git-commit` request, not split by feature this time despite covering two distinct concerns ([[rules/commit-messages]]'s usual split-by-concern pattern was not invoked here).

## Skipped

- Uninstall and update commands — each agent's own `mcp remove` covers uninstall; re-running `install` after a rebuild covers update, since it re-registers the current binary path.

Links: [[decisions/0017-mcp-server-exposes-every-module-as-a-tool]] · [[decisions/0004-dev-build-isolated-by-ldflags]] · [[rules/commit-messages]] · [[sessions/c1d09b19-d5ad-4ed0-b929-4d689f8bd290]]