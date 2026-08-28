package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"pecunia/internal/accounts"
	"pecunia/internal/cards"
	"pecunia/internal/categories"
	"pecunia/internal/core"
	"pecunia/internal/goals"
	"pecunia/internal/transactions"
)

const transactionsHelp = `Record and review transactions.

Usage:
  pecunia transactions [command] [ID]
  pecunia t            [command] [ID]

Commands:
  (none)              list this month
  new      | n        record a transaction
  transfer | tr       move money between two of your accounts
  edit    | e  [ID]   edit a transaction
  delete  | d  [ID]   delete a transaction
  ID                  show one transaction in detail

Filters (all combine, and any of them replaces the this-month default):
  --all               every transaction ever
  --transfers         only money moved between your own accounts
  --date   DATE       one day, YYYY-MM-DD
  --month  YYYY-MM    one month
  --from   DATE       on or after DATE
  --to     DATE       on or before DATE
  --tag    TAG        transactions carrying TAG
  --search TEXT       TEXT anywhere in the title or the description
  --category CODE|ID  transactions filed under a category
  --account  CODE|ID  transactions through an account
  --card     CODE|ID  transactions through a credit card
  --goal     ID       transactions feeding a goal

A transaction moves the balance of the account or credit card it names, and
editing or deleting it moves that balance back. Leaving [ID] out opens a
picker. Add -h to any command for its own help.
`

var transactionSubHelp = map[string]string{
	"new": `Record a transaction.

Usage:
  pecunia transactions new
  pecunia t n

Opens a form: title, description (optional), date, kind, account or credit
card, amount, category (optional), goal (optional), installments and tags. The
amount is typed without a sign — income and outcome is what the kind says — and
is read at the currency of whatever it is filed against.

Only goals counting the same currency as the chosen account or card are
offered: a goal adds up one currency and there is no rate anywhere in pecunia to
turn satoshis into centavos.

Spending from an account lowers its balance; spending on a credit card raises
what the card owes. A card that declines at its limit refuses a transaction
that would push it past.

Installments split a credit card purchase over that many bills: one row per
bill, dated a month apart from the date given, with the amount divided between
them and any odd cents on the first. The whole purchase hits the card's limit
at once, the way a real issuer takes it. 1 is an ordinary charge, and an
account purchase cannot be split — it has no bills to spread over.
`,
	"transfer": `Move money between two accounts you own.

Usage:
  pecunia transactions transfer
  pecunia t tr

Opens a form: title, description (optional), the account it leaves, the one it
arrives in, both amounts, the date, an optional goal and tags.

A transfer is not income and it is not an expense — nothing was earned and
nothing was consumed — so it counts toward neither total on the summary and
toward no budget. It carries no category for the same reason: a category that
never counts is a lie.

Both amounts are asked for, and they need not match. Different currencies is
the obvious case: R$500.00 leaves and $92.00 arrives, and the rate is used once
and stored nowhere. The same currency with the two differing is a fee, which is
what a TED costs.

It is stored as two rows, one on each account, so each side's balance moves by
its own amount and each row says where the money came from or went. Editing or
deleting either one reaches both — half a transfer is money vanishing.

The arriving leg may name a goal: money arriving somewhere is what counts
toward one, and only goals in that account's currency are offered.
`,
	"edit": `Edit a transaction.

Usage:
  pecunia transactions edit [ID]
  pecunia t e [ID]

Opens the create form pre-filled. Without ID, pick from a list first. The
balance the old transaction moved is put back before the new one is applied,
so changing the amount, flipping the kind or moving it to another account
leaves every balance right.

One installment of a series asks first whether the edit is for that one, for
it and the ones after it, or for the whole series. A wider scope carries the
title, description, category, goal, tags and kind across; each installment
keeps its own date and amount, since each falls on its own bill. To change what a series
is worth, delete it and record it again.
`,
	"delete": `Delete a transaction for good.

Usage:
  pecunia transactions delete [ID]
  pecunia t d [ID]

Asks for confirmation, then gives the account or credit card back what the
transaction took. Without ID, pick from a list first.

One installment of a series asks first whether to remove that one, it and the
ones after it, or the whole series. Deleting a bill payment gives the account
its money back and the card its debt.
`,
}

var errNoTransactions = errors.New("no transactions yet — create one with: pecunia t n")

