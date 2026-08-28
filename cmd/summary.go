package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"time"

	"pecunia/internal/summary"
	"pecunia/internal/transactions"
)

const summaryHelp = `Where you stand, on one screen.

Usage:
  pecunia summary [flags]
  pecunia s       [flags]

Flags:
  --date YYYY-MM-DD  the day to summarise (default today)
  --month            widen to the whole month that day falls in

What came in and what went out, what needs paying, what the accounts and cards
hold, and where the goals are — the same figures the other commands show, read
in one go so nothing has to be held in your head between them.

Amounts are never added across currencies: reais, dollars and satoshis are
printed side by side, because there is no exchange rate anywhere in pecunia to
make one of the other.

The two flags stack, so the month of any day is --month --date 2026-07-04. A
summary of a window that is already over leaves out what is due: nothing can be
late "today" in a month that ended.
`

func runSummary(args []string) error {
	// The flag package answers -h with its own "flag: help requested" error, so
	// the help text has to get there first.
	if len(args) > 0 && isHelpFlag(args[0]) {
		fmt.Fprint(out, summaryHelp)
		return nil
	}

	fs := flag.NewFlagSet("summary", flag.ContinueOnError)
	// Its usage dump would print the flags a second time in its own single-dash
	// spelling, right beside the error report() already prints.
	fs.SetOutput(io.Discard)
	date := fs.String("date", "", "one day, YYYY-MM-DD")
	month := fs.Bool("month", false, "the whole month that day falls in")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w — see: pecunia summary -h", err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q — summary takes flags and nothing else", fs.Arg(0))
	}

	day := transactions.Today()
	if *date != "" {
		parsed, err := transactions.ParseDate(*date)
		if err != nil {
			return fmt.Errorf("--date: %w", err)
		}
		day = parsed
	}

	period := summary.Period{From: day, To: day}
	if *month {
		start, end, err := monthRange(transactions.CycleOf(day))
		if err != nil {
			return err
		}
		period = summary.Period{From: start, To: end}
	}

	return withConn(func(conn *sql.DB) error {
		s, err := summary.Collect(conn, period, time.Now())
		if err != nil {
			return err
		}
		fmt.Fprint(out, summary.Render(s))
		return nil
	})
}
