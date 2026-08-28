package main

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"pecunia/internal/accounts"
	"pecunia/internal/cards"
	"pecunia/internal/categories"
	"pecunia/internal/core"
	"pecunia/internal/recurring"
	"pecunia/internal/transactions"
)

const recurringHelp = `Recurring bills — the ones that come round every month.

Usage:
  pecunia bill [CODE] [command]
  pecunia b    [CODE] [command]

Commands:
  (none)              the board: where every bill stands this cycle
  new       | n       create a bill
  CODE                show one bill, its last payments and what they averaged
  CODE pay  | p       record this cycle's payment
  CODE edit | e       edit a bill
  CODE delete | d     delete a bill
  CODE archive        stop a bill counting as due, keeping its history
  CODE unarchive      bring an archived bill back

Flags:
  --all               the archived bills too

The code may come on either side of the command: "pecunia bill ENERG pay" and
"pecunia bill pay ENERG" are the same thing. Leaving CODE out opens a picker.
Add -h to any command for its own help.

A bill holds no money. What it costs is the transactions filed against it, and
where a month stands is worked out from them every time it is read — so a bill
can never disagree with the ledger.
`

var recurringSubHelp = map[string]string{
	"new": `Create a recurring bill.

Usage:
  pecunia bill new
  pecunia b n

Opens a form: code, name, description (optional), colour, what pays it, the
usual amount (optional), the two days and a category.

"Available from" is the day of the month the bill can be paid; "overdue after"
is the day it is late. A day the month is too short for lands on its last, and
an overdue day *before* the available day falls in the month after — which is
the ordinary shape of a bill arriving on the 28th and due on the 5th.

The usual amount is what fills the form in when you pay, not what you will be
charged: an energy bill is a different number every month. Leave it blank for a
bill nobody has seen a number for yet.
`,
	"pay": `Pay a bill.

Usage:
  pecunia bill ENERG pay
  pecunia b ENERG p

Opens the ordinary transaction form with everything the bill already knows
filled in — title, usual amount, source, category and tags — so paying is a
matter of correcting the amount and pressing enter.

The cycle is the month the payment is *for*, and starts at the oldest one this
bill still owes. That is what lets February's bill be paid on 3 March and clear
February, instead of leaving it overdue forever and marking March paid.

The transaction it writes is an ordinary one: it moves the balance of whatever
paid it, and editing or deleting it later moves that balance back.
`,
	"edit": `Edit a recurring bill.

Usage:
  pecunia bill ENERG edit
  pecunia b e ENERG

Opens the create form pre-filled. Without CODE, pick from a list first. Nothing
that has already been paid changes: the payments are transactions of their own
and keep the amounts they were filed with.
`,
	"delete": `Delete a recurring bill for good.

Usage:
  pecunia bill ENERG delete
  pecunia b d ENERG

Asks for confirmation. Without CODE, pick from a list first. Nothing blocks the
delete: the payments made against the bill keep their money and lose the link.

To stop a bill counting as due without losing how it is grouped, archive it
instead.
`,
	"archive": `Archive a bill, or bring one back.

Usage:
  pecunia bill NFLIX archive
  pecunia bill NFLIX unarchive

An archived bill is off the board and counts as nothing due, and keeps every
payment ever made against it — which is what a cancelled subscription wants:
its history is still worth reading.
`,
}

var errNoBills = errors.New("no bills yet — create one with: pecunia bill n")

// recurringVerbs is every name a subcommand answers to. It is also what tells a
// verb from a code, since either may come first.
var recurringVerbs = map[string]string{
	"new": "new", "n": "new",
	"pay": "pay", "p": "pay",
	"edit": "edit", "e": "edit",
	"delete": "delete", "d": "delete",
	"archive": "archive", "unarchive": "unarchive",
}

