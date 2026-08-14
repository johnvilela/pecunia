package main

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"kakei/internal/budgets"
	"kakei/internal/db"
	"kakei/internal/transactions"
)

// runBudgetsIn points KAKEI_DB at a database of this case's own, captures what
// the command writes and returns both.
//
// Only the paths that never open a form are driven from here: new, edit and the
// delete confirmation all block on a TTY, so they stay in budgets.Form's
// territory and are covered through the store instead.
func runBudgetsIn(t *testing.T, dbPath string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("KAKEI_DB", dbPath)

	var buf bytes.Buffer
	old := out
	out = &buf
	t.Cleanup(func() { out = old })

	err := runBudgets(args)
	return buf.String(), err
}

// openAt is the database at path, for the cases that have to reach past the
// command to set something up.
func openAt(t *testing.T, path string) *sql.DB {
	t.Helper()
	t.Setenv("KAKEI_DB", path)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// seedBudget puts a category and a budget over it in the database at path.
func seedBudget(t *testing.T, path, code, name string, amount int64) budgets.Budget {
	t.Helper()
	conn := openAt(t, path)

	res, err := conn.Exec(
		`INSERT INTO categories (code, name, color) VALUES (?, ?, 'green')`, code[:4]+"C", name)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	b := budgets.Budget{
		Code: code, Name: name, Description: "what it is allowed to cost",
		Amount: amount, Currency: "BRL", Color: "green",
		Category: transactions.Ref{ID: cat},
	}
	if err := budgets.NewStore(conn).Create(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

// spendOn files one outcome under the budget's category, so a case can put the
// budget somewhere other than zero.
func spendOn(t *testing.T, path string, b budgets.Budget, value int64, date string) {
	t.Helper()
	conn := openAt(t, path)

	var cat int64
	if err := conn.QueryRow(`SELECT category_id FROM budgets WHERE id = ?`, b.ID).Scan(&cat); err != nil {
		t.Fatal(err)
	}
	var acc int64
	if err := conn.QueryRow(`SELECT id FROM accounts WHERE code = 'WLLT1'`).Scan(&acc); err != nil {
		res, err := conn.Exec(
			`INSERT INTO accounts (code, name, color, balance, currency)
			 VALUES ('WLLT1', 'Wallet', 'orange', 0, 'BRL')`)
		if err != nil {
			t.Fatal(err)
		}
		if acc, err = res.LastInsertId(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.Exec(
		`INSERT INTO transactions (title, account_id, category_id, value, kind, date)
		 VALUES ('Groceries', ?, ?, ?, 'outcome', ?)`, acc, cat, value, date); err != nil {
		t.Fatal(err)
	}
}

func TestBudgetsHelp(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"top level -h", []string{"-h"}, "Budgets"},
		{"top level --help", []string{"--help"}, "Budgets"},
		{"new -h", []string{"new", "-h"}, "Create a budget"},
		{"n -h", []string{"n", "-h"}, "Create a budget"},
		{"edit -h", []string{"edit", "-h"}, "Edit a budget"},
		{"e --help", []string{"e", "--help"}, "Edit a budget"},
		{"delete --help", []string{"delete", "--help"}, "Delete a budget"},
		{"d -h", []string{"d", "-h"}, "Delete a budget"},
		{"archive -h", []string{"archive", "-h"}, "Archive a budget"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Help must not need a database at all — point at a path that cannot
			// be created and it should still print.
			path := filepath.Join(t.TempDir(), "nope", "unused.db")
			got, err := runBudgetsIn(t, path, tc.args...)
			if err != nil {
				t.Fatalf("help returned %v", err)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("help = %q; want it to contain %q", got, tc.want)
			}
		})
	}
}

func TestBudgetsList(t *testing.T) {
	t.Run("the table is the list", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedBudget(t, path, "FOOD1", "Food", 80000)
		seedBudget(t, path, "FUEL1", "Fuel", 30000)

		got, err := runBudgetsIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"BUDGET", "SPENT", "Food", "Fuel", "R$800.00", "R$300.00"} {
			if !strings.Contains(got, want) {
				t.Errorf("list is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("the month being shown is named", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedBudget(t, path, "FOOD1", "Food", 80000)

		got, err := runBudgetsIn(t, path, "--month", "2026-07")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "July 2026") {
			t.Errorf("list does not say which month it is:\n%s", got)
		}
	})

	t.Run("--month narrows the spend to that month", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		b := seedBudget(t, path, "FOOD1", "Food", 80000)
		spendOn(t, path, b, 54000, "2026-07-10")
		spendOn(t, path, b, 12000, "2026-08-10")

		got, err := runBudgetsIn(t, path, "--month", "2026-07")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "R$540.00") {
			t.Errorf("July does not show July's spend:\n%s", got)
		}
		if strings.Contains(got, "R$120.00") {
			t.Errorf("July dragged August's spend in:\n%s", got)
		}
	})

	t.Run("a broken month is refused", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedBudget(t, path, "FOOD1", "Food", 80000)

		if _, err := runBudgetsIn(t, path, "--month", "August"); err == nil {
			t.Fatal("--month August succeeded; want it refused")
		}
	})

	t.Run("--month with nothing after it is refused", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedBudget(t, path, "FOOD1", "Food", 80000)

		if _, err := runBudgetsIn(t, path, "--month"); err == nil {
			t.Fatal("a bare --month succeeded; want it refused")
		}
	})

	t.Run("an archived budget is left out until --all", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedBudget(t, path, "FOOD1", "Food", 80000)
		if _, err := runBudgetsIn(t, path, "FOOD1", "archive"); err != nil {
			t.Fatal(err)
		}

		got, err := runBudgetsIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got, "Food") {
			t.Errorf("the archived budget is still in the list:\n%s", got)
		}

		got, err = runBudgetsIn(t, path, "--all")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "Food") {
			t.Errorf("--all left the archived budget out:\n%s", got)
		}
	})

	t.Run("an empty database says how to start", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		got, err := runBudgetsIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "kakei bg n") {
			t.Errorf("list does not say how to make a budget:\n%s", got)
		}
	})
}

