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
| `notes [new\|edit\|delete\|sync] [ID]` | `n` | Notes with a priority that moves — markdown files edited in your own `$EDITOR`. Bare `ID` shows one; `--path ID` prints its file. Filters: `--all`, `--status`, `--priority`, `--min-score`, `--tag`, `--search`, `--due`, `--overdue`, `--account`, `--card`, `--goal` |
| `bill [new\|pay\|edit\|delete\|archive\|unarchive] [CODE]` | `b` | Recurring bills — the ones that come round every month. `--all` includes archived |
| `budget [new\|edit\|delete\|archive\|unarchive] [CODE]` | `bg` | Monthly caps per category. `--month YYYY-MM` shows another month, `--all` includes archived |
| `logs [--entity NAME] [--id N] [--action NAME] [--source NAME] [--from DATE] [--to DATE] [--limit N]` | `l` | Audit trail, newest first — every create, edit and delete, whether it came from you, the system or an AI agent |
| `mcp` / `mcp install [AGENT]` | — | Serve every module to an AI agent over MCP on stdio; `install` registers it with claude-code, codex, gemini or opencode |
| `backup [setup\|run\|list\|restore\|schedule]` | — | Copy the database and the notes to a directory, an S3 bucket, Dropbox or Google Drive, encrypted if you give a passphrase, on a systemd timer if you give a schedule. See [Backup](#backup) |
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
| `pecunia_notes` | The owner's notes, with their effective priority; read and write bodies |
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
| `/pecunia-notes [level \| words]` | Open notes, highest effective priority first. A level word keeps only that level; other words search titles and bodies |
| `/pecunia-alerts` | Only problems — overdue bills, budgets over cap, cards near their limit. Silent when all is well, which makes it a free daily nudge as an Omni scheduled task |
| `/pecunia-add AMOUNT TITLE [@ACCOUNT] [#CATEGORY]` | Quick expense, e.g. `/pecunia-add 12.50 lunch #food`. With one account the `@CODE` is optional; with more, pecunia asks rather than guesses |

One command does use the LLM — `/pecunia-coach` starts an agent session that reads your situation (via the `pecunia_situation` MCP tool), interviews you, keeps a single coaching plan in Omni's plan pages and offers twice-daily check-in reminders. Words after the command are a quick update for the coach; `/pecunia-coach --forget` wipes the plan and its reminders. Requires omni ≥ v0.25.0 — older installs reject the manifest.

## Data

Everything lives in a single SQLite file:

1. `$PECUNIA_DB`, if set
2. `~/.config/pecunia/pecunia.db` on Linux, `~/Library/Application Support/pecunia/pecunia.db` on macOS

The file is created `0600` and migrations apply automatically on every run. Amounts are stored as integers in minor units, and currencies are never added together — there is no exchange rate anywhere in pecunia.

Notes are the one thing that lives outside it: each is a markdown file in `notes/` beside the database (`$PECUNIA_NOTES` overrides), with its title, priority, status, target, tags and links in a YAML front matter block and the body yours. SQLite keeps only the index — counters, the file's mtime — so you can edit the files with anything and the next `pecunia n` picks the change up (`pecunia n sync` adopts files you dropped in).

The priority you write in a note never changes by itself. Beside it pecunia shows an effective one, worked out on every read from the base, how near the target is, how often you open the note, activity on the accounts, cards or goals it names, and how long it has sat untouched — so a LOW note surfaces as HIGH when its time comes, and a HIGH one sinks once forgotten.

## Backup

`pecunia backup setup` asks where the archives go — a directory (an external drive, a folder another tool syncs), an S3 bucket (AWS, or anything that speaks S3: MinIO, Cloudflare R2, Backblaze B2 with an endpoint), Dropbox or Google Drive — how often, how many to keep, and whether to encrypt them. Every answer is also a flag, for scripts:

```sh
pecunia backup setup --provider s3 --bucket my-bucket --region eu-west-1 \
  --access-key AKIA... --secret-key ... --every 2/day --keep 14 --passphrase 'open sesame'
pecunia backup setup --provider dropbox --app-key <key> --every daily --keep 30
pecunia backup setup --provider gdrive --client-id <id> --client-secret <secret> --every 1/week
pecunia backup run            # one archive, now
pecunia backup list           # what the bucket holds
pecunia backup restore        # the newest one back in place; the live files move aside as .bak
pecunia backup schedule off   # stop the timer
```

Dropbox needs an app of your own, made once at [dropbox.com/developers/apps](https://www.dropbox.com/developers/apps) (scoped access, an app folder is enough, with the `files.content.write`, `files.content.read` and `files.metadata.read` permissions). Setup takes its app key, prints a URL to approve the app at, asks for the code Dropbox shows, and keeps the refresh token it gets back — the consent page is seen once; every run after mints its own short-lived access token. Archives land in `/Apps/pecunia` unless you say `--folder`.

Google Drive needs an OAuth client of your own: in the [Google Cloud console](https://console.cloud.google.com/) make a project, turn the Drive API on, and create an OAuth client of type *Desktop app* (add yourself as a test user while the consent screen is in testing). Setup takes its id and secret and prints a URL to approve the app at; the browser comes back to pecunia on this machine, or — on a headless box — you paste the address it lands on. The scope is `drive.file`, so pecunia can see only the files it made itself, in a `pecunia` folder at the top of My Drive (`--folder` names another).

An archive is `pecunia-<moment>.tar.gz`: a consistent snapshot of the database (taken through SQLite, so a write in flight is never half in it) plus every file under the notes directory. With a passphrase it is an [age](https://age-encryption.org) file — `pecunia-<moment>.tar.gz.age` — that the `age` tool opens too, so you never need pecunia to get your data back. The archive holds only your data: never `backup.toml`, which holds the credentials.

The schedule — `2/day`, `3/week`, `daily`, `weekly` — becomes a systemd user timer that runs `pecunia backup run` (`Persistent=true`, so a run missed while the machine was off happens at the next boot). Without systemd, `schedule` prints the crontab line instead. `keep N` prunes the oldest archives after every run so the bucket stays at N.

Settings live in `backup.toml` beside the database, `0600`. The secrets can stay out of it: `PECUNIA_BACKUP_PASSPHRASE`, `PECUNIA_BACKUP_S3_ACCESS_KEY`, `PECUNIA_BACKUP_S3_SECRET_KEY`, `PECUNIA_BACKUP_DROPBOX_REFRESH_TOKEN` and `PECUNIA_BACKUP_GDRIVE_REFRESH_TOKEN` override whatever the file says.

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
