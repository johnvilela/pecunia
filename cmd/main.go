package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"

	"kakei/internal/core"
	"kakei/internal/db"
)

// version is the single source of truth for releases: bumping it on master
// makes CI tag and publish v<version>.
var version = "0.2.0"

const banner = `
 ___  __    ________  ___  __    _______   ___
|\  \|\  \ |\   __  \|\  \|\  \ |\  ___ \ |\  \
\ \  \/  /|\ \  \|\  \ \  \/  /|\ \   __/|\ \  \
 \ \   ___  \ \   __  \ \   ___  \ \  \_|/_\ \  \
  \ \  \\ \  \ \  \ \  \ \  \\ \  \ \  \_|\ \ \  \
   \ \__\\ \__\ \__\ \__\ \__\\ \__\ \_______\ \__\
    \|__| \|__|\|__|\|__|\|__| \|__|\|_______|\|__|

              personal finance, on your machine
`

const help = `Usage:
  kakei <command> [flags]

Commands:
  setup             create the SQLite database (--force to back up and recreate)
  summary | s       where you stand today (--date, --month)
  accounts | ac     manage accounts (new, edit, delete, freeze, details)
  credit-card | cc  manage credit cards (new, edit, delete, bill, pay)
  category | ct     manage categories (new, edit, delete, details)
  transactions | t  record and review transactions (new, edit, delete)
  goals | g         track goals (new, edit, delete, details)
  bill | b          recurring bills (new, pay, edit, delete, archive)
  budget | bg       monthly caps per category (new, edit, delete, archive)
  logs | l          what happened, newest first (--entity, --id, --action,
                    --source, --from, --to)
  mcp               serve every module to an AI agent over MCP, on stdio
                    (mcp install [AGENT] hooks it up to claude-code, codex,
                    gemini or opencode)
  upgrade           update kakei to the latest release (-y to skip the prompt)
  migrate           apply any pending database migrations
  version           show the version
  help              show this message

Environment:
  KAKEI_DB   path to the database file
             (default: ~/.config/kakei/kakei.db)
`

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) < 2 {
		fmt.Print(banner, "\n", help)
		return 0
	}

	switch os.Args[1] {
	case "help", "-h", "--help":
		fmt.Print(banner, "\n", help)
		return 0

	case "version", "-v", "--version":
		fmt.Println(version)
		return 0

	case "setup":
		fs := flag.NewFlagSet("setup", flag.ExitOnError)
		force := fs.Bool("force", false, "back up the existing database and create a new one")
		fs.Parse(os.Args[2:])
		return report("setup", runSetup(*force))

	case "summary", "s":
		return report("summary", runSummary(os.Args[2:]))

	case "accounts", "ac":
		return report("accounts", runAccounts(os.Args[2:]))

	case "credit-card", "cc":
		return report("credit-card", runCards(os.Args[2:]))

	case "category", "ct":
		return report("category", runCategories(os.Args[2:]))

	case "transactions", "t":
		return report("transactions", runTransactions(os.Args[2:]))

	case "goals", "g":
		return report("goals", runGoals(os.Args[2:]))

	case "bill", "b":
		return report("bill", runRecurring(os.Args[2:]))

	case "budget", "bg":
		return report("budget", runBudgets(os.Args[2:]))

	case "logs", "l":
		return report("logs", runLogs(os.Args[2:]))

	case "mcp":
		return report("mcp", runMCP(os.Args[2:]))

	case "upgrade":
		return report("upgrade", runUpgrade(os.Args[2:]))

	case "migrate":
		return report("migrate", runMigrate())

	default:
		fmt.Fprintf(os.Stderr, "kakei: unknown command %q\n\n", os.Args[1])
		fmt.Fprint(os.Stderr, help)
		return 2
	}
}

// withConn opens the database, hands the connection over and closes it after.
// Transactions need four stores off one connection, which is what pulled this
// out of the copy each module used to keep.
func withConn(fn func(*sql.DB) error) error {
	conn, err := db.Open()
	if err != nil {
		return err
	}
	defer conn.Close()
	return fn(conn)
}

// report turns a command's error into an exit code. Backing out of a form or
// picker is a decision, not a failure, so it exits quietly.
func report(cmd string, err error) int {
	switch {
	case err == nil, errors.Is(err, core.ErrCancelled):
		return 0
	default:
		fmt.Fprintf(os.Stderr, "kakei %s: %v\n", cmd, err)
		return 1
	}
}