func runTransactions(args []string) error {
	if len(args) == 0 {
		return listTransactions(nil)
	}
	sub, rest := args[0], args[1:]
	if isHelpFlag(sub) {
		fmt.Fprint(out, transactionsHelp)
		return nil
	}

	name := map[string]string{
		"new": "new", "n": "new",
		"transfer": "transfer", "tr": "transfer",
		"edit": "edit", "e": "edit",
		"delete": "delete", "d": "delete",
	}[sub]

	if name == "" {
		// Anything starting with a dash is a filter; anything else is an id.
		if strings.HasPrefix(sub, "-") {
			return listTransactions(args)
		}
		return withTransactions(func(s *transactions.Store) error {
			t, err := resolveTransaction(s, sub)
			if err != nil {
				return err
			}
			fmt.Fprint(out, transactions.Details(t))
			return nil
		})
	}
	if len(rest) > 0 && isHelpFlag(rest[0]) {
		fmt.Fprint(out, transactionSubHelp[name])
		return nil
	}

	switch name {
	case "new":
		return createTransaction()
	case "transfer":
		return createTransfer()
	case "edit":
		return editTransaction(rest)
	default:
		return deleteTransaction(rest)
	}
}

func withTransactions(fn func(*transactions.Store) error) error {
	return withConn(func(conn *sql.DB) error { return fn(transactions.NewStore(conn)) })
}

// resolveTransaction turns an argument into a transaction. Unlike every other
// module there is no code to fall back on, so anything that is not a number is
// simply not a reference.
func resolveTransaction(s *transactions.Store, ref string) (transactions.Transaction, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(ref), 10, 64)
	if err != nil {
		return transactions.Transaction{}, fmt.Errorf("no transaction matching %q — transactions are referenced by id", ref)
	}
	t, err := s.Get(id)
	if errors.Is(err, transactions.ErrNotFound) {
		return t, fmt.Errorf("no transaction matching %q", ref)
	}
	return t, err
}

// resolveOrPickTransaction falls back to the picker when no id was given.
func resolveOrPickTransaction(s *transactions.Store, args []string, title string) (transactions.Transaction, error) {
	if len(args) > 0 && args[0] != "" {
		return resolveTransaction(s, args[0])
	}
	all, err := s.List(transactions.Filter{})
	if err != nil {
		return transactions.Transaction{}, err
	}
	if len(all) == 0 {
		return transactions.Transaction{}, errNoTransactions
	}
	return transactions.Pick(all, title)
}

// listFilter is the parsed form of the list flags: the filter itself, plus
// whether anything was asked for at all, which is what decides between the
// this-month default and showing everything.
type listFilter struct {
	filter   transactions.Filter
	narrowed bool
}

// parseListFlags turns the command line into a filter, resolving every
// {CODE|ID} through its own module so `--category food1` works the way
// `pecunia ct food1` does.
func parseListFlags(conn *sql.DB, args []string) (listFilter, error) {
	fs := flag.NewFlagSet("transactions", flag.ContinueOnError)
	// The flag package's own usage dump would print the flags a second time, in
	// its own single-dash spelling, right next to the error report() already
	// prints. pecunia t -h is the one that documents them.
	fs.SetOutput(io.Discard)
	var (
		all       = fs.Bool("all", false, "every transaction ever")
		transfers = fs.Bool("transfers", false, "only money moved between your own accounts")
		date      = fs.String("date", "", "one day, YYYY-MM-DD")
		month     = fs.String("month", "", "one month, YYYY-MM")
		from      = fs.String("from", "", "on or after this date")
		to        = fs.String("to", "", "on or before this date")
		tag       = fs.String("tag", "", "transactions carrying this tag")
		search    = fs.String("search", "", "text in the title or the description")
		category  = fs.String("category", "", "category CODE or ID")
		account   = fs.String("account", "", "account CODE or ID")
		card      = fs.String("card", "", "credit card CODE or ID")
		goal      = fs.String("goal", "", "goal ID")
	)
	if err := fs.Parse(args); err != nil {
		return listFilter{}, fmt.Errorf("%w — see: pecunia t -h", err)
	}
	if n := fs.NArg(); n > 0 {
		return listFilter{}, fmt.Errorf("unexpected argument %q — filters are flags, and an id takes none", fs.Arg(0))
	}

	out := listFilter{filter: transactions.Filter{Tag: *tag, Search: *search, Transfers: *transfers}}
	// A transfer is not a monthly habit — narrowing to them without widening the
	// window would show only the ones that happen to fall in this month.
	out.narrowed = *all || *tag != "" || *search != "" || *transfers

	for _, d := range []struct {
		flag string
		v    *string
	}{{"--date", date}, {"--from", from}, {"--to", to}} {
		if *d.v == "" {
			continue
		}
		parsed, err := transactions.ParseDate(*d.v)
		if err != nil {
			return listFilter{}, fmt.Errorf("%s: %w", d.flag, err)
		}
		*d.v = parsed
		out.narrowed = true
	}
	if *date != "" {
		out.filter.From, out.filter.To = *date, *date
	}
	if *month != "" {
		start, end, err := monthRange(*month)
		if err != nil {
			return listFilter{}, err
		}
		out.filter.From, out.filter.To = start, end
		out.narrowed = true
	}
	if *from != "" {
		out.filter.From = *from
	}
	if *to != "" {
		out.filter.To = *to
	}

	if *category != "" {
		c, err := categories.NewStore(conn).Resolve(*category)
		if err != nil {
			return listFilter{}, fmt.Errorf("no category matching %q", *category)
		}
		out.filter.CategoryID, out.narrowed = c.ID, true
	}
	if *account != "" {
		a, err := accounts.NewStore(conn).Resolve(*account)
		if err != nil {
			return listFilter{}, fmt.Errorf("no account matching %q", *account)
		}
		out.filter.AccountID, out.narrowed = a.ID, true
	}
	if *card != "" {
		c, err := cards.NewStore(conn).Resolve(*card)
		if err != nil {
			return listFilter{}, fmt.Errorf("no credit card matching %q", *card)
		}
		out.filter.CardID, out.narrowed = c.ID, true
	}
	if *goal != "" {
		// narrowed matters here more than anywhere: a goal's transactions span
		// months, and the this-month default would hide most of them.
		g, err := resolveGoal(goals.NewStore(conn), *goal)
		if err != nil {
			return listFilter{}, err
		}
		out.filter.GoalID, out.narrowed = g.ID, true
	}
	return out, nil
}

