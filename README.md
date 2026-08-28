# Kakei - Personal Finance Manager CLI

Kakei (家計) is a CLI to manage your personal finances holding everything on your PC and using your favorite LLM to help you understand and plan better. Inspired by hledger, this is crafted to match my personal needs as a guy who NEEDS to control the finances but struggles to use the mainstream apps. The main problem is that as apps that focus on controlling your expenses, they focus too much on selling me something that most of the time was not worth it. This aims to be simple, direct and easy to use. First starting with a CLI, but with plans to create a PWA, mobile app and even a self-hosted ecosystem. Take control of what is yours.

## Install

```sh
curl -sS https://raw.githubusercontent.com/johnvilela/kakei/master/scripts/install.sh | sh
```

The script downloads the latest release binary for your platform (Linux/macOS, amd64/arm64, checksum-verified, no Go needed) to `~/.local/bin`, then runs `kakei setup` — which creates the SQLite database, seeds starter categories and offers to hook kakei up to an AI agent. Later, `kakei upgrade` updates it in place.

Or manually:

```sh
# grab a tarball from https://github.com/johnvilela/kakei/releases

# or from a local checkout (requires Go):
scripts/install.sh      # builds and installs to ~/.local/bin + runs setup
scripts/build.sh        # just builds ./kakei
```

Set `BIN_DIR` to install somewhere other than `~/.local/bin`.

## Quick start

```sh
# 1. Create the database and seed starter categories (once)
kakei setup

# 2. Create an account and a credit card (interactive forms)
kakei accounts new
kakei credit-card new

# 3. Record what happens
kakei transactions new     # a purchase, a salary, a bill paid
kakei t transfer           # move money between your accounts
kakei bill new             # a recurring bill (rent, subscription)
kakei budget new           # a monthly cap for a category

# 4. See where you stand
kakei summary              # today, on one screen
kakei s --month            # the whole month
kakei t --month 2026-08    # the ledger for a month
kakei t --category food    # what a category cost
kakei goals                # how the goals are doing

# 5. Stay current
kakei upgrade              # update to the latest release
```

Every command opens an interactive picker or form when you leave arguments out, and `-h` on any command prints its own help.

## Commands

| Command | Alias | Description |
|---------|-------|-------------|
| `setup [--force]` | — | Create the SQLite database and seed starter categories, then offer to hook kakei up to an AI agent. `--force` backs up the existing database and creates a fresh one |
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

`kakei mcp install` registers `kakei mcp` with your agent (claude-code, codex, gemini or opencode — leaving the agent out opens a picker), so sessions get one tool per module:

| Tool | What it does |
|------|--------------|
| `kakei_summary` | Where you stand — the same one-screen figures as `kakei summary` |
| `kakei_accounts` | List, create, edit, delete and freeze accounts |
| `kakei_credit_cards` | Manage credit cards, their bills and payments |
| `kakei_categories` | Manage categories |
| `kakei_transactions` | Record and review transactions and transfers |
| `kakei_goals` | Track goals |
| `kakei_recurring_bills` | Manage recurring bills |
| `kakei_budgets` | Manage monthly caps per category |
| `kakei_logs` | Read the audit trail |

Reads and writes go through the same stores the CLI uses, and every agent write is logged with source `ai` — `kakei logs --source ai` shows exactly what an agent did. Amounts everywhere are integers in minor units (cents; satoshis for BTC).

## Data

Everything lives in a single SQLite file:

1. `$KAKEI_DB`, if set
2. `~/.config/kakei/kakei.db` on Linux, `~/Library/Application Support/kakei/kakei.db` on macOS

The file is created `0600` and migrations apply automatically on every run. Amounts are stored as integers in minor units, and currencies are never added together — there is no exchange rate anywhere in kakei.

## Technology used

- Go
- SQLite
- charmbracelet (bubbletea, bubbles, huh, lipgloss)

## Development

```sh
scripts/dev.sh             # builds ./dev wired to an isolated kakei.dev.db + seeds it
scripts/dev.sh --reseed    # recreate the dev database from scratch
scripts/build.sh           # release-style build (GOOS/GOARCH to cross-compile)
go test ./...
```

See [AGENTS.md](AGENTS.md) for project context and `wiki/` for the decision log.
