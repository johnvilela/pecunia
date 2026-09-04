# Pecunia - Personal Finance Manager CLI

Pecunia (Latin for money, wealth) is a CLI to manage your personal finances holding everything on your PC and using your favorite LLM to help you understand and plan better. Inspired by hledger, this is crafted to match my personal needs as a guy who NEEDS to control the finances but struggles to use the mainstream apps. The main problem is that as apps that focus on controlling your expenses, they focus too much on selling me something that most of the time was not worth it. This aims to be simple, direct and easy to use. First starting with a CLI, but with plans to create a PWA, mobile app and even a self-hosted ecosystem. Take control of what is yours.

## Install

```sh
curl -sS https://raw.githubusercontent.com/johnvilela/pecunia/master/scripts/install.sh | sh
```

The script downloads the latest release binary for your platform (Linux/macOS, amd64/arm64, checksum-verified, no Go needed) to `~/.local/bin`, then runs `pecunia setup` — which creates the SQLite database, seeds starter categories and offers to hook pecunia up to an AI agent. Later, `pecunia upgrade` updates it in place.

Or manually:

```sh
# grab a tarball from https://github.com/johnvilela/pecunia/releases

# or from a local checkout (requires Go):
scripts/install.sh      # builds and installs to ~/.local/bin + runs setup
scripts/build.sh        # just builds ./pecunia
```

Set `BIN_DIR` to install somewhere other than `~/.local/bin`.

## Quick start

```sh
# 1. Create the database and seed starter categories (once)
pecunia setup

# 2. Create an account and a credit card (interactive forms)
pecunia accounts new
pecunia credit-card new

# 3. Record what happens
pecunia transactions new     # a purchase, a salary, a bill paid
pecunia t transfer           # move money between your accounts
pecunia bill new             # a recurring bill (rent, subscription)
pecunia budget new           # a monthly cap for a category

# 4. See where you stand
pecunia summary              # today, on one screen
pecunia s --month            # the whole month
pecunia t --month 2026-08    # the ledger for a month
pecunia t --category food    # what a category cost
pecunia goals                # how the goals are doing

# 5. Stay current
pecunia upgrade              # update to the latest release
```

Every command opens an interactive picker or form when you leave arguments out, and `-h` on any command prints its own help.

## Commands

| Command | Alias | Description |
|---------|-------|-------------|
| `setup [--force] [--skills]` | — | Create the SQLite database and seed starter categories, then offer to hook pecunia up to an AI agent. `--force` backs up the existing database and creates a fresh one; `--skills` installs the finance skills into your AI agents |
| `summary [--date YYYY-MM-DD] [--month]` | `s` | Where you stand on one screen: in and out, what needs paying, account and card balances, goal progress. The flags stack: `--month --date 2026-07-04` is that day's whole month |
| `accounts [new\|edit\|delete\|freeze] [CODE\|ID]` | `ac` | Manage accounts. Bare `CODE\|ID` shows one in detail; `--all`/`-a` includes frozen accounts |
| `credit-card [new\|edit\|delete\|bill\|pay] [CODE\|ID]` | `cc` | Manage credit cards. `bill [ref] [YYYY-MM]` lists bills or shows one, `pay` pays one |
| `category [new\|edit\|delete] [CODE\|ID]` | `ct` | Manage categories |
| `transactions [new\|transfer\|edit\|delete] [ID]` | `t` | Record and review transactions. Default list is this month; filters combine: `--all`, `--transfers`, `--date`, `--month`, `--from`, `--to`, `--tag`, `--search`, `--category`, `--account`, `--card`, `--goal` |
| `goals [new\|edit\|delete] [ID]` | `g` | Track goals. `--resume` prints a compact table without progress bars |
| `bill [new\|pay\|edit\|delete\|archive\|unarchive] [CODE]` | `b` | Recurring bills — the ones that come round every month. `--all` includes archived |
| `budget [new\|edit\|delete\|archive\|unarchive] [CODE]` | `bg` | Monthly caps per category. `--month YYYY-MM` shows another month, `--all` includes archived |
| `logs [--entity NAME] [--id N] [--action NAME] [--source NAME] [--from DATE] [--to DATE] [--limit N]` | `l` | Audit trail, newest first — every create, edit and delete, whether it came from you, the system or an AI agent |
| `mcp` / `mcp install [AGENT]` | — | Serve every module to an AI agent over MCP on stdio; `install` registers it with claude-code, codex, gemini or opencode |
| `upgrade [-y]` | — | Check GitHub for a newer release, show the changelog, replace the binary in place and migrate the database. `-y` skips the prompt |
| `migrate` | — | Apply any pending database migrations (also happens automatically on every run) |
| `version` | `-v` | Show the version |
| `help` | `-h` | Show usage |

