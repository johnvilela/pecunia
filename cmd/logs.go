package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"kakei/internal/logs"
	"kakei/internal/transactions"
)

const logsHelp = `Logs — what happened, newest first.

Usage:
  kakei logs [flags]
  kakei l    [flags]

Flags:
  --entity NAME     one kind of thing: account, card, category, transaction,
                    transfer, goal, recurring, budget or card_bill
  --id N            one thing in particular (asks for --entity: an id is only
                    an id of something)
  --action NAME     created, edited or deleted
  --source NAME     user, system or ai
  --from YYYY-MM-DD on or after this day
  --to YYYY-MM-DD   on or before this day
  --limit N         how many rows (default 10)

Every create, edit and delete lands here, whoever caused it — you at the
terminal, or kakei itself generating a card bill. An edit carries only what
changed: the fields that moved, what they said before and what they say now.

The trail outlives what it describes. Deleting an account does not delete the
record of ever having had one.
`

// logEntities is what --entity may name. The table itself takes anything — a
// new module should not need a migration to log — so the vocabulary is held
// here, where a typo can actually happen.
var logEntities = []string{
	"account", "card", "category", "transaction", "transfer",
	"goal", "recurring", "budget", "card_bill",
}

func runLogs(args []string) error {
	if len(args) > 0 && isHelpFlag(args[0]) {
		fmt.Fprint(out, logsHelp)
		return nil
	}

	f, err := parseLogFlags(args)
	if err != nil {
		return err
	}

	return withConn(func(conn *sql.DB) error {
		found, err := logs.List(conn, f)
		if err != nil {
			return err
		}
		if len(found) == 0 {
			fmt.Fprintln(out, `nothing logged that matches — the trail starts with the next thing you do`)
			return nil
		}
		fmt.Fprintln(out, logs.Table(found))
		return nil
	})
}

// parseLogFlags turns the command line into a filter, refusing anything the
// trail could never hold before the database is even opened.
func parseLogFlags(args []string) (logs.Filter, error) {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	// The flag package's own usage dump would print the flags a second time,
	// right next to the error report() already prints. kakei l -h is the one
	// that documents them.
	fs.SetOutput(io.Discard)
	var (
		entity = fs.String("entity", "", "one kind of thing")
		id     = fs.Int64("id", 0, "one thing in particular")
		action = fs.String("action", "", "created, edited or deleted")
		source = fs.String("source", "", "user, system or ai")
		from   = fs.String("from", "", "on or after this day")
		to     = fs.String("to", "", "on or before this day")
		limit  = fs.Int("limit", 0, "how many rows")
	)
	if err := fs.Parse(args); err != nil {
		return logs.Filter{}, fmt.Errorf("%w — see: kakei l -h", err)
	}
	if fs.NArg() > 0 {
		return logs.Filter{}, fmt.Errorf("unexpected argument %q — the trail is narrowed with flags", fs.Arg(0))
	}

	if *id != 0 && *entity == "" {
		return logs.Filter{}, errors.New("--id needs --entity — an id is only an id of something")
	}
	for _, check := range []struct {
		flag, given string
		allowed     []string
	}{
		{"--entity", *entity, logEntities},
		{"--action", *action, []string{"created", "edited", "deleted"}},
		{"--source", *source, []string{logs.User, logs.System, logs.AI}},
	} {
		if check.given == "" {
			continue
		}
		found := false
		for _, a := range check.allowed {
			found = found || check.given == a
		}
		if !found {
			return logs.Filter{}, fmt.Errorf("%s %q is not one of: %s",
				check.flag, check.given, strings.Join(check.allowed, ", "))
		}
	}
	for _, d := range []struct {
		flag string
		v    *string
	}{{"--from", from}, {"--to", to}} {
		if *d.v == "" {
			continue
		}
		parsed, err := transactions.ParseDate(*d.v)
		if err != nil {
			return logs.Filter{}, fmt.Errorf("%s: %w", d.flag, err)
		}
		*d.v = parsed
	}

	return logs.Filter{
		Source: *source, Action: *action, Entity: *entity, EntityID: *id,
		From: *from, To: *to, Limit: *limit,
	}, nil
}