// monthRange turns YYYY-MM into the first and last day of that month. Day 0 of
// the next month is the last day of this one, which is how the stdlib spells
// "how long is February".
func monthRange(s string) (string, string, error) {
	m, err := time.Parse("2006-01", strings.TrimSpace(s))
	if err != nil {
		return "", "", fmt.Errorf("--month: a month is YYYY-MM, like %s", time.Now().Format("2006-01"))
	}
	last := time.Date(m.Year(), m.Month()+1, 0, 0, 0, 0, 0, time.UTC)
	return m.Format(transactions.DateLayout), last.Format(transactions.DateLayout), nil
}

func listTransactions(args []string) error {
	return withConn(func(conn *sql.DB) error {
		lf, err := parseListFlags(conn, args)
		if err != nil {
			return err
		}
		s := transactions.NewStore(conn)

		// Without a filter the list is this month. Years of history scrolling
		// past is not a list, and the footer is what keeps the scope from being
		// a secret.
		now := time.Now()
		scope := ""
		if !lf.narrowed {
			start, end, err := monthRange(now.Format("2006-01"))
			if err != nil {
				return err
			}
			lf.filter.From, lf.filter.To = start, end
			scope = now.Format("January 2006")
		}

		found, err := s.List(lf.filter)
		if err != nil {
			return err
		}
		if len(found) == 0 {
			// An empty month and an empty ledger are different news.
			total, err := s.List(transactions.Filter{})
			if err != nil {
				return err
			}
			switch {
			case len(total) == 0:
				fmt.Fprintln(out, errNoTransactions)
			case scope != "":
				fmt.Fprintf(out, "nothing in %s — widen with: pecunia t --all\n", scope)
			default:
				fmt.Fprintln(out, "nothing matched that filter — widen with: pecunia t --all")
			}
			return nil
		}

		fmt.Fprintln(out, transactions.Table(found))
		if scope != "" {
			fmt.Fprintf(out, "%s — %d transaction(s). Widen with: pecunia t --all, or pecunia t --month %s\n",
				scope, len(found), now.AddDate(0, -1, 0).Format("2006-01"))
		}
		return nil
	})
}

// formData gathers everything the form offers to choose from.
func formData(conn *sql.DB) (transactions.FormData, error) {
	var d transactions.FormData
	var err error
	if d.Accounts, err = accounts.NewStore(conn).List(); err != nil {
		return d, err
	}
	if d.Cards, err = cards.NewStore(conn).List(); err != nil {
		return d, err
	}
	if d.Categories, err = categories.NewStore(conn).List(); err != nil {
		return d, err
	}
	if d.Goals, err = goals.NewStore(conn).List(); err != nil {
		return d, err
	}
	d.Tags, err = transactions.NewStore(conn).AllTags()
	return d, err
}

