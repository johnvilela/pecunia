package main

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"kakei/internal/budgets"
	"kakei/internal/categories"
	"kakei/internal/core"
	"kakei/internal/transactions"
)

const budgetsHelp = `Budgets — what a category is allowed to cost each month.

Usage:
  kakei budget [CODE] [command]
  kakei bg     [CODE] [command]

Commands:
  (none)              every budget and where this month stands
  new       | n       create a budget
  CODE                show one budget, its pace and the months before it
  CODE edit | e       edit a budget
  CODE delete | d     delete a budget
  CODE archive        stop tracking a budget, keeping its history
  CODE unarchive      bring an archived budget back

Flags:
  --month YYYY-MM     the month to read (default this one)
  --all               the archived budgets too

The code may come on either side of the command: "kakei bg FOOD1 edit" and
"kakei bg edit FOOD1" are the same thing. Leaving CODE out opens a picker.
Add -h to any command for its own help.

A budget holds no money and nothing is linked to it. What it is at is the sum
of the transactions filed under its category in that month, worked out every
time it is read — so a budget can never disagree with the ledger, and recording
a transaction never has to think about one.

Going over is shown, not refused: a budget is a promise to yourself, and a
ledger that turns real purchases away stops being a ledger. What it does say is
whether the month is running ahead of its own pace, which is the part there is
still time to do something about.
`

var budgetSubHelp = map[string]string{
	"new": `Create a budget.

Usage:
  kakei budget new
  kakei bg n

Opens a form: name, description (optional), code, category, colour, currency
and the monthly cap. The cap is read at the currency you pick, so a Bitcoin
budget takes its eight decimal places.

One category is capped once per currency. A category already capped in reais
can still be capped in satoshis — those are two disjoint sets of transactions —
but a second budget over the same pair would count the same spend twice.

Nothing is filed against the budget here, and nothing ever is: a transaction is
counted because of the category it already names.
`,
	"edit": `Edit a budget.

Usage:
  kakei budget FOOD1 edit
  kakei bg e FOOD1

Opens the create form pre-filled. Without CODE, pick from a list first.

Changing the cap is logged: the form asks why, and "kakei bg CODE" shows every
move it has made with the day it happened. A cap is a promise about the future
and the future moves — rice goes up and the food budget follows — and what it
used to say is worth keeping. The reason is optional, and is only kept when the
cap really changed.

The currency can only be changed while nothing has been counted in the old one.
Those transactions were filed in the currency they were filed in, and reading
their total as another one would change what the budget is at without a row
having moved.
`,
	"delete": `Delete a budget for good.

Usage:
  kakei budget FOOD1 delete
  kakei bg d FOOD1

Asks for confirmation. Without CODE, pick from a list first. Nothing is blocked
and nothing else goes: no transaction was ever linked to the budget, and the
category it capped outlives it. Only the cap and its history are lost.

To stop tracking a budget without losing what it has been, archive it instead.
`,
	"archive": `Archive a budget, or bring one back.

Usage:
  kakei budget FOOD1 archive
  kakei budget FOOD1 unarchive

An archived budget drops out of the list and off the summary, and stops being
judged against the month — it is not being tracked, so it is neither on track
nor over. Everything it has been stays readable, and "--all" brings it back
into view.
`,
}

var errNoBudgets = errors.New("no budgets yet — create one with: kakei bg n")

// budgetVerbs is every name a subcommand answers to. It is also what tells a
// verb from a code, since either may come first.
var budgetVerbs = map[string]string{
	"new": "new", "n": "new",
	"edit": "edit", "e": "edit",
	"delete": "delete", "d": "delete",
	"archive": "archive", "unarchive": "unarchive",
}

// budgetFlags is the flags pulled out before the code and the verb are read.
// They are taken out first because either of those may come first, and a
// positional parse that also had to step over --month would have to know how
// many words each flag eats in three places instead of one.
type budgetFlags struct {
	cycle    string
	archived bool
	rest     []string
}

