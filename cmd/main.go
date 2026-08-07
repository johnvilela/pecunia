package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"kakei/internal/core"
)

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
  accounts | ac     manage accounts (new, edit, delete, freeze, details)
  credit-card | cc  manage credit cards (new, edit, delete, details)
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

	case "setup":
		fs := flag.NewFlagSet("setup", flag.ExitOnError)
		force := fs.Bool("force", false, "back up the existing database and create a new one")
		fs.Parse(os.Args[2:])
		return report("setup", runSetup(*force))

	case "accounts", "ac":
		return report("accounts", runAccounts(os.Args[2:]))

	case "credit-card", "cc":
		return report("credit-card", runCards(os.Args[2:]))

	default:
		fmt.Fprintf(os.Stderr, "kakei: unknown command %q\n\n", os.Args[1])
		fmt.Fprint(os.Stderr, help)
		return 2
	}
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
