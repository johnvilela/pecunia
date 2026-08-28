package main

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"pecunia/internal/accounts"
	"pecunia/internal/cards"
	"pecunia/internal/categories"
	"pecunia/internal/db"
	"pecunia/internal/goals"
	"pecunia/internal/logs"
	"pecunia/internal/transactions"
)

// runTransactionsIn points PECUNIA_DB at a database of this case's own, captures
// what the command writes and returns both.
//
// Only the paths that never open a form are driven from here: new, edit and the
// delete confirmation all block on a TTY, so they stay in transactions.Form's
// territory and are covered through the store instead.
func runTransactionsIn(t *testing.T, dbPath string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("PECUNIA_DB", dbPath)

	var buf bytes.Buffer
	old := out
	out = &buf
	t.Cleanup(func() { out = old })

	err := runTransactions(args)
	return buf.String(), err
}

// ledger is a database with one account, one card, one category and whatever
// transactions the case asks for.
type ledger struct {
	path     string
	account  accounts.Account
	card     cards.Card
	category categories.Category
}

func newLedger(t *testing.T) *ledger {
	t.Helper()
	l := &ledger{path: filepath.Join(t.TempDir(), "pecunia.db")}
	l.with(t, func(conn *sql.DB) {
		l.account = accounts.Account{Code: "INTER", Name: "Banco Inter", Color: "orange",
			Currency: "BRL", Balance: 1000000}
		if err := accounts.NewStore(conn).Create(&l.account); err != nil {
			t.Fatal(err)
		}
		l.card = cards.Card{Code: "NUCRD", Name: "Nubank", Color: "violet", Currency: "BRL",
			Limit: 500000, ClosingDay: 15, DueDay: 22}
		if err := cards.NewStore(conn).Create(&l.card); err != nil {
			t.Fatal(err)
		}
		l.category = categories.Category{Code: "FOOD1", Name: "Food", Color: "lime"}
		if err := categories.NewStore(conn).Create(&l.category, logs.User); err != nil {
			t.Fatal(err)
		}
	})
	return l
}

func (l *ledger) with(t *testing.T, fn func(*sql.DB)) {
	t.Helper()
	t.Setenv("PECUNIA_DB", l.path)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fn(conn)
}

func (l *ledger) add(t *testing.T, tr transactions.Transaction) transactions.Transaction {
	t.Helper()
	if tr.Value == 0 {
		tr.Value = 12000
	}
	if tr.Kind == "" {
		tr.Kind = transactions.KindOutcome
	}
	if tr.Account.ID == 0 && tr.Card.ID == 0 {
		tr.Account = transactions.Ref{ID: l.account.ID}
	}
	l.with(t, func(conn *sql.DB) {
		if err := transactions.NewStore(conn).Create(&tr); err != nil {
			t.Fatal(err)
		}
	})
	return tr
}

// today and lastMonth keep the cases off fixed dates: the default list is the
// current month, so what "this month" means moves with the clock.
func today() string { return time.Now().Format("2006-01-02") }

func lastMonth() string {
	// The first of this month, minus a day, is always in the month before —
	// which the 31st of this one would not be.
	now := time.Now()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, -1).Format("2006-01-02")
}

func TestTransactionsHelp(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"top level -h", []string{"-h"}, "Record and review"},
		{"top level --help", []string{"--help"}, "Record and review"},
		{"new -h", []string{"new", "-h"}, "Record a transaction"},
		{"n -h", []string{"n", "-h"}, "Record a transaction"},
		{"edit -h", []string{"edit", "-h"}, "Edit a transaction"},
		{"e --help", []string{"e", "--help"}, "Edit a transaction"},
		{"delete --help", []string{"delete", "--help"}, "Delete a transaction"},
		{"d -h", []string{"d", "-h"}, "Delete a transaction"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Help must not need a database at all — point at a path that
			// cannot be created and it should still print.
			got, err := runTransactionsIn(t, filepath.Join(t.TempDir(), "nope", "unused.db"), tc.args...)
			if err != nil {
				t.Fatalf("pecunia t %v = %v", tc.args, err)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("pecunia t %v printed %q; want it to contain %q", tc.args, got, tc.want)
			}
		})
	}
}

