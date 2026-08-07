package main

import (
	"errors"
	"fmt"

	"kakei/internal/cards"
	"kakei/internal/core"
	"kakei/internal/db"
)

const cardsHelp = `Manage credit cards.

Usage:
  kakei credit-card [command] [CODE|ID]
  kakei cc          [command] [CODE|ID]

Commands:
  (none)              list your credit cards
  new     | n         create a credit card
  edit    | e  [ref]  edit a credit card
  delete  | d  [ref]  delete a credit card
  CODE|ID             show one credit card in detail

Leaving [ref] out opens a picker. Add -h to any command for its own help.
`

var cardSubHelp = map[string]string{
	"new": `Create a credit card.

Usage:
  kakei credit-card new
  kakei cc n

Opens a form: name, description (optional), code, color, currency, limit,
balance, closing day, due day. The code is 5 characters and comes pre-filled
with a free suggestion. Colors are a preset of 12. Currencies are Dollar, Euro,
Brazilian Real and Bitcoin.

Balance is what the open invoice already owes — available credit is the limit
minus it. Closing and due are days of the month (1-31) and repeat every month;
a day the month is too short for falls on its last day.
`,
	"edit": `Edit a credit card.

Usage:
  kakei credit-card edit [CODE|ID]
  kakei cc e [CODE|ID]

Opens the create form pre-filled. Without CODE|ID, pick from a list first.
`,
	"delete": `Delete a credit card for good.

Usage:
  kakei credit-card delete [CODE|ID]
  kakei cc d [CODE|ID]

Asks for confirmation. Without CODE|ID, pick from a list first.
`,
}

var errNoCards = errors.New("no credit cards yet — create one with: kakei cc n")

func runCards(args []string) error {
	if len(args) == 0 {
		return listCards()
	}
	sub, rest := args[0], args[1:]
	if isHelpFlag(sub) {
		fmt.Fprint(out, cardsHelp)
		return nil
	}

	name := map[string]string{
		"new": "new", "n": "new",
		"edit": "edit", "e": "edit",
		"delete": "delete", "d": "delete",
	}[sub]

	if name == "" {
		// Anything else is a {CODE|ID}.
		return withCards(func(s *cards.Store) error {
			c, err := resolveOrPickCard(s, args, "Credit card details")
			if err != nil {
				return err
			}
			fmt.Fprint(out, cards.Details(c))
			return nil
		})
	}
	if len(rest) > 0 && isHelpFlag(rest[0]) {
		fmt.Fprint(out, cardSubHelp[name])
		return nil
	}

	return withCards(func(s *cards.Store) error {
		switch name {
		case "new":
			return createCard(s)
		case "edit":
			return editCard(s, rest)
		default:
			return deleteCard(s, rest)
		}
	})
}

func withCards(fn func(*cards.Store) error) error {
	conn, err := db.Open()
	if err != nil {
		return err
	}
	defer conn.Close()
	return fn(cards.NewStore(conn))
}

// resolveOrPickCard turns an optional {CODE|ID} argument into a card, falling
// back to the picker when none was given.
func resolveOrPickCard(s *cards.Store, args []string, title string) (cards.Card, error) {
	if len(args) > 0 && args[0] != "" {
		c, err := s.Resolve(args[0])
		if errors.Is(err, cards.ErrNotFound) {
			return c, fmt.Errorf("no credit card matching %q", args[0])
		}
		return c, err
	}
	all, err := s.List()
	if err != nil {
		return cards.Card{}, err
	}
	if len(all) == 0 {
		return cards.Card{}, errNoCards
	}
	return cards.Pick(all, title)
}

func listCards() error {
	return withCards(func(s *cards.Store) error {
		all, err := s.List()
		if err != nil {
			return err
		}
		if len(all) == 0 {
			fmt.Fprintln(out, "no credit cards yet — create one with: kakei cc n")
			return nil
		}
		fmt.Fprintln(out, cards.Table(all))
		return nil
	})
}

func createCard(s *cards.Store) error {
	code, err := s.SuggestCode()
	if err != nil {
		return err
	}
	c := cards.Card{
		Code:       code,
		Color:      core.Palette[0].Name,
		Currency:   core.Currencies[0].Code,
		ClosingDay: 1,
		DueDay:     10,
	}
	if err := cards.Form(s, &c, "New credit card"); err != nil {
		return err
	}
	if err := s.Create(&c); err != nil {
		return err
	}
	fmt.Fprintf(out, "created credit card %s (%s)\n", c.Code, c.Name)
	return nil
}

func editCard(s *cards.Store, args []string) error {
	c, err := resolveOrPickCard(s, args, "Edit which credit card?")
	if err != nil {
		return err
	}
	if err := cards.Form(s, &c, "Edit credit card"); err != nil {
		return err
	}
	if err := s.Update(c); err != nil {
		return err
	}
	fmt.Fprintf(out, "updated credit card %s (%s)\n", c.Code, c.Name)
	return nil
}

func deleteCard(s *cards.Store, args []string) error {
	c, err := resolveOrPickCard(s, args, "Delete which credit card?")
	if err != nil {
		return err
	}
	ok, err := core.Confirm(
		fmt.Sprintf("Delete %s (%s)?", c.Code, c.Name),
		"This cannot be undone.")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "cancelled")
		return nil
	}
	if err := s.Delete(c.ID); err != nil {
		return err
	}
	fmt.Fprintf(out, "deleted credit card %s (%s)\n", c.Code, c.Name)
	return nil
}
