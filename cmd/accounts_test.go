package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kakei/internal/accounts"
	"kakei/internal/db"
)

// runAccountsIn points KAKEI_DB at a database of this case's own, captures what
// the command writes and returns both. Every case gets its own SQLite file, so
// nothing here depends on the order tests run in.
//
// Only the paths that never open a form are driven from here: new, edit and the
// delete confirmation all block on a TTY, so they stay in accounts.Form's
// territory and are covered through the store instead.
func runAccountsIn(t *testing.T, dbPath string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("KAKEI_DB", dbPath)

	var buf bytes.Buffer
	old := out
	out = &buf
	t.Cleanup(func() { out = old })

	err := runAccounts(args)
	return buf.String(), err
}

// newTestDB returns the path to an empty, migrated database.
func newTestDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kakei.db")
	t.Setenv("KAKEI_DB", path)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	return path
}

// seed puts one account in the database at path and hands it back.
func seed(t *testing.T, path string, a accounts.Account) accounts.Account {
	t.Helper()
	t.Setenv("KAKEI_DB", path)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := accounts.NewStore(conn).Create(&a); err != nil {
		t.Fatal(err)
	}
	return a
}

// freeze flips an account to frozen in the database at path.
func freeze(t *testing.T, path string, a accounts.Account) {
	t.Helper()
	t.Setenv("KAKEI_DB", path)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := accounts.NewStore(conn).ToggleFreeze(a.ID); err != nil {
		t.Fatal(err)
	}
}

func wallet() accounts.Account {
	return accounts.Account{Code: "WLLT2", Name: "Wallet", Color: "green", Currency: "BTC", Balance: 150000000}
}

func TestAccountsHelp(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"top level -h", []string{"-h"}, "Manage accounts"},
		{"top level --help", []string{"--help"}, "Manage accounts"},
		{"new -h", []string{"new", "-h"}, "Create an account"},
		{"n -h", []string{"n", "-h"}, "Create an account"},
		{"edit -h", []string{"edit", "-h"}, "Edit an account"},
		{"delete -h", []string{"delete", "-h"}, "Delete an account"},
		{"freeze --help", []string{"freeze", "--help"}, "Freeze or unfreeze"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Help must not need a database at all — point at a path that
			// cannot be created and it should still print.
			got, err := runAccountsIn(t, filepath.Join(t.TempDir(), "unused.db"), tc.args...)
			if err != nil {
				t.Fatalf("kakei ac %v = %v", tc.args, err)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("kakei ac %v printed:\n%s\nwant it to mention %q", tc.args, got, tc.want)
			}
		})
	}
}