func TestBudgetsDetails(t *testing.T) {
	t.Run("a code shows the card", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		b := seedBudget(t, path, "FOOD1", "Food", 80000)
		spendOn(t, path, b, 54000, "2026-08-10")

		got, err := runBudgetsIn(t, path, "FOOD1", "--month", "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Food", "FOOD1", "R$540.00", "R$800.00", "August 2026"} {
			if !strings.Contains(got, want) {
				t.Errorf("details is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("the card looks back over the months before it", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		b := seedBudget(t, path, "FOOD1", "Food", 80000)
		spendOn(t, path, b, 70000, "2026-06-10")
		spendOn(t, path, b, 54000, "2026-08-10")

		got, err := runBudgetsIn(t, path, "FOOD1", "--month", "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"last months", "2026-06", "R$700.00"} {
			if !strings.Contains(got, want) {
				t.Errorf("the history is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("the list does not drag the history along", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedBudget(t, path, "FOOD1", "Food", 80000)

		got, err := runBudgetsIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got, "last months") {
			t.Errorf("the list showed a per-budget history:\n%s", got)
		}
	})

	t.Run("an unknown code says which", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedBudget(t, path, "FOOD1", "Food", 80000)

		_, err := runBudgetsIn(t, path, "NOPE1")
		if err == nil || !strings.Contains(err.Error(), "NOPE1") {
			t.Fatalf("details of an unknown code = %v; want it to name the ref", err)
		}
	})
}

func TestBudgetsArchive(t *testing.T) {
	t.Run("archive and unarchive both report what happened", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedBudget(t, path, "FOOD1", "Food", 80000)

		got, err := runBudgetsIn(t, path, "FOOD1", "archive")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "archived") {
			t.Errorf("archive said %q; want it to say so", got)
		}

		got, err = runBudgetsIn(t, path, "FOOD1", "unarchive")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "back") {
			t.Errorf("unarchive said %q; want it to say the budget is back", got)
		}
	})

	t.Run("the code may come on either side of the command", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedBudget(t, path, "FOOD1", "Food", 80000)

		if _, err := runBudgetsIn(t, path, "archive", "FOOD1"); err != nil {
			t.Fatalf("verb before the code: %v", err)
		}
	})

	t.Run("an unknown command is named rather than guessed at", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedBudget(t, path, "FOOD1", "Food", 80000)

		_, err := runBudgetsIn(t, path, "FOOD1", "frobnicate")
		if err == nil || !strings.Contains(err.Error(), "frobnicate") {
			t.Fatalf("err = %v; want it to name the unknown command", err)
		}
	})
}

func TestBudgetsEditAndDeleteMissing(t *testing.T) {
	// The lookup has to fail before any form or confirmation opens, or these
	// cases would block on a TTY that is not there.
	for _, verb := range []string{"edit", "e", "delete", "d", "archive"} {
		t.Run(verb+" with an unknown code errors", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "kakei.db")
			seedBudget(t, path, "FOOD1", "Food", 80000)

			if _, err := runBudgetsIn(t, path, verb, "NOPE1"); err == nil {
				t.Fatalf("%s NOPE1 = nil; want an error", verb)
			}
		})
	}
}

func TestBudgetsWithoutADatabase(t *testing.T) {
	// A directory is not a database file, so opening it must fail rather than
	// panic somewhere further in.
	dir := t.TempDir()
	if _, err := runBudgetsIn(t, dir); err == nil {
		t.Fatal("listing with an unopenable database was not an error")
	}
}