func TestTransactionsList(t *testing.T) {
	t.Run("defaults to this month and says so", func(t *testing.T) {
		l := newLedger(t)
		l.add(t, transactions.Transaction{Title: "Groceries", Date: today()})
		l.add(t, transactions.Transaction{Title: "Old rent", Date: lastMonth()})

		got, err := runTransactionsIn(t, l.path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "Groceries") {
			t.Fatalf("this month's transaction is missing:\n%s", got)
		}
		if strings.Contains(got, "Old rent") {
			t.Fatalf("last month's transaction should not be in the default list:\n%s", got)
		}
		// The scope is implicit, so the footer has to make it explicit.
		for _, want := range []string{time.Now().Format("January 2006"), "--all"} {
			if !strings.Contains(got, want) {
				t.Fatalf("the footer is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("--all reaches back", func(t *testing.T) {
		l := newLedger(t)
		l.add(t, transactions.Transaction{Title: "Groceries", Date: today()})
		l.add(t, transactions.Transaction{Title: "Old rent", Date: lastMonth()})

		got, err := runTransactionsIn(t, l.path, "--all")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"DATE", "TITLE", "CATEGORY", "SOURCE", "AMOUNT", "Groceries", "Old rent"} {
			if !strings.Contains(got, want) {
				t.Fatalf("pecunia t --all is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("an empty database says how to start", func(t *testing.T) {
		l := newLedger(t)
		got, err := runTransactionsIn(t, l.path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"no transactions yet", "pecunia t n"} {
			if !strings.Contains(got, want) {
				t.Fatalf("empty list = %q; want it to mention %q", got, want)
			}
		}
	})

	t.Run("an empty month says so without saying the ledger is empty", func(t *testing.T) {
		l := newLedger(t)
		l.add(t, transactions.Transaction{Title: "Old rent", Date: lastMonth()})

		got, err := runTransactionsIn(t, l.path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got, "no transactions yet") {
			t.Fatalf("an empty month reads as an empty ledger:\n%s", got)
		}
		if !strings.Contains(got, "--all") {
			t.Fatalf("an empty month does not say how to widen:\n%s", got)
		}
	})
}

func TestTransactionsFilters(t *testing.T) {
	// build gives every filter something to leave out.
	build := func(t *testing.T) *ledger {
		t.Helper()
		l := newLedger(t)
		l.add(t, transactions.Transaction{Title: "Groceries", Date: "2026-03-08",
			Tags: []string{"food"}, Category: transactions.Ref{ID: l.category.ID}})
		l.add(t, transactions.Transaction{Title: "Coffee", Date: "2026-03-20", Value: 800})
		l.add(t, transactions.Transaction{Title: "Card lunch", Date: "2026-04-02",
			Card: transactions.Ref{ID: l.card.ID}})
		return l
	}

	cases := []struct {
		name  string
		args  []string
		want  []string
		avoid []string
	}{
		{"--date", []string{"--date", "2026-03-08"}, []string{"Groceries"}, []string{"Coffee", "Card lunch"}},
		{"--month", []string{"--month", "2026-03"}, []string{"Groceries", "Coffee"}, []string{"Card lunch"}},
		{"--from and --to", []string{"--from", "2026-03-10", "--to", "2026-04-30"},
			[]string{"Coffee", "Card lunch"}, []string{"Groceries"}},
		{"--tag", []string{"--tag", "food"}, []string{"Groceries"}, []string{"Coffee"}},
		{"--search", []string{"--search", "coff"}, []string{"Coffee"}, []string{"Groceries"}},
		{"--category by code", []string{"--category", "FOOD1"}, []string{"Groceries"}, []string{"Coffee"}},
		{"--category in any case", []string{"--category", "food1"}, []string{"Groceries"}, []string{"Coffee"}},
		{"--account", []string{"--account", "INTER"}, []string{"Groceries", "Coffee"}, []string{"Card lunch"}},
		{"--card", []string{"--card", "NUCRD"}, []string{"Card lunch"}, []string{"Groceries"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := build(t)
			got, err := runTransactionsIn(t, l.path, tc.args...)
			if err != nil {
				t.Fatalf("pecunia t %v = %v", tc.args, err)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("pecunia t %v is missing %q:\n%s", tc.args, want, got)
				}
			}
			for _, avoid := range tc.avoid {
				if strings.Contains(got, avoid) {
					t.Fatalf("pecunia t %v should have left %q out:\n%s", tc.args, avoid, got)
				}
			}
		})
	}

	t.Run("an unknown flag is an error, not a silent list", func(t *testing.T) {
		l := build(t)
		if _, err := runTransactionsIn(t, l.path, "--nope"); err == nil {
			t.Fatal("pecunia t --nope = nil; want an error")
		}
	})

	t.Run("a filter naming something that is not there says which", func(t *testing.T) {
		l := build(t)
		_, err := runTransactionsIn(t, l.path, "--category", "NOPE1")
		if err == nil || !strings.Contains(err.Error(), "NOPE1") {
			t.Fatalf("pecunia t --category NOPE1 = %v; want it to name the reference", err)
		}
	})

	t.Run("a malformed date is an error", func(t *testing.T) {
		l := build(t)
		for _, args := range [][]string{{"--date", "08/03/2026"}, {"--month", "2026-3"}, {"--from", "nope"}} {
			if _, err := runTransactionsIn(t, l.path, args...); err == nil {
				t.Fatalf("pecunia t %v = nil; want an error", args)
			}
		}
	})
}

func TestTransactionsDetails(t *testing.T) {
	t.Run("an id shows the card", func(t *testing.T) {
		l := newLedger(t)
		made := l.add(t, transactions.Transaction{Title: "Groceries", Date: today(),
			Tags: []string{"food"}, Category: transactions.Ref{ID: l.category.ID}})

		got, err := runTransactionsIn(t, l.path, strconv.FormatInt(made.ID, 10))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Groceries", "120.00", "FOOD1", "INTER", "food"} {
			if !strings.Contains(got, want) {
				t.Fatalf("the details card is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("an unknown id names what was asked for", func(t *testing.T) {
		l := newLedger(t)
		_, err := runTransactionsIn(t, l.path, "999")
		if err == nil || !strings.Contains(err.Error(), `no transaction matching "999"`) {
			t.Fatalf("pecunia t 999 = %v; want it to name the reference", err)
		}
	})

	t.Run("a transaction is referenced by id and nothing else", func(t *testing.T) {
		l := newLedger(t)
		l.add(t, transactions.Transaction{Title: "Groceries", Date: today()})
		_, err := runTransactionsIn(t, l.path, "Groceries")
		if err == nil || !strings.Contains(err.Error(), "Groceries") {
			t.Fatalf("pecunia t Groceries = %v; want it to say that is not an id", err)
		}
	})
}

// Edit and delete open a form or a confirm prompt, but the lookup happens
// first — so the failing-lookup path is reachable without a TTY.
func TestTransactionsEditAndDeleteMissing(t *testing.T) {
	for _, sub := range []string{"edit", "e", "delete", "d"} {
		t.Run(sub+" on an unknown id", func(t *testing.T) {
			l := newLedger(t)
			l.add(t, transactions.Transaction{Title: "Groceries", Date: today()})

			_, err := runTransactionsIn(t, l.path, sub, "999")
			if err == nil || !strings.Contains(err.Error(), `no transaction matching "999"`) {
				t.Fatalf("pecunia t %s 999 = %v", sub, err)
			}
		})
	}
}

func TestTransactionsWithoutADatabase(t *testing.T) {
	t.Run("reports the error instead of panicking", func(t *testing.T) {
		// A directory where the file should be: Open cannot create it.
		if _, err := runTransactionsIn(t, t.TempDir()); err == nil {
			t.Fatal("pecunia t on an unopenable database = nil; want an error")
		}
	})
}

// goal puts one goal in the ledger's database and hands it back.
func (l *ledger) goal(t *testing.T, name string) goals.Goal {
	t.Helper()
	g := goals.Goal{Name: name, Target: 500000, Currency: "BRL", Kind: goals.KindSaving}
	l.with(t, func(conn *sql.DB) {
		if err := goals.NewStore(conn).Create(&g); err != nil {
			t.Fatal(err)
		}
	})
	return g
}

func TestTransactionsFilterByGoal(t *testing.T) {
	t.Run("narrows to the transactions feeding it", func(t *testing.T) {
		l := newLedger(t)
		g := l.goal(t, "New laptop")
		// Deliberately last month: a goal's transactions span months, so the
		// filter has to widen past the this-month default on its own.
		l.add(t, transactions.Transaction{Title: "Toward the laptop", Date: lastMonth(),
			Goal: transactions.Ref{ID: g.ID}})
		l.add(t, transactions.Transaction{Title: "Coffee", Date: today()})

		got, err := runTransactionsIn(t, l.path, "--goal", strconv.FormatInt(g.ID, 10))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "Toward the laptop") {
			t.Errorf("--goal did not widen past this month:\n%s", got)
		}
		if strings.Contains(got, "Coffee") {
			t.Errorf("--goal kept a transaction that feeds no goal:\n%s", got)
		}
	})

	t.Run("an unknown goal says so", func(t *testing.T) {
		l := newLedger(t)
		if _, err := runTransactionsIn(t, l.path, "--goal", "404"); err == nil {
			t.Fatal("--goal 404 = nil; want an error")
		}
	})

	t.Run("a goal is referenced by id, never a code", func(t *testing.T) {
		l := newLedger(t)
		l.goal(t, "New laptop")
		_, err := runTransactionsIn(t, l.path, "--goal", "LAPTOP")
		if err == nil || !strings.Contains(err.Error(), "id") {
			t.Fatalf("--goal LAPTOP = %v; want it to say goals have no code", err)
		}
	})
}

// seedTransfer records one transfer in the database at path and hands back the
// group, which is the id of the leg the money left.
func seedTransfer(t *testing.T, path string) int64 {
	t.Helper()
	t.Setenv("PECUNIA_DB", path)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	as := accounts.NewStore(conn)
	from := accounts.Account{Code: "NUBON", Name: "Nubank", Color: "violet",
		Currency: "BRL", Balance: 100000}
	to := accounts.Account{Code: "INTER", Name: "Inter", Color: "orange",
		Currency: "BRL", Balance: 0}
	for _, a := range []*accounts.Account{&from, &to} {
		if err := as.Create(a); err != nil {
			t.Fatal(err)
		}
	}

	tr := transactions.Transfer{
		Title: "Transferência", Date: "2026-08-14",
		From: transactions.Ref{ID: from.ID}, To: transactions.Ref{ID: to.ID},
		FromValue: 50000, ToValue: 50000,
	}
	ts := transactions.NewStore(conn)
	if err := ts.Transfer(&tr); err != nil {
		t.Fatal(err)
	}
	// One ordinary transaction beside it, so a case can tell the two apart.
	feira := transactions.Transaction{Title: "Feira", Value: 8400,
		Kind: transactions.KindOutcome, Date: "2026-08-14",
		Account: transactions.Ref{ID: from.ID}}
	if err := ts.Create(&feira); err != nil {
		t.Fatal(err)
	}
	return tr.Group
}

func TestTransactionsTransferHelp(t *testing.T) {
	for _, args := range [][]string{{"transfer", "-h"}, {"tr", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nope", "unused.db")
			got, err := runTransactionsIn(t, path, args...)
			if err != nil {
				t.Fatalf("help returned %v", err)
			}
			if !strings.Contains(got, "Move money between two accounts") {
				t.Fatalf("help = %q; want the transfer help", got)
			}
		})
	}
}

func TestTransactionsTransferList(t *testing.T) {
	t.Run("--transfers shows only the transfers", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		seedTransfer(t, path)

		got, err := runTransactionsIn(t, path, "--transfers")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "Transferência") {
			t.Errorf("--transfers left the transfer out:\n%s", got)
		}
		if strings.Contains(got, "Feira") {
			t.Errorf("--transfers listed an ordinary transaction:\n%s", got)
		}
	})

	t.Run("both legs are listed, pointing opposite ways", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		seedTransfer(t, path)

		got, err := runTransactionsIn(t, path, "--transfers")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "→") || !strings.Contains(got, "←") {
			t.Errorf("want both legs and both arrows:\n%s", got)
		}
	})

	t.Run("the ordinary list shows them too", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		seedTransfer(t, path)

		got, err := runTransactionsIn(t, path, "--all")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "Transferência") {
			t.Errorf("the ledger hid a real movement:\n%s", got)
		}
	})
}

func TestTransactionsTransferDetails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pecunia.db")
	group := seedTransfer(t, path)

	got, err := runTransactionsIn(t, path, strconv.FormatInt(group, 10))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NUBON", "INTER", "R$500.00", "the other side"} {
		if !strings.Contains(got, want) {
			t.Errorf("details is missing %q:\n%s", want, got)
		}
	}
}

func TestEditRefusesAdjustments(t *testing.T) {
	l := newLedger(t)
	var id int64
	l.with(t, func(conn *sql.DB) {
		adj := transactions.Transaction{
			Title: "Balance adjustment", Value: -5000, Kind: transactions.KindAdjustment,
			Date: "2026-08-27", Account: transactions.Ref{ID: l.account.ID},
		}
		if err := transactions.NewStore(conn).Create(&adj); err != nil {
			t.Fatal(err)
		}
		id = adj.ID
	})

	_, err := runTransactionsIn(t, l.path, "edit", strconv.FormatInt(id, 10))
	if err == nil || !strings.Contains(err.Error(), "balance adjustment") {
		t.Fatalf("edit on an adjustment = %v; want the refusal naming it", err)
	}
}