func TestAccountsList(t *testing.T) {
	t.Run("empty database points at the create command", func(t *testing.T) {
		got, err := runAccountsIn(t, newTestDB(t))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "no accounts yet") || !strings.Contains(got, "kakei ac n") {
			t.Fatalf("empty list printed:\n%s", got)
		}
	})

	t.Run("prints a table of the accounts", func(t *testing.T) {
		path := newTestDB(t)
		seed(t, path, wallet())
		seed(t, path, accounts.Account{Code: "SAVE1", Name: "Savings", Color: "blue", Currency: "BRL"})

		got, err := runAccountsIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"WLLT2", "Wallet", "SAVE1", "Savings"} {
			if !strings.Contains(got, want) {
				t.Errorf("list is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("hides frozen accounts", func(t *testing.T) {
		path := newTestDB(t)
		seed(t, path, wallet())
		freeze(t, path, seed(t, path, accounts.Account{Code: "OLDAC", Name: "Antiga", Color: "pink", Currency: "BRL"}))

		got, err := runAccountsIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got, "OLDAC") {
			t.Errorf("frozen account listed without --all:\n%s", got)
		}
		if !strings.Contains(got, "WLLT2") {
			t.Errorf("active account missing:\n%s", got)
		}
	})

	t.Run("--all brings the frozen ones back", func(t *testing.T) {
		path := newTestDB(t)
		seed(t, path, wallet())
		freeze(t, path, seed(t, path, accounts.Account{Code: "OLDAC", Name: "Antiga", Color: "pink", Currency: "BRL"}))

		got, err := runAccountsIn(t, path, "--all")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"WLLT2", "OLDAC", "❄"} {
			if !strings.Contains(got, want) {
				t.Errorf("kakei ac --all is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("says where the frozen ones went", func(t *testing.T) {
		path := newTestDB(t)
		freeze(t, path, seed(t, path, wallet()))

		got, err := runAccountsIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		// Every account is frozen: an empty table would look like data loss.
		if !strings.Contains(got, "--all") {
			t.Fatalf("all-frozen list does not mention --all:\n%s", got)
		}
	})
}

func TestAccountsDetails(t *testing.T) {
	t.Run("an unknown subcommand is treated as a reference", func(t *testing.T) {
		path := newTestDB(t)
		a := seed(t, path, wallet())

		for _, ref := range []string{"WLLT2", "wllt2", "1"} {
			got, err := runAccountsIn(t, path, ref)
			if err != nil {
				t.Fatalf("kakei ac %s = %v", ref, err)
			}
			if !strings.Contains(got, a.Name) || !strings.Contains(got, "₿1.50000000") {
				t.Errorf("kakei ac %s printed:\n%s", ref, got)
			}
		}
	})

	t.Run("a reference that matches nothing is an error", func(t *testing.T) {
		got, err := runAccountsIn(t, newTestDB(t), "NOPE1")
		if err == nil {
			t.Fatalf("missing account was not an error; printed:\n%s", got)
		}
		if !strings.Contains(err.Error(), `no account matching "NOPE1"`) {
			t.Fatalf("error = %v; want it to name the reference", err)
		}
	})
}

func TestAccountsFreeze(t *testing.T) {
	t.Run("toggles and reports the new state", func(t *testing.T) {
		path := newTestDB(t)
		seed(t, path, wallet())

		got, err := runAccountsIn(t, path, "freeze", "WLLT2")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "is now frozen") {
			t.Fatalf("first freeze printed:\n%s", got)
		}

		got, err = runAccountsIn(t, path, "f", "wllt2")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "is now unfrozen") {
			t.Fatalf("second freeze printed:\n%s", got)
		}
	})

	t.Run("reports a missing account instead of opening the picker", func(t *testing.T) {
		if _, err := runAccountsIn(t, newTestDB(t), "freeze", "NOPE1"); err == nil {
			t.Fatal("freeze of a missing account was not an error")
		}
	})
}

func TestAccountsDeleteMissing(t *testing.T) {
	// The confirmation prompt needs a TTY, so only the failing lookup — which
	// happens first — is reachable here.
	if _, err := runAccountsIn(t, newTestDB(t), "delete", "NOPE1"); err == nil {
		t.Fatal("delete of a missing account was not an error")
	}
}

func TestAccountsEditMissing(t *testing.T) {
	if _, err := runAccountsIn(t, newTestDB(t), "e", "NOPE1"); err == nil {
		t.Fatal("edit of a missing account was not an error")
	}
}

func TestAccountsWithoutADatabase(t *testing.T) {
	// A directory where the database file should be: opening it must fail
	// loudly rather than panic.
	dir := t.TempDir()
	path := filepath.Join(dir, "kakei.db")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := runAccountsIn(t, path); err == nil {
		t.Fatal("listing with an unopenable database was not an error")
	}
}

func TestReportTreatsCancelAsSuccess(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"no error", nil, 0},
		{"cancelled", accounts.ErrCancelled, 0},
		{"wrapped cancel", errors.New("x"), 1},
		{"real failure", accounts.ErrNotFound, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := report("accounts", tc.err); got != tc.want {
				t.Fatalf("report(%v) = %d; want %d", tc.err, got, tc.want)
			}
		})
	}
}