func parseBudgetFlags(args []string) (budgetFlags, error) {
	f := budgetFlags{cycle: budgets.ThisCycle(time.Now())}
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--all":
			f.archived = true
		case arg == "--month":
			if i+1 >= len(args) {
				return f, errors.New("--month: a month is YYYY-MM, like " + f.cycle)
			}
			i++
			cycle, err := budgets.ParseCycle(args[i])
			if err != nil {
				return f, fmt.Errorf("--month: %w", err)
			}
			f.cycle = cycle
		case len(arg) > 8 && arg[:8] == "--month=":
			cycle, err := budgets.ParseCycle(arg[8:])
			if err != nil {
				return f, fmt.Errorf("--month: %w", err)
			}
			f.cycle = cycle
		default:
			f.rest = append(f.rest, arg)
		}
	}
	return f, nil
}

func runBudgets(args []string) error {
	if len(args) > 0 && isHelpFlag(args[0]) {
		fmt.Fprint(out, budgetsHelp)
		return nil
	}

	f, err := parseBudgetFlags(args)
	if err != nil {
		return err
	}
	if len(f.rest) == 0 {
		return listBudgets(f)
	}

	// The code may come on either side of the verb: "kakei bg FOOD1 edit" is how
	// it is said out loud, "kakei bg edit FOOD1" is how every other module reads,
	// and neither is worth refusing.
	verb, ref := budgetVerbs[f.rest[0]], ""
	rest := f.rest[1:]
	if verb == "" {
		ref = f.rest[0]
		if len(rest) > 0 {
			verb = budgetVerbs[rest[0]]
			if verb == "" && !isHelpFlag(rest[0]) {
				return fmt.Errorf("kakei budget: unknown command %q", rest[0])
			}
			rest = rest[1:]
		}
	} else if len(rest) > 0 && !isHelpFlag(rest[0]) {
		ref, rest = rest[0], rest[1:]
	}

	if len(rest) > 0 && isHelpFlag(rest[0]) {
		name := verb
		if name == "unarchive" {
			name = "archive"
		}
		fmt.Fprint(out, budgetSubHelp[name])
		return nil
	}

	return withConn(func(conn *sql.DB) error {
		s := budgets.NewStore(conn)
		switch verb {
		case "new":
			return createBudget(conn, s, f.cycle)
		case "edit":
			return editBudget(conn, s, ref, f.cycle)
		case "delete":
			return deleteBudget(s, ref, f.cycle)
		case "archive", "unarchive":
			return archiveBudget(s, ref, f.cycle, verb == "archive")
		default:
			return showBudget(s, ref, f.cycle)
		}
	})
}

func withBudgets(fn func(*budgets.Store) error) error {
	return withConn(func(conn *sql.DB) error { return fn(budgets.NewStore(conn)) })
}

// resolveOrPickBudget turns an optional {CODE|ID} into a budget, falling back to
// the picker when none was given.
func resolveOrPickBudget(s *budgets.Store, ref, cycle, title string) (budgets.Budget, error) {
	if ref != "" {
		b, err := s.Resolve(ref, cycle)
		if errors.Is(err, budgets.ErrNotFound) {
			return b, fmt.Errorf("no budget matching %q", ref)
		}
		return b, err
	}
	// Archived budgets are on offer here: editing or looking at one is exactly
	// what archiving leaves you able to do.
	all, err := s.List(cycle, true)
	if err != nil {
		return budgets.Budget{}, err
	}
	if len(all) == 0 {
		return budgets.Budget{}, errNoBudgets
	}
	return budgets.Pick(all, title)
}

// monthTitle is the month a screen is for, said the way the summary says it.
func monthTitle(cycle string) string {
	d, err := time.Parse(budgets.CycleLayout, cycle)
	if err != nil {
		return cycle
	}
	return d.Format("January 2006")
}

func listBudgets(f budgetFlags) error {
	return withBudgets(func(s *budgets.Store) error {
		all, err := s.List(f.cycle, f.archived)
		if err != nil {
			return err
		}
		if len(all) == 0 {
			// An empty month and an empty database are different news, but a
			// budget is not per-month — there are none either way.
			fmt.Fprintln(out, errNoBudgets)
			return nil
		}
		fmt.Fprintln(out, core.HeaderStyle.Render(monthTitle(f.cycle)))
		fmt.Fprintln(out, budgets.Table(all, time.Now()))
		return nil
	})
}