func runRecurring(args []string) error {
	if len(args) == 0 {
		return listRecurring(false)
	}
	if isHelpFlag(args[0]) {
		fmt.Fprint(out, recurringHelp)
		return nil
	}
	if args[0] == "--all" {
		return listRecurring(true)
	}

	// The code may come on either side of the verb: "pecunia bill ENERG pay" is
	// how it is said out loud, "pecunia bill pay ENERG" is how every other module
	// reads, and neither is worth refusing.
	verb, ref := recurringVerbs[args[0]], ""
	rest := args[1:]
	if verb == "" {
		ref = args[0]
		if len(rest) > 0 {
			verb = recurringVerbs[rest[0]]
			if verb == "" && !isHelpFlag(rest[0]) {
				return fmt.Errorf("pecunia bill: unknown command %q", rest[0])
			}
			rest = rest[1:]
		}
	} else if len(rest) > 0 && !isHelpFlag(rest[0]) {
		ref, rest = rest[0], rest[1:]
	}

	if len(rest) > 0 && isHelpFlag(rest[0]) || (verb != "" && ref == "" && len(args) > 1 && isHelpFlag(args[1])) {
		name := verb
		if name == "unarchive" {
			name = "archive"
		}
		fmt.Fprint(out, recurringSubHelp[name])
		return nil
	}

	return withRecurring(func(s *recurring.Store) error {
		switch verb {
		case "new":
			return createRecurring(s)
		case "pay":
			return payRecurring(s, ref)
		case "edit":
			return editRecurring(s, ref)
		case "delete":
			return deleteRecurring(s, ref)
		case "archive", "unarchive":
			return archiveRecurring(s, ref, verb == "archive")
		default:
			return showRecurring(s, ref)
		}
	})
}

func withRecurring(fn func(*recurring.Store) error) error {
	return withConn(func(conn *sql.DB) error { return fn(recurring.NewStore(conn)) })
}

// resolveOrPickRecurring turns an optional CODE into a bill, falling back to the
// picker when none was given.
func resolveOrPickRecurring(s *recurring.Store, ref, title string) (recurring.Bill, error) {
	if ref != "" {
		b, err := s.ByCode(ref)
		if errors.Is(err, recurring.ErrNotFound) {
			return b, fmt.Errorf("no bill matching %q", ref)
		}
		return b, err
	}
	// Archived bills are on offer here: editing or looking at one is exactly
	// what archiving leaves you able to do.
	all, err := s.List(true)
	if err != nil {
		return recurring.Bill{}, err
	}
	if len(all) == 0 {
		return recurring.Bill{}, errNoBills
	}
	return recurring.Pick(all, title)
}

func listRecurring(archived bool) error {
	return withRecurring(func(s *recurring.Store) error {
		all, err := s.List(archived)
		if err != nil {
			return err
		}
		if len(all) == 0 {
			fmt.Fprintln(out, errNoBills)
			return nil
		}
		fmt.Fprint(out, recurring.Board(all, time.Now()))
		return nil
	})
}

func showRecurring(s *recurring.Store, ref string) error {
	b, err := resolveOrPickRecurring(s, ref, "Bill details")
	if err != nil {
		return err
	}
	paid, err := s.Payments(b.ID)
	if err != nil {
		return err
	}
	fmt.Fprint(out, recurring.Details(b, paid, time.Now()))
	return nil
}

// recurringFormData gathers everything the bill form offers to choose from.
func recurringFormData(conn *sql.DB) (recurring.FormData, error) {
	var d recurring.FormData
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
	d.Tags, err = transactions.NewStore(conn).AllTags()
	return d, err
}

func createRecurring(s *recurring.Store) error {
	return withConn(func(conn *sql.DB) error {
		d, err := recurringFormData(conn)
		if err != nil {
			return err
		}
		code, err := core.SuggestCode(s.CodeTaken)
		if err != nil {
			return err
		}
		b := recurring.Bill{Code: code, Color: core.Palette[0].Name, OpenDay: 1, DueDay: 10}
		if err := recurring.Form(d, &b, "New bill"); err != nil {
			return err
		}
		if err := s.Create(&b); err != nil {
			return err
		}
		fmt.Fprintf(out, "created bill %s (%s)\n", b.Code, b.Name)
		return nil
	})
}

