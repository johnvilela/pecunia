package main

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"pecunia/internal/accounts"
	"pecunia/internal/db"
	"pecunia/internal/recurring"
	"pecunia/internal/transactions"
)

// runBillsIn points PECUNIA_DB at a database of this case's own, captures what
// the command writes and returns both.
//
// Only the paths that never open a form are driven from here: new, edit, pay
// and the delete confirmation all block on a TTY, so they stay in the ui
// package's territory and are covered through the store instead.
func runBillsIn(t *testing.T, dbPath string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("PECUNIA_DB", dbPath)

	var buf bytes.Buffer
	old := out
	out = &buf
	t.Cleanup(func() { out = old })

	err := runRecurring(args)
	return buf.String(), err
}

// seedBill puts one bill in the database at path, on an account it creates the
// first time, and hands both back.
func seedBill(t *testing.T, path string, b recurring.Bill) (recurring.Bill, *sql.DB) {
	t.Helper()
	t.Setenv("PECUNIA_DB", path)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	as := accounts.NewStore(conn)
	all, err := as.List()
	if err != nil {
		t.Fatal(err)
	}
	var account int64
	if len(all) > 0 {
		account = all[0].ID
	} else {
		a := accounts.Account{Code: "INTER", Name: "Banco Inter", Color: "orange",
			Currency: "BRL", Balance: 1000000}
		if err := as.Create(&a); err != nil {
			t.Fatal(err)
		}
		account = a.ID
	}
	b.Account = transactions.Ref{ID: account}
	if err := recurring.NewStore(conn).Create(&b); err != nil {
		t.Fatal(err)
	}
	return b, conn
}

func energyBill() recurring.Bill {
	return recurring.Bill{
		Code: "ENERG", Name: "Energy", Description: "Neoenergia", Color: "amber",
		Expected: 21490, OpenDay: 5, DueDay: 15,
	}
}

func TestRecurringHelp(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"top level -h", []string{"-h"}, "Recurring bills"},
		{"top level --help", []string{"--help"}, "Recurring bills"},
		{"new -h", []string{"new", "-h"}, "Create a recurring bill"},
		{"n -h", []string{"n", "-h"}, "Create a recurring bill"},
		{"pay -h", []string{"pay", "-h"}, "Pay a bill"},
		{"edit -h", []string{"edit", "-h"}, "Edit a recurring bill"},
		{"delete --help", []string{"delete", "--help"}, "Delete a recurring bill"},
		{"archive -h", []string{"archive", "-h"}, "Archive a bill"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Help must not need a database at all — point at a path that cannot
			// be created and it should still print.
			path := filepath.Join(t.TempDir(), "nope", "unused.db")
			got, err := runBillsIn(t, path, tc.args...)
			if err != nil {
				t.Fatalf("help returned %v", err)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("help = %q; want it to contain %q", got, tc.want)
			}
		})
	}
}

func TestRecurringList(t *testing.T) {
	t.Run("says how to start when there are none", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		got, err := runBillsIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "pecunia bill n") {
			t.Errorf("empty list does not say how to make one:\n%s", got)
		}
	})

	t.Run("shows the board", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		seedBill(t, path, energyBill())

		got, err := runBillsIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"ENERG", "Energy", "R$214.90"} {
			if !strings.Contains(got, want) {
				t.Errorf("board is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("leaves archived bills out until --all", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		seedBill(t, path, energyBill())
		gone := energyBill()
		gone.Code, gone.Name = "NFLIX", "Netflix"
		seedBill(t, path, gone)
		if _, err := runBillsIn(t, path, "NFLIX", "archive"); err != nil {
			t.Fatal(err)
		}

		got, err := runBillsIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got, "NFLIX") {
			t.Errorf("an archived bill is still on the board:\n%s", got)
		}

		got, err = runBillsIn(t, path, "--all")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "NFLIX") {
			t.Errorf("--all left the archived bill out:\n%s", got)
		}
	})
}

func TestRecurringDetails(t *testing.T) {
	t.Run("shows one bill and what it has cost", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		bill, conn := seedBill(t, path, energyBill())
		tr := transactions.Transaction{
			Title: "Energy", Value: 19000, Kind: transactions.KindOutcome, Date: "2026-07-08",
			Account: bill.Account, Recurring: transactions.Ref{ID: bill.ID}, Cycle: "2026-07",
		}
		if err := transactions.NewStore(conn).Create(&tr); err != nil {
			t.Fatal(err)
		}

		got, err := runBillsIn(t, path, "ENERG")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"ENERG", "Energy", "Neoenergia", "R$190.00", "average"} {
			if !strings.Contains(got, want) {
				t.Errorf("details are missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("finds a bill however the code is typed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		seedBill(t, path, energyBill())
		got, err := runBillsIn(t, path, "energ")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "Energy") {
			t.Errorf("a lowercase code found nothing:\n%s", got)
		}
	})

	t.Run("names an unknown code", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		seedBill(t, path, energyBill())
		_, err := runBillsIn(t, path, "NOPE1")
		if err == nil || !strings.Contains(err.Error(), "NOPE1") {
			t.Fatalf("error = %v, want one naming the code", err)
		}
	})
}

func TestRecurringArchive(t *testing.T) {
	t.Run("archives and brings back, either way round", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		seedBill(t, path, energyBill())

		// The code may come on either side of the verb: pecunia bill ENERG archive
		// is how it is said out loud, pecunia bill archive ENERG is how every other
		// module reads.
		got, err := runBillsIn(t, path, "ENERG", "archive")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "archived") {
			t.Errorf("archive said nothing:\n%s", got)
		}

		got, err = runBillsIn(t, path, "unarchive", "ENERG")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "ENERG") {
			t.Errorf("unarchive said nothing:\n%s", got)
		}

		board, err := runBillsIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(board, "ENERG") {
			t.Errorf("the bill did not come back:\n%s", board)
		}
	})
}