func createTransaction() error {
	return withConn(func(conn *sql.DB) error {
		d, err := formData(conn)
		if err != nil {
			return err
		}
		t := transactions.Transaction{Kind: transactions.KindOutcome, Date: transactions.Today()}
		if err := transactions.Form(d, &t, "New transaction"); err != nil {
			return err
		}
		s := transactions.NewStore(conn)
		if err := s.Create(&t); err != nil {
			return err
		}
		saved, err := s.Get(t.ID)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "recorded #%d %s (%s)\n", saved.ID, saved.Title, transactions.Amount(saved))
		return nil
	})
}

func editTransaction(args []string) error {
	return withConn(func(conn *sql.DB) error {
		s := transactions.NewStore(conn)
		t, err := resolveOrPickTransaction(s, args, "Edit which transaction?")
		if err != nil {
			return err
		}
		// A transfer is edited whole, from either leg: almost nothing on the
		// ordinary form applies to one, and half an edit is half a transfer.
		if t.IsTransfer() {
			return editTransfer(conn, s, t.TransferGroup)
		}
		if t.Kind == transactions.KindAdjustment {
			return fmt.Errorf("#%d is a balance adjustment — it is not edited; delete it (pecunia t d %d) and the balance reverts", t.ID, t.ID)
		}
		scope, err := transactions.AskScope(t, "Edit")
		if err != nil {
			return err
		}
		d, err := formData(conn)
		if err != nil {
			return err
		}
		if err := transactions.Form(d, &t, "Edit transaction"); err != nil {
			return err
		}
		if err := s.Update(t, scope); err != nil {
			return err
		}
		saved, err := s.Get(t.ID)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "updated #%d %s (%s)\n", saved.ID, saved.Title, transactions.Amount(saved))
		return nil
	})
}

func deleteTransaction(args []string) error {
	return withTransactions(func(s *transactions.Store) error {
		t, err := resolveOrPickTransaction(s, args, "Delete which transaction?")
		if err != nil {
			return err
		}
		scope, err := transactions.AskScope(t, "Delete")
		if err != nil {
			return err
		}
		// Either leg takes the other with it, so the confirmation says so rather
		// than leaving the second row a surprise.
		note := "This cannot be undone. " + t.Target().Code + " gets the money back."
		if t.IsTransfer() {
			note = fmt.Sprintf("This cannot be undone. Both legs go: %s and %s go back to what they were.",
				t.Target().Code, t.Counterpart.Ref.Code)
		}
		ok, err := core.Confirm(
			fmt.Sprintf("Delete #%d %s (%s)?", t.ID, t.Title, transactions.Amount(t)),
			note, "Yes, delete")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(out, "cancelled")
			return nil
		}
		if err := s.Delete(t.ID, scope); err != nil {
			return err
		}
		fmt.Fprintf(out, "deleted #%d %s\n", t.ID, t.Title)
		return nil
	})
}

// createTransfer records money moving between two of your own accounts. It is
// its own command rather than a shape of `new` because almost nothing on the
// ordinary form applies: there is no category, no card, no installments, and
// two amounts instead of one.
func createTransfer() error {
	return withConn(func(conn *sql.DB) error {
		d, err := formData(conn)
		if err != nil {
			return err
		}
		t := transactions.Transfer{Date: transactions.Today()}
		if err := transactions.TransferForm(d, &t, "New transfer"); err != nil {
			return err
		}
		s := transactions.NewStore(conn)
		if err := s.Transfer(&t); err != nil {
			return err
		}
		saved, err := s.Get(t.Group)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "moved %s from %s to %s\n",
			transactions.Amount(saved), saved.Account.Code, saved.Counterpart.Ref.Code)
		return nil
	})
}

// editTransfer edits both legs together, from whichever one was named.
func editTransfer(conn *sql.DB, s *transactions.Store, group int64) error {
	t, err := s.GetTransfer(group)
	if err != nil {
		return err
	}
	d, err := formData(conn)
	if err != nil {
		return err
	}
	if err := transactions.TransferForm(d, &t, "Edit transfer"); err != nil {
		return err
	}
	if err := s.UpdateTransfer(t); err != nil {
		return err
	}
	saved, err := s.Get(t.Group)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "updated: %s from %s to %s\n",
		transactions.Amount(saved), saved.Account.Code, saved.Counterpart.Ref.Code)
	return nil
}