func editRecurring(s *recurring.Store, ref string) error {
	b, err := resolveOrPickRecurring(s, ref, "Edit which bill?")
	if err != nil {
		return err
	}
	return withConn(func(conn *sql.DB) error {
		d, err := recurringFormData(conn)
		if err != nil {
			return err
		}
		if err := recurring.Form(d, &b, "Edit bill"); err != nil {
			return err
		}
		if err := s.Update(b); err != nil {
			return err
		}
		fmt.Fprintf(out, "updated bill %s (%s)\n", b.Code, b.Name)
		return nil
	})
}

// payRecurring opens the ordinary transaction form with everything the bill already
// knows filled in. The bill itself writes nothing: a payment is a transaction
// like any other, which is what keeps it moving the balance it came out of.
func payRecurring(s *recurring.Store, ref string) error {
	b, err := resolveOrPickRecurring(s, ref, "Pay which bill?")
	if err != nil {
		return err
	}
	occ := b.Current(time.Now())
	// A cycle already settled is not a mistake — a bill can arrive twice, or in
	// two parts — but it is worth saying before the form opens.
	if occ.Status == recurring.StatusPaid {
		fmt.Fprintf(out, "%s is already paid for %s (%s) — this will file another payment\n",
			b.Code, occ.Cycle, b.Fmt(occ.Paid))
	}

	return withConn(func(conn *sql.DB) error {
		d, err := formData(conn)
		if err != nil {
			return err
		}
		// The form only pre-selects tags it has an option for, and its options are
		// the tags already on some transaction — so a bill's own tag would be
		// dropped the first time it is paid unless it is put on offer here.
		d.Tags = transactions.NormalizeTags(append(d.Tags, b.Tags...))

		t := transactions.Transaction{
			Title:     b.Name,
			Value:     b.Expected,
			Kind:      transactions.KindOutcome,
			Date:      transactions.Today(),
			Category:  b.Category,
			Account:   b.Account,
			Card:      b.Card,
			Currency:  b.Currency,
			Tags:      b.Tags,
			Recurring: transactions.Ref{ID: b.ID},
			Cycle:     occ.Cycle,
		}
		if err := transactions.Form(d, &t, "Pay "+b.Code+" — "+b.Name); err != nil {
			return err
		}
		if err := transactions.NewStore(conn).Create(&t); err != nil {
			return err
		}
		fmt.Fprintf(out, "paid %s for %s (%s)\n", b.Code, t.Cycle, b.Fmt(t.Value))
		return nil
	})
}

func archiveRecurring(s *recurring.Store, ref string, archive bool) error {
	title := "Archive which bill?"
	if !archive {
		title = "Bring which bill back?"
	}
	b, err := resolveOrPickRecurring(s, ref, title)
	if err != nil {
		return err
	}
	if err := s.SetActive(b.ID, !archive); err != nil {
		return err
	}
	if archive {
		fmt.Fprintf(out, "archived bill %s (%s) — its payments are still there\n", b.Code, b.Name)
		return nil
	}
	fmt.Fprintf(out, "brought bill %s (%s) back\n", b.Code, b.Name)
	return nil
}

func deleteRecurring(s *recurring.Store, ref string) error {
	b, err := resolveOrPickRecurring(s, ref, "Delete which bill?")
	if err != nil {
		return err
	}
	linked, err := s.Linked(b.ID)
	if err != nil {
		return err
	}
	// Nothing refuses this delete, so the confirm is the only place the cost of
	// it can be said.
	note := "This cannot be undone. Archive it instead to keep it grouped."
	if linked > 0 {
		note += fmt.Sprintf(" %d payment(s) keep their money and lose the link.", linked)
	}
	ok, err := core.Confirm(fmt.Sprintf("Delete %s (%s)?", b.Code, b.Name), note, "Yes, delete")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "cancelled")
		return nil
	}
	if err := s.Delete(b.ID); err != nil {
		return err
	}
	fmt.Fprintf(out, "deleted bill %s (%s)\n", b.Code, b.Name)
	return nil
}
