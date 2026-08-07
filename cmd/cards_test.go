package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kakei/internal/cards"
	"kakei/internal/db"
)

// runCardsIn points KAKEI_DB at a database of this case's own, captures what
// the command writes and returns both.
//
// Only the paths that never open a form are driven from here: new, edit and the
// delete confirmation all block on a TTY, so they stay in cards.Form's
// territory and are covered through the store instead.
func runCardsIn(t *testing.T, dbPath string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("KAKEI_DB", dbPath)

	var buf bytes.Buffer
	old := out
	out = &buf
	t.Cleanup(func() { out = old })

	err := runCards(args)
	return buf.String(), err
}

// seedCard puts one credit card in the database at path and hands it back.
func seedCard(t *testing.T, path string, c cards.Card) cards.Card {
	t.Helper()
	t.Setenv("KAKEI_DB", path)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := cards.NewStore(conn).Create(&c); err != nil {
		t.Fatal(err)
	}
	return c
}

func nubank() cards.Card {
	return cards.Card{
		Code: "NUCRD", Name: "Nubank", Color: "violet", Currency: "BRL",
		Limit: 500000, Balance: 123850, ClosingDay: 15, DueDay: 22,
	}
}

func TestCardsHelp(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"top level -h", []string{"-h"}, "Manage credit cards"},
		{"top level --help", []string{"--help"}, "Manage credit cards"},
		{"new -h", []string{"new", "-h"}, "Create a credit card"},
		{"n -h", []string{"n", "-h"}, "Create a credit card"},
		{"edit -h", []string{"edit", "-h"}, "Edit a credit card"},
		{"delete --help", []string{"delete", "--help"}, "Delete a credit card"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Help must not need a database at all — point at a path that
			// cannot be created and it should still print.
			got, err := runCardsIn(t, filepath.Join(t.TempDir(), "unused.db"), tc.args...)
			if err != nil {
				t.Fatalf("kakei cc %v = %v", tc.args, err)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("kakei cc %v printed:\n%s\nwant it to mention %q", tc.args, got, tc.want)
			}
		})
	}

	t.Run("new explains what balance means", func(t *testing.T) {
		got, _ := runCardsIn(t, filepath.Join(t.TempDir(), "unused.db"), "new", "-h")
		for _, want := range []string{"open invoice", "1-31"} {
			if !strings.Contains(got, want) {
				t.Errorf("kakei cc new -h does not mention %q:\n%s", want, got)
			}
		}
	})
}

func TestCardsList(t *testing.T) {
	t.Run("empty database points at the create command", func(t *testing.T) {
		got, err := runCardsIn(t, newTestDB(t))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "no credit cards yet") || !strings.Contains(got, "kakei cc n") {
			t.Fatalf("empty list printed:\n%s", got)
		}
	})

	t.Run("prints a table of the cards", func(t *testing.T) {
		path := newTestDB(t)
		seedCard(t, path, nubank())
		other := nubank()
		other.Code, other.Name, other.Currency = "ITAU1", "Itaú", "USD"
		seedCard(t, path, other)

		got, err := runCardsIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"NUCRD", "Nubank", "ITAU1", "Itaú", "15/22"} {
			if !strings.Contains(got, want) {
				t.Errorf("list is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("does not list accounts", func(t *testing.T) {
		path := newTestDB(t)
		seedCard(t, path, nubank())
		seed(t, path, wallet())

		got, err := runCardsIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got, "WLLT2") {
			t.Fatalf("kakei cc listed an account:\n%s", got)
		}
	})
}

func TestCardsDetails(t *testing.T) {
	t.Run("an unknown subcommand is treated as a reference", func(t *testing.T) {
		path := newTestDB(t)
		c := seedCard(t, path, nubank())

		for _, ref := range []string{"NUCRD", "nucrd", "1"} {
			got, err := runCardsIn(t, path, ref)
			if err != nil {
				t.Fatalf("kakei cc %s = %v", ref, err)
			}
			if !strings.Contains(got, c.Name) || !strings.Contains(got, "R$3761.50") {
				t.Errorf("kakei cc %s printed:\n%s", ref, got)
			}
		}
	})

	t.Run("a reference that matches nothing is an error", func(t *testing.T) {
		got, err := runCardsIn(t, newTestDB(t), "NOPE1")
		if err == nil {
			t.Fatalf("missing card was not an error; printed:\n%s", got)
		}
		if !strings.Contains(err.Error(), `no credit card matching "NOPE1"`) {
			t.Fatalf("error = %v; want it to name the reference", err)
		}
	})

	t.Run("an account's code is not a card", func(t *testing.T) {
		path := newTestDB(t)
		seed(t, path, wallet())
		if _, err := runCardsIn(t, path, "WLLT2"); err == nil {
			t.Fatal("kakei cc found an account by its code")
		}
	})
}

func TestCardsEditMissing(t *testing.T) {
	if _, err := runCardsIn(t, newTestDB(t), "e", "NOPE1"); err == nil {
		t.Fatal("edit of a missing card was not an error")
	}
}

func TestCardsDeleteMissing(t *testing.T) {
	// The confirmation prompt needs a TTY, so only the failing lookup — which
	// happens first — is reachable here.
	if _, err := runCardsIn(t, newTestDB(t), "delete", "NOPE1"); err == nil {
		t.Fatal("delete of a missing card was not an error")
	}
}

func TestCardsWithoutADatabase(t *testing.T) {
	// A directory where the database file should be: opening it must fail
	// loudly rather than panic.
	path := filepath.Join(t.TempDir(), "kakei.db")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := runCardsIn(t, path); err == nil {
		t.Fatal("listing with an unopenable database was not an error")
	}
}
