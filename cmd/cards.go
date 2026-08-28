package main

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"pecunia/internal/accounts"
	"pecunia/internal/bills"
	"pecunia/internal/cards"
	"pecunia/internal/core"
	"pecunia/internal/transactions"
)

const cardsHelp = `Manage credit cards.

Usage:
  pecunia credit-card [command] [CODE|ID]
  pecunia cc          [command] [CODE|ID]

Commands:
  (none)              list your credit cards
  new     | n         create a credit card
  edit    | e  [ref]  edit a credit card
  delete  | d  [ref]  delete a credit card
  bill    | b  [ref] [YYYY-MM]  bills, or one of them in detail
  pay     | p  [ref]  pay a bill
  CODE|ID             show one credit card in detail

Leaving [ref] out opens a picker. Add -h to any command for its own help.
`

var cardSubHelp = map[string]string{
	"new": `Create a credit card.

Usage:
  pecunia credit-card new
  pecunia cc n

Opens a form: name, description (optional), code, color, currency, limit,
over-limit allowance, balance, closing day, due day. The code is 5 characters
and comes pre-filled with a free suggestion. Colors are a preset of 12.
Currencies are Dollar, Euro, Brazilian Real and Bitcoin.

Balance is what the open invoice already owes — available credit is the limit
minus it. A card may only carry a balance past its limit if it is marked as
usable over the limit (↑ in the list); otherwise the balance is capped there.
Closing and due are days of the month (1-31) and repeat every month; a day the
month is too short for falls on its last day.
`,
	"edit": `Edit a credit card.

Usage:
  pecunia credit-card edit [CODE|ID]
  pecunia cc e [CODE|ID]

Opens the create form pre-filled. Without CODE|ID, pick from a list first.

The balance is not on the form: after creation it moves only through the
ledger — charges, payments and nothing else.
`,
	"delete": `Delete a credit card for good.

Usage:
  pecunia credit-card delete [CODE|ID]
  pecunia cc d [CODE|ID]

Asks for confirmation. Without CODE|ID, pick from a list first.
`,
	"bill": `Show a credit card's bills.

Usage:
  pecunia credit-card bill [CODE|ID] [YYYY-MM]
  pecunia cc b [CODE|ID] [YYYY-MM]

A bill is one closing cycle: everything charged from the day after the last
closing through the closing date itself. Without CODE|ID, every card's bills.
With a YYYY-MM, that card's cycle closing in that month, in detail, with the
charges that make it up.

Bills are worked out from the card's closing day the first time anything asks
for them, so there is no closing step to run. A bill's total is a snapshot
taken when the cycle closes; if a transaction inside a closed one is edited
afterwards the detail view says what the ledger sums to now.
`,
	"pay": `Pay a credit card bill.

Usage:
  pecunia credit-card pay [CODE|ID]
  pecunia cc p [CODE|ID]

Asks which account pays, how much, and when. The amount comes pre-filled with
what is still owed — type over it to pay part of it, and the bill reads as
partial until the payments cover the total.

The payment is an ordinary outcome on the account that paid, which happens to
name the bill. That account's balance drops and the card's debt drops with it,
and because the payment is not a card transaction it never shows up as
spending on the next bill.
`,
}

var errNoCards = errors.New("no credit cards yet — create one with: pecunia cc n")

