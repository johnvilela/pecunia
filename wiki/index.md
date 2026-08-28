# Wiki index

Map of the project memory. Read this first, then grep `wiki/` or follow the links. Pages carry YAML `tags:` frontmatter for topic lookup.

## Rules — conventions every session must follow

- [tdd](rules/tdd.md) — the red/green cycle used for all module work
- [commit-messages](rules/commit-messages.md) — Conventional Commits format
- [git-hooks](rules/git-hooks.md) — per-clone hook setup (`gofmt`/`go vet` pre-commit)

## Decisions — why the code is the way it is

- [0001-balance-as-int64-minor-units](decisions/0001-balance-as-int64-minor-units.md) — money is int64 minor units, never floats
- [0002-flat-cmd-package-layout](decisions/0002-flat-cmd-package-layout.md) — flat `cmd/kakei/` package, no deep tree
- [0003-static-stripped-build-script](decisions/0003-static-stripped-build-script.md) — `build.sh` makes one static, stripped binary
- [0004-dev-build-isolated-by-ldflags](decisions/0004-dev-build-isolated-by-ldflags.md) — dev build with seeded `kakei.dev.db`, isolated via ldflags
- [0005-internal-core-shared-kernel](decisions/0005-internal-core-shared-kernel.md) — shared kernel in `internal/core`
- [0006-credit-card-money-schedule-and-over-limit-model](decisions/0006-credit-card-money-schedule-and-over-limit-model.md) — card limits, closing/due schedule, over-limit behavior
- [0007-category-starter-set-seeded-from-go](decisions/0007-category-starter-set-seeded-from-go.md) — starter categories seeded from Go code, not SQL
- [0008-transaction-double-entry-tags-and-filters](decisions/0008-transaction-double-entry-tags-and-filters.md) — transaction model, balance updates, tags and list filters
- [0009-bills-as-rows-and-installments-as-transactions](decisions/0009-bills-as-rows-and-installments-as-transactions.md) — card bills generated on read; installments are ordinary transactions
- [0010-goal-progress-summed-from-the-ledger](decisions/0010-goal-progress-summed-from-the-ledger.md) — goal progress computed from transactions, never stored
- [0011-recurring-bills-derived-from-payments](decisions/0011-recurring-bills-derived-from-payments.md) — recurring bills are templates; paid/due state derived from payments
- [0012-summary-composes-existing-stores](decisions/0012-summary-composes-existing-stores.md) — `kakei s` composes existing stores, no new tables
- [0013-data-integrity-fixes-and-known-gaps](decisions/0013-data-integrity-fixes-and-known-gaps.md) — WAL + busy timeout, 0600 files, currency freeze, frozen-account guard — **and the list of gaps, now all closed or scoped**
- [0014-logs-as-a-single-audit-table](decisions/0014-logs-as-a-single-audit-table.md) — one audit row per logical action, source user/system/ai, JSON diffs, no FK
- [0015-balance-adjustments-as-a-hidden-kind](decisions/0015-balance-adjustments-as-a-hidden-kind.md) — balance edits file signed adjustment transactions; card balances ledger-only; first table-rebuild migration
- [0016-bill-reads-stop-writing](decisions/0016-bill-reads-stop-writing.md) — reads write only when a cycle turns; charges refresh their bill at write time; write-time never creates bill rows
- [0017-mcp-server-exposes-every-module-as-a-tool](decisions/0017-mcp-server-exposes-every-module-as-a-tool.md) — `kakei mcp` serves all nine modules over MCP/stdio; `logs.Actor` stamps agent writes as `ai`
- [0018-mcp-install-writes-agent-config-files](decisions/0018-mcp-install-writes-agent-config-files.md) — `kakei mcp install <agent>`, offered at the end of `kakei setup` — shells out to an agent's own CLI, or merges JSON directly for opencode
- [0019-pr-only-master-with-ci-and-release-on-merge](decisions/0019-pr-only-master-with-ci-and-release-on-merge.md) — master takes PRs only (ruleset, admin included); CI runs gofmt/vet/build/test on PRs; bumping `var version` in cmd/main.go releases `v<version>` with 4 tarballs on merge
- [0020-upgrade-command-self-updates-and-migrates](decisions/0020-upgrade-command-self-updates-and-migrates.md) — `kakei upgrade` fetches GitHub releases, shows every skipped changelog, rename-swaps the binary and execs the new build's `migrate`; `-y` skips only the prompt; `core.Confirm` grew an affirmative-label param