func showBudget(s *budgets.Store, ref, cycle string) error {
	b, err := resolveOrPickBudget(s, ref, cycle, "Budget details")
	if err != nil {
		return err
	}
	log, err := s.AmountLog(b.ID)
	if err != nil {
		return err
	}
	// Six months is enough to tell a bad month from a wrong cap, and short
	// enough to read at a glance.
	history, err := s.History(b, 6)
	if err != nil {
		return err
	}
	fmt.Fprint(out, budgets.Details(b, log, history, time.Now()))
	return nil
}

// budgetFormData is the categories the form offers. Only the ones that exist
// can be capped, and a database with none is a database that cannot have a
// budget yet.
func budgetFormData(conn *sql.DB) (budgets.FormData, error) {
	cats, err := categories.NewStore(conn).List()
	if err != nil {
		return budgets.FormData{}, err
	}
	d := budgets.FormData{Categories: make([]transactions.Ref, len(cats))}
	for i, c := range cats {
		d.Categories[i] = transactions.Ref{ID: c.ID, Code: c.Code, Name: c.Name, Color: c.Color}
	}
	return d, nil
}

func createBudget(conn *sql.DB, s *budgets.Store, cycle string) error {
	d, err := budgetFormData(conn)
	if err != nil {
		return err
	}
	if len(d.Categories) == 0 {
		return errors.New("no categories yet — a budget caps one, so make it first with: kakei ct n")
	}

	code, err := s.SuggestCode()
	if err != nil {
		return err
	}
	b := budgets.Budget{
		Code: code, Color: core.Palette[0].Name, Currency: core.Currencies[0].Code,
		Category: d.Categories[0], Cycle: cycle, Active: true,
	}
	// Nothing can have been counted by a budget that does not exist yet, so the
	// currency is always on offer here.
	if _, err := budgets.Form(d, &b, "New budget", 0); err != nil {
		return err
	}
	if err := s.Create(&b); err != nil {
		return err
	}
	fmt.Fprintf(out, "created budget %s %s (%s a month)\n", b.Code, b.Name, b.Fmt(b.Amount))
	return nil
}

func editBudget(conn *sql.DB, s *budgets.Store, ref, cycle string) error {
	b, err := resolveOrPickBudget(s, ref, cycle, "Edit which budget?")
	if err != nil {
		return err
	}
	d, err := budgetFormData(conn)
	if err != nil {
		return err
	}
	counted, err := s.Counted(b.Category.ID, b.Currency)
	if err != nil {
		return err
	}
	note, err := budgets.Form(d, &b, "Edit budget", counted)
	if err != nil {
		return err
	}
	if err := s.Update(b, note); err != nil {
		return err
	}
	fmt.Fprintf(out, "updated budget %s %s (%s a month)\n", b.Code, b.Name, b.Fmt(b.Amount))
	return nil
}

func deleteBudget(s *budgets.Store, ref, cycle string) error {
	b, err := resolveOrPickBudget(s, ref, cycle, "Delete which budget?")
	if err != nil {
		return err
	}
	// Nothing is linked to a budget, so unlike a goal there is nothing to count
	// here — the honest note is that the spending itself is untouched.
	ok, err := core.Confirm(
		fmt.Sprintf("Delete budget %s (%s a month)?", b.Name, b.Fmt(b.Amount)),
		"This cannot be undone. The transactions and the category stay exactly as they are — "+
			"only the cap and its history go. To stop tracking it instead, archive it.")
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
	fmt.Fprintf(out, "deleted budget %s %s\n", b.Code, b.Name)
	return nil
}

func archiveBudget(s *budgets.Store, ref, cycle string, archive bool) error {
	title := "Archive which budget?"
	if !archive {
		title = "Bring which budget back?"
	}
	b, err := resolveOrPickBudget(s, ref, cycle, title)
	if err != nil {
		return err
	}
	if err := s.SetActive(b.ID, !archive); err != nil {
		return err
	}
	if archive {
		fmt.Fprintf(out, "archived budget %s %s — its history stays, and --all still shows it\n",
			b.Code, b.Name)
		return nil
	}
	fmt.Fprintf(out, "budget %s %s is back\n", b.Code, b.Name)
	return nil
}
