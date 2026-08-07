package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"kakei/internal/accounts"
	"kakei/internal/core"
	"kakei/internal/db"
)

// out is where every command writes; tests swap it for a buffer.
var out io.Writer = os.Stdout

const accountsHelp = `Manage accounts.

Usage:
  kakei accounts [command] [CODE|ID]
  kakei ac       [command] [CODE|ID]

Commands:
  (none)              list the active accounts
  --all   | -a        list every account, frozen ones included
  new     | n         create an account
  edit    | e  [ref]  edit an account
  delete  | d  [ref]  delete an account
  freeze  | f  [ref]  freeze or unfreeze an account
  CODE|ID             show one account in detail

Frozen accounts are hidden from the list, and shown dimmed at the bottom with
--all. Leaving [ref] out opens a picker. Add -h to any command for its own help.
`

var subHelp = map[string]string{
	"new": `Create an account.

Usage:
  kakei accounts new
  kakei ac n

Opens a form: name, description (optional), code, color, currency, balance.
The code is 5 characters and comes pre-filled with a free suggestion.
Colors are a preset of 12. Currencies are Dollar, Euro, Brazilian Real and
Bitcoin — the balance is stored at that currency's precision (2 decimals for
fiat, 8 for Bitcoin).
`,
	"edit": `Edit an account.

Usage:
  kakei accounts edit [CODE|ID]
  kakei ac e [CODE|ID]

Opens the create form pre-filled. Without CODE|ID, pick from a list first.
`,
	"delete": `Delete an account for good.

Usage:
  kakei accounts delete [CODE|ID]
  kakei ac d [CODE|ID]

Asks for confirmation. Without CODE|ID, pick from a list first.
`,
	"freeze": `Freeze or unfreeze an account.

Usage:
  kakei accounts freeze [CODE|ID]
  kakei ac f [CODE|ID]

Toggles: a frozen account is unfrozen and the new state is printed.
Without CODE|ID, pick from a list first.
`,
}

var errNoAccounts = errors.New("no accounts yet — create one with: kakei ac n")

func isHelpFlag(s string) bool { return s == "-h" || s == "--help" }

func runAccounts(args []string) error {
	if len(args) == 0 {
		return listAccounts(false)
	}
	sub, rest := args[0], args[1:]
	if isHelpFlag(sub) {
		fmt.Fprint(out, accountsHelp)
		return nil
	}
	if sub == "--all" || sub == "-a" {
		return listAccounts(true)
	}

	name := map[string]string{
		"new": "new", "n": "new",
		"edit": "edit", "e": "edit",
		"delete": "delete", "d": "delete",
		"freeze": "freeze", "f": "freeze",
	}[sub]

	if name == "" {
		// Anything else is a {CODE|ID}.
		return withStore(func(s *accounts.Store) error {
			a, err := resolveOrPick(s, args, "Account details")
			if err != nil {
				return err
			}
			fmt.Fprint(out, accounts.Details(a))
			return nil
		})
	}
	if len(rest) > 0 && isHelpFlag(rest[0]) {
		fmt.Fprint(out, subHelp[name])
		return nil
	}

	return withStore(func(s *accounts.Store) error {
		switch name {
		case "new":
			return createAccount(s)
		case "edit":
			return editAccount(s, rest)
		case "delete":
			return deleteAccount(s, rest)
		default:
			return freezeAccount(s, rest)
		}
	})
}

func withStore(fn func(*accounts.Store) error) error {
	conn, err := db.Open()
	if err != nil {
		return err
	}
	defer conn.Close()
	return fn(accounts.NewStore(conn))
}

// resolveOrPick turns an optional {CODE|ID} argument into an account, falling
// back to the picker when none was given.
func resolveOrPick(s *accounts.Store, args []string, title string) (accounts.Account, error) {
	if len(args) > 0 && args[0] != "" {
		a, err := s.Resolve(args[0])
		if errors.Is(err, accounts.ErrNotFound) {
			return a, fmt.Errorf("no account matching %q", args[0])
		}
		return a, err
	}
	all, err := s.List()
	if err != nil {
		return accounts.Account{}, err
	}
	if len(all) == 0 {
		return accounts.Account{}, errNoAccounts
	}
	return accounts.Pick(all, title)
}

func listAccounts(showAll bool) error {
	return withStore(func(s *accounts.Store) error {
		all, err := s.List()
		if err != nil {
			return err
		}
		if len(all) == 0 {
			fmt.Fprintln(out, "no accounts yet — create one with: kakei ac n")
			return nil
		}

		shown := all
		if !showAll {
			shown = nil
			for _, a := range all {
				if !a.IsFrozen {
					shown = append(shown, a)
				}
			}
		}
		if len(shown) == 0 {
			// An empty table here would read as data loss.
			fmt.Fprintf(out, "every account is frozen — list them with: kakei ac --all\n")
			return nil
		}

		fmt.Fprintln(out, accounts.Table(shown))
		if n := len(all) - len(shown); n > 0 {
			fmt.Fprintf(out, "%d frozen account(s) hidden — show them with: kakei ac --all\n", n)
		}
		return nil
	})
}

func createAccount(s *accounts.Store) error {
	code, err := s.SuggestCode()
	if err != nil {
		return err
	}
	a := accounts.Account{
		Code:     code,
		Color:    core.Palette[0].Name,
		Currency: core.Currencies[0].Code,
	}
	if err := accounts.Form(s, &a, "New account"); err != nil {
		return err
	}
	if err := s.Create(&a); err != nil {
		return err
	}
	fmt.Fprintf(out, "created account %s (%s)\n", a.Code, a.Name)
	return nil
}

func editAccount(s *accounts.Store, args []string) error {
	a, err := resolveOrPick(s, args, "Edit which account?")
	if err != nil {
		return err
	}
	if err := accounts.Form(s, &a, "Edit account"); err != nil {
		return err
	}
	if err := s.Update(a); err != nil {
		return err
	}
	fmt.Fprintf(out, "updated account %s (%s)\n", a.Code, a.Name)
	return nil
}

func deleteAccount(s *accounts.Store, args []string) error {
	a, err := resolveOrPick(s, args, "Delete which account?")
	if err != nil {
		return err
	}
	ok, err := core.Confirm(
		fmt.Sprintf("Delete %s (%s)?", a.Code, a.Name),
		"This cannot be undone.")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "cancelled")
		return nil
	}
	if err := s.Delete(a.ID); err != nil {
		return err
	}
	fmt.Fprintf(out, "deleted account %s (%s)\n", a.Code, a.Name)
	return nil
}

func freezeAccount(s *accounts.Store, args []string) error {
	a, err := resolveOrPick(s, args, "Freeze which account?")
	if err != nil {
		return err
	}
	frozen, err := s.ToggleFreeze(a.ID)
	if err != nil {
		return err
	}
	state := "unfrozen"
	if frozen {
		state = "frozen"
	}
	fmt.Fprintf(out, "%s (%s) is now %s\n", a.Code, a.Name, state)
	return nil
}