## Gotchas — bugs that already bit once

- [account-code-validation-vs-generation-alphabet](gotchas/account-code-validation-vs-generation-alphabet.md) — code validator and generator used different alphabets
- [huh-form-skips-validators-on-eof](gotchas/huh-form-skips-validators-on-eof.md) — `huh` forms skip validators on EOF

## Tasks — module specs, in build order

- [01-accounts-module](tasks/01-accounts-module.md)
- [02-credit-card-module](tasks/02-credit-card-module.md)
- [03-category-module](tasks/03-category-module.md)
- [04-transactions-module](tasks/04-transactions-module.md)
- [05-installments-and-credit-card-bill](tasks/05-installments-and-credit-card-bill.md)
- [06-goals-module](tasks/06-goals-module.md)
- [07-recurring-bills-module](tasks/07-recurring-bills-module.md)
- [08-summary-module](tasks/08-summary-module.md)
- [09-budgets-module](tasks/09-budgets-module.md)
- [10-transfers](tasks/10-transfers.md)
- [11-logs-module](tasks/11-logs-module.md)

## Sessions — what each past session did

- [13ad2597](sessions/13ad2597-6868-468a-b30d-d49f105208cf.md) — bootstrap: CLI skeleton, hooks, first accounts module
- [a1d26976](sessions/a1d26976-dbc4-45f1-a548-d79fc66d022d.md) — `build.sh` static binary
- [2d27f8ef](sessions/2d27f8ef-e996-46a1-80f7-d9457f69527b.md) — accounts TDD suite, dev-mode tooling, list/detail UI redesign
- [ce07d7cb](sessions/ce07d7cb-4a82-4381-89cb-9ad513a7159d.md) — credit-card module end to end
- [2a7339bb](sessions/2a7339bb-af86-47f1-a0fb-8fbc097dd9ea.md) — category module, then transactions module
- [24176110](sessions/24176110-2c6e-4514-9d14-5d9ef35face9.md) — card bills and installments
- [b04280f1](sessions/b04280f1-f379-437b-bf05-df83f355e2f4.md) — bill month-name column follow-up
- [4b4dcd74](sessions/4b4dcd74-5218-4aa9-b73f-c1f8ae4e3279.md) — goals module, bills close-out, target-change log
- [5321cd80](sessions/5321cd80-4dd0-4dea-85c3-391b008334d2.md) — recurring bills module, spec settled via Q&A
- [b4318b40](sessions/b4318b40-e452-4a98-bf96-2a937fcccfdc.md) — summary command (`kakei s`), rendering fixes
- [c2b6cbbe](sessions/c2b6cbbe-5735-4790-abac-4c4b5a60aca7.md) — full-CLI evaluation, budgets module, data-integrity fixes, transfers
- [55542fff](sessions/55542fff-857b-4326-bc93-9554a745c41b.md) — logs module, GitHub metadata, wiki index, balance-adjustment fix, bill-reads-stop-writing fix — closes the last of decision 0013's gaps
- [c1d09b19](sessions/c1d09b19-d5ad-4ed0-b929-4d689f8bd290.md) — MCP server exposing all nine modules to an AI agent, plus an agent-config installer wired into `kakei setup`; both committed together
- [8a34db89](sessions/8a34db89-ce6a-4525-a01c-052289ef8b89.md) — master locked to PR-only with CI checks and release-on-merge (PR #1, merged), then the `kakei upgrade` self-update command (PR #2, open at session end)