func runCards(args []string) error {
	if len(args) == 0 {
		return listCards()
	}
	sub, rest := args[0], args[1:]
	if isHelpFlag(sub) {
		fmt.Fprint(out, cardsHelp)
		return nil
	}

	// Every verb here is shorter or longer than a code, never five characters —
	// "bills" would have made a card coded BILLS unreachable.
	name := map[string]string{
		"new": "new", "n": "new",
		"edit": "edit", "e": "edit",
		"delete": "delete", "d": "delete",
		"bill": "bill", "b": "bill",
		"pay": "pay", "p": "pay",
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

	switch name {
	case "bill":
		return showBills(rest)
	case "pay":
		return payBill(rest)
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
	return withConn(func(conn *sql.DB) error { return fn(cards.NewStore(conn)) })
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
			fmt.Fprintln(out, "no credit cards yet — create one with: pecunia cc n")
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
		"This cannot be undone.", "Yes, delete")
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

// showBills drives `pecunia cc bill`: every card's bills, one card's bills, or one
// cycle in detail.
func showBills(args []string) error {
	return withConn(func(conn *sql.DB) error {
		cs := cards.NewStore(conn)
		bs := bills.NewStore(conn)

		if len(args) == 0 || args[0] == "" {
			all, err := cs.List()
			if err != nil {
				return err
			}
			if len(all) == 0 {
				fmt.Fprintln(out, errNoCards)
				return nil
			}
			var found []bills.Bill
			for _, c := range all {
				of, err := bs.List(c)
				if err != nil {
					return err
				}
				found = append(found, of...)
			}
			// Newest first across every card, the same order one card's list uses.
			slices.SortFunc(found, func(a, b bills.Bill) int {
				return strings.Compare(b.ClosesOn, a.ClosesOn)
			})
			fmt.Fprintln(out, bills.Table(found))
			return nil
		}

		c, err := resolveOrPickCard(cs, args, "Whose bills?")
		if err != nil {
			return err
		}
		if len(args) < 2 {
			found, err := bs.List(c)
			if err != nil {
				return err
			}
			fmt.Fprintln(out, bills.Table(found))
			return nil
		}
		return showBill(bs, c, args[1])
	})
}

// showBill is one cycle in detail. The month names it, because the closing day
// is the card's own business and nobody remembers whether it was the 10th.
func showBill(bs *bills.Store, c cards.Card, month string) error {
	if _, err := time.Parse("2006-01", strings.TrimSpace(month)); err != nil {
		return fmt.Errorf("a month is YYYY-MM, like %s", time.Now().Format("2006-01"))
	}
	found, err := bs.List(c)
	if err != nil {
		return err
	}
	for _, b := range found {
		if strings.HasPrefix(b.ClosesOn, strings.TrimSpace(month)) {
			charges, err := bs.Charges(b)
			if err != nil {
				return err
			}
			live, err := bs.LiveTotal(b)
			if err != nil {
				return err
			}
			fmt.Fprint(out, bills.Details(b, charges, live))
			return nil
		}
	}
	return fmt.Errorf("%s has no bill closing in %s", c.Code, month)
}

// payBill drives `pecunia cc pay`: pick the bill, then say which account settles it
// and by how much.
func payBill(args []string) error {
	return withConn(func(conn *sql.DB) error {
		cs := cards.NewStore(conn)
		c, err := resolveOrPickCard(cs, args, "Pay which card's bill?")
		if err != nil {
			return err
		}

		bs := bills.NewStore(conn)
		owing, err := bs.Unpaid(c)
		if err != nil {
			return err
		}
		if len(owing) == 0 {
			fmt.Fprintf(out, "%s has nothing to pay\n", c.Code)
			return nil
		}
		// One bill is not a choice. Oldest first, so that is the one offered.
		bill := owing[0]
		if len(owing) > 1 {
			if bill, err = bills.Pick(owing, "Pay which bill?"); err != nil {
				return err
			}
		}

		accs, err := accounts.NewStore(conn).List()
		if err != nil {
			return err
		}
		p, err := bills.PayForm(bill, accs)
		if err != nil {
			return err
		}
		if err := transactions.NewStore(conn).PayBill(bill.ID, p.AccountID, p.Value, p.Date); err != nil {
			return err
		}

		after, err := bs.Get(c, bill.ClosesOn)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "paid %s off the %s bill closing %s — %s\n",
			after.Fmt(p.Value), c.Code, after.ClosesOn, after.Status)
		return nil
	})
}
