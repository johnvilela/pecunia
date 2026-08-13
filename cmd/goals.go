package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"kakei/internal/core"
	"kakei/internal/goals"
)

const goalsHelp = `Track goals.

Usage:
  kakei goals [command] [ID]
  kakei g     [command] [ID]

Commands:
  (none)          list your goals
  new     | n     create a goal
  edit    | e  [ID]  edit a goal
  delete  | d  [ID]  delete a goal
  ID              show one goal in detail

A goal holds no money of its own: what it is at is the sum of the transactions
linked to it, worked out every time it is read. A saving goal climbs on money
in and falls on money out; a goal for paying something down climbs the other
way, so both read as progress toward their target.

A transaction names its goal from its own form, and only a goal in the same
currency is offered. Goals are referenced by id — they have no code. Leaving
[ID] out opens a picker. Add -h to any command for its own help.
`

var goalSubHelp = map[string]string{
	"new": `Create a goal.

Usage:
  kakei goals new
  kakei g n

Opens a form: name, description (optional), kind, currency and target. The
target is read at the currency you pick, so a Bitcoin goal takes its eight
decimal places.

Nothing is filed against the goal here. A transaction names the goal it feeds
from its own form: kakei t n.
`,
	"edit": `Edit a goal.

Usage:
  kakei goals edit [ID]
  kakei g e [ID]

Opens the create form pre-filled. Without ID, pick from a list first.

The currency can only be changed while nothing is linked: the transactions
already filed were filed in the currency they were filed in, and reading their
total as another one would change what the goal is at without a row having
moved. Everything else — including the kind, which flips what counts as
progress — can be changed at any time.
`,
	"delete": `Delete a goal for good.

Usage:
  kakei goals delete [ID]
  kakei g d [ID]

Asks for confirmation. Without ID, pick from a list first. Nothing blocks the
delete: the transactions that fed the goal keep their money and lose the link.
`,
}

var errNoGoals = errors.New("no goals yet — create one with: kakei g n")

func runGoals(args []string) error {
	if len(args) == 0 {
		return listGoals()
	}
	sub, rest := args[0], args[1:]
	if isHelpFlag(sub) {
		fmt.Fprint(out, goalsHelp)
		return nil
	}

	name := map[string]string{
		"new": "new", "n": "new",
		"edit": "edit", "e": "edit",
		"delete": "delete", "d": "delete",
	}[sub]

	if name == "" {
		// Anything else is an id.
		return withGoals(func(s *goals.Store) error {
			g, err := resolveOrPickGoal(s, args, "Goal details")
			if err != nil {
				return err
			}
			fmt.Fprint(out, goals.Details(g))
			return nil
		})
	}
	if len(rest) > 0 && isHelpFlag(rest[0]) {
		fmt.Fprint(out, goalSubHelp[name])
		return nil
	}

	return withGoals(func(s *goals.Store) error {
		switch name {
		case "new":
			return createGoal(s)
		case "edit":
			return editGoal(s, rest)
		default:
			return deleteGoal(s, rest)
		}
	})
}

func withGoals(fn func(*goals.Store) error) error {
	return withConn(func(conn *sql.DB) error { return fn(goals.NewStore(conn)) })
}

// resolveGoal turns a reference into a goal. A goal has no code, so anything
// that is not a number is a mistake worth naming rather than a lookup to try.
func resolveGoal(s *goals.Store, ref string) (goals.Goal, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(ref), 10, 64)
	if err != nil {
		return goals.Goal{}, fmt.Errorf("no goal matching %q — goals are referenced by id", ref)
	}
	g, err := s.Get(id)
	if errors.Is(err, goals.ErrNotFound) {
		return g, fmt.Errorf("no goal matching %q", ref)
	}
	return g, err
}

// resolveOrPickGoal turns an optional ID argument into a goal, falling back to
// the picker when none was given.
func resolveOrPickGoal(s *goals.Store, args []string, title string) (goals.Goal, error) {
	if len(args) > 0 && args[0] != "" {
		return resolveGoal(s, args[0])
	}
	all, err := s.List()
	if err != nil {
		return goals.Goal{}, err
	}
	if len(all) == 0 {
		return goals.Goal{}, errNoGoals
	}
	return goals.Pick(all, title)
}

func listGoals() error {
	return withGoals(func(s *goals.Store) error {
		all, err := s.List()
		if err != nil {
			return err
		}
		if len(all) == 0 {
			fmt.Fprintln(out, errNoGoals)
			return nil
		}
		fmt.Fprintln(out, goals.Table(all))
		return nil
	})
}

func createGoal(s *goals.Store) error {
	g := goals.Goal{Kind: goals.KindSaving, Currency: core.Currencies[0].Code}
	// Nothing can be linked to a goal that does not exist yet, so the currency
	// is always on offer here.
	if err := goals.Form(&g, "New goal", 0); err != nil {
		return err
	}
	if err := s.Create(&g); err != nil {
		return err
	}
	fmt.Fprintf(out, "created goal %s (%s)\n", g.Name, g.Fmt(g.Target))
	return nil
}

func editGoal(s *goals.Store, args []string) error {
	g, err := resolveOrPickGoal(s, args, "Edit which goal?")
	if err != nil {
		return err
	}
	linked, err := s.Linked(g.ID)
	if err != nil {
		return err
	}
	if err := goals.Form(&g, "Edit goal", linked); err != nil {
		return err
	}
	if err := s.Update(g); err != nil {
		return err
	}
	fmt.Fprintf(out, "updated goal %s (%s)\n", g.Name, g.Fmt(g.Target))
	return nil
}

func deleteGoal(s *goals.Store, args []string) error {
	g, err := resolveOrPickGoal(s, args, "Delete which goal?")
	if err != nil {
		return err
	}
	linked, err := s.Linked(g.ID)
	if err != nil {
		return err
	}
	// Nothing refuses this delete, unlike an account or a card, so the confirm
	// is the only place the cost of it can be said.
	note := "This cannot be undone."
	if linked > 0 {
		note += fmt.Sprintf(" %d transaction(s) keep their money and lose the link.", linked)
	}
	ok, err := core.Confirm(fmt.Sprintf("Delete %s (%s)?", g.Name, g.Fmt(g.Target)), note)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "cancelled")
		return nil
	}
	if err := s.Delete(g.ID); err != nil {
		return err
	}
	fmt.Fprintf(out, "deleted goal %s (%s)\n", g.Name, g.Fmt(g.Target))
	return nil
}