## MCP server

`pecunia mcp install` registers `pecunia mcp` with your agent (claude-code, codex, gemini or opencode — leaving the agent out opens a picker), so sessions get one tool per module:

| Tool | What it does |
|------|--------------|
| `pecunia_summary` | Where you stand — the same one-screen figures as `pecunia summary` |
| `pecunia_accounts` | List, create, edit, delete and freeze accounts |
| `pecunia_credit_cards` | Manage credit cards, their bills and payments |
| `pecunia_categories` | Manage categories |
| `pecunia_transactions` | Record and review transactions and transfers |
| `pecunia_goals` | Track goals |
| `pecunia_recurring_bills` | Manage recurring bills |
| `pecunia_budgets` | Manage monthly caps per category |
| `pecunia_logs` | Read the audit trail |

Reads and writes go through the same stores the CLI uses, and every agent write is logged with source `ai` — `pecunia logs --source ai` shows exactly what an agent did. Amounts everywhere are integers in minor units (cents; satoshis for BTC).

`pecunia setup --skills` installs four finance skills alongside — `pecunia-overview` (where you stand, with alerts and tips), `pecunia-budget` (caps built from your real spending), `pecunia-import` (statements from PDF/CSV/JSON, without duplicates) and `pecunia-health` (money leaks, ranked by impact) — into `~/.agents/skills` and `~/.claude/skills`, where all four supported agents read them.

## Omni plugin

Pecunia is an [Omni](https://github.com/johnvilela/omni) plugin — the binary itself answers Omni's plugin contract, so on the Omni host:

```sh
omni plugins install johnvilela/pecunia
```

That wires the MCP server and the skills (all of the above, plus `pecunia-omni`) into Omni agent sessions, and registers Telegram commands that print your data instantly, with no LLM involved:

| Command | What it shows |
|---------|---------------|
| `/pecunia-resume [period]` | Balances, money in and out, and any alerts. Periods: `today`, `yesterday`, `week`, `last week`, `month`, `last month`, `YYYY-MM`, `YYYY-MM-DD` |
| `/pecunia-goals` | Every goal and its progress |
| `/pecunia-bills` | Recurring bills and where this cycle stands |
| `/pecunia-cc` | Cards: limit, used, available, the open statement |
| `/pecunia-budget` | This month's caps against actual spend |
| `/pecunia-alerts` | Only problems — overdue bills, budgets over cap, cards near their limit. Silent when all is well, which makes it a free daily nudge as an Omni scheduled task |
| `/pecunia-add AMOUNT TITLE [@ACCOUNT] [#CATEGORY]` | Quick expense, e.g. `/pecunia-add 12.50 lunch #food`. With one account the `@CODE` is optional; with more, pecunia asks rather than guesses |

One command does use the LLM — `/pecunia-coach` starts an agent session that reads your situation (via the `pecunia_situation` MCP tool), interviews you, keeps a single coaching plan in Omni's plan pages and offers twice-daily check-in reminders. Words after the command are a quick update for the coach; `/pecunia-coach --forget` wipes the plan and its reminders. Requires omni ≥ v0.25.0 — older installs reject the manifest.

## Data

Everything lives in a single SQLite file:

1. `$PECUNIA_DB`, if set
2. `~/.config/pecunia/pecunia.db` on Linux, `~/Library/Application Support/pecunia/pecunia.db` on macOS

The file is created `0600` and migrations apply automatically on every run. Amounts are stored as integers in minor units, and currencies are never added together — there is no exchange rate anywhere in pecunia.

## Technology used

- Go
- SQLite
- charmbracelet (bubbletea, bubbles, huh, lipgloss)

## Development

```sh
scripts/dev.sh             # builds ./dev wired to an isolated pecunia.dev.db + seeds it
scripts/dev.sh --reseed    # recreate the dev database from scratch
scripts/build.sh           # release-style build (GOOS/GOARCH to cross-compile)
go test ./...
```

See [AGENTS.md](AGENTS.md) for project context and `wiki/` for the decision log.
