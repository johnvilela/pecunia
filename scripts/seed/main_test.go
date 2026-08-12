package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	"kakei/internal/accounts"
	"kakei/internal/bills"
	"kakei/internal/cards"
	"kakei/internal/categories"
	"kakei/internal/core"
	"kakei/internal/db"
	"kakei/internal/transactions"
)

func newTestStore(t *testing.T) *accounts.Store {
	t.Helper()
	t.Setenv("KAKEI_DB", filepath.Join(t.TempDir(), "kakei.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return accounts.NewStore(conn)
}

func TestSeed(t *testing.T) {
	t.Run("inserts every fixture", func(t *testing.T) {
		s := newTestStore(t)
		n, err := seed(s)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(fixtures) {
			t.Fatalf("seed inserted %d; want %d", n, len(fixtures))
		}

		all, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != len(fixtures) {
			t.Fatalf("database holds %d accounts; want %d", len(all), len(fixtures))
		}
	})

	t.Run("running twice changes nothing", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := seed(s); err != nil {
			t.Fatal(err)
		}

		n, err := seed(s)
		if err != nil {
			t.Fatalf("second seed: %v", err)
		}
		if n != 0 {
			t.Fatalf("second seed inserted %d; want 0", n)
		}

		all, _ := s.List()
		if len(all) != len(fixtures) {
			t.Fatalf("database holds %d accounts after two seeds; want %d", len(all), len(fixtures))
		}
	})

	t.Run("leaves an edited fixture alone", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := seed(s); err != nil {
			t.Fatal(err)
		}

		a, err := s.ByCode(fixtures[0].Code)
		if err != nil {
			t.Fatal(err)
		}
		a.Balance = 42
		if err := s.Update(a); err != nil {
			t.Fatal(err)
		}

		if _, err := seed(s); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.ByCode(fixtures[0].Code); got.Balance != 42 {
			t.Fatalf("seed overwrote a local edit: balance is %d", got.Balance)
		}
	})
}

func TestFixturesAreValid(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range fixtures {
		t.Run(f.Code, func(t *testing.T) {
			if err := core.ValidateCode(f.Code); err != nil {
				t.Errorf("code %q: %v", f.Code, err)
			}
			if seen[f.Code] {
				t.Errorf("code %q appears twice", f.Code)
			}
			seen[f.Code] = true

			if f.Name == "" {
				t.Error("fixture has no name")
			}
			if core.ColorByName(f.Color).Name != f.Color {
				t.Errorf("color %q is not in the palette", f.Color)
			}
			if core.CurrencyByCode(f.Currency).Code != f.Currency {
				t.Errorf("currency %q is not supported", f.Currency)
			}
		})
	}
}

func newTestCardStore(t *testing.T) *cards.Store {
	t.Helper()
	t.Setenv("KAKEI_DB", filepath.Join(t.TempDir(), "kakei.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return cards.NewStore(conn)
}

func TestSeedCards(t *testing.T) {
	t.Run("inserts every fixture", func(t *testing.T) {
		s := newTestCardStore(t)
		n, err := seedCards(s)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(cardFixtures) {
			t.Fatalf("seedCards inserted %d; want %d", n, len(cardFixtures))
		}

		all, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != len(cardFixtures) {
			t.Fatalf("database holds %d cards; want %d", len(all), len(cardFixtures))
		}
	})

	t.Run("running twice changes nothing", func(t *testing.T) {
		s := newTestCardStore(t)
		if _, err := seedCards(s); err != nil {
			t.Fatal(err)
		}

		n, err := seedCards(s)
		if err != nil {
			t.Fatalf("second seed: %v", err)
		}
		if n != 0 {
			t.Fatalf("second seed inserted %d; want 0", n)
		}
	})

	t.Run("leaves an edited fixture alone", func(t *testing.T) {
		s := newTestCardStore(t)
		if _, err := seedCards(s); err != nil {
			t.Fatal(err)
		}

		c, err := s.ByCode(cardFixtures[0].Code)
		if err != nil {
			t.Fatal(err)
		}
		c.Balance = 42
		if err := s.Update(c); err != nil {
			t.Fatal(err)
		}

		if _, err := seedCards(s); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.ByCode(cardFixtures[0].Code); got.Balance != 42 {
			t.Fatalf("seed overwrote a local edit: balance is %d", got.Balance)
		}
	})
}

func TestCardFixturesAreValid(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range cardFixtures {
		t.Run(f.Code, func(t *testing.T) {
			if err := core.ValidateCode(f.Code); err != nil {
				t.Errorf("code %q: %v", f.Code, err)
			}
			if seen[f.Code] {
				t.Errorf("code %q appears twice", f.Code)
			}
			seen[f.Code] = true

			if f.Name == "" {
				t.Error("fixture has no name")
			}
			if core.ColorByName(f.Color).Name != f.Color {
				t.Errorf("color %q is not in the palette", f.Color)
			}
			if core.CurrencyByCode(f.Currency).Code != f.Currency {
				t.Errorf("currency %q is not supported", f.Currency)
			}
			if f.ClosingDay < 1 || f.ClosingDay > 31 || f.DueDay < 1 || f.DueDay > 31 {
				t.Errorf("days %d/%d are outside 1-31", f.ClosingDay, f.DueDay)
			}
			if err := f.ValidateBalance(); err != nil {
				t.Error(err)
			}
		})
	}
}

// The fixtures exist to exercise every branch of the renderers.
func TestCardFixturesCoverTheRenderBranches(t *testing.T) {
	var overLimit, zeroBalance, noDescription, mayGoOver, mayNot bool
	for _, f := range cardFixtures {
		overLimit = overLimit || f.Available() < 0
		zeroBalance = zeroBalance || f.Balance == 0
		noDescription = noDescription || f.Description == ""
		mayGoOver = mayGoOver || f.OverLimitAllowed
		mayNot = mayNot || !f.OverLimitAllowed
	}
	if !overLimit || !zeroBalance || !noDescription || !mayGoOver || !mayNot {
		t.Fatalf("fixtures miss a branch: overLimit=%v zeroBalance=%v noDescription=%v mayGoOver=%v mayNot=%v",
			overLimit, zeroBalance, noDescription, mayGoOver, mayNot)
	}
}

// newTestConn is newTestStore's twin for the seeders that need more than one
// store off the same connection.
func newTestConn(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("KAKEI_DB", filepath.Join(t.TempDir(), "kakei.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// seedEverything is the order main runs the seeders in: transactions point at
// accounts, cards and categories, so those have to be there first.
func seedEverything(t *testing.T, conn *sql.DB) int {
	t.Helper()
	if _, err := seed(accounts.NewStore(conn)); err != nil {
		t.Fatal(err)
	}
	if _, err := seedCards(cards.NewStore(conn)); err != nil {
		t.Fatal(err)
	}
	if _, err := categories.Seed(categories.NewStore(conn)); err != nil {
		t.Fatal(err)
	}
	n, err := seedTransactions(conn)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSeedTransactions(t *testing.T) {
	t.Run("inserts every fixture", func(t *testing.T) {
		conn := newTestConn(t)
		// More rows than fixtures: one of them is a five-way split.
		if n := seedEverything(t, conn); n != txRows() {
			t.Fatalf("seeded %d transactions; want %d", n, txRows())
		}

		all, err := transactions.NewStore(conn).List(transactions.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != txRows() {
			t.Fatalf("database holds %d transactions; want %d", len(all), txRows())
		}
	})

	t.Run("running twice changes nothing", func(t *testing.T) {
		conn := newTestConn(t)
		seedEverything(t, conn)

		n, err := seedTransactions(conn)
		if err != nil {
			t.Fatalf("second seed: %v", err)
		}
		if n != 0 {
			t.Fatalf("second seed inserted %d; want 0", n)
		}
	})

	t.Run("every fixture is filed against something that exists", func(t *testing.T) {
		conn := newTestConn(t)
		seedEverything(t, conn)

		all, err := transactions.NewStore(conn).List(transactions.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, tr := range all {
			if tr.Target().Code == "" {
				t.Fatalf("%q has no account or card", tr.Title)
			}
			if tr.Currency == "" {
				t.Fatalf("%q inherited no currency", tr.Title)
			}
		}
	})

	t.Run("the balances moved with the transactions", func(t *testing.T) {
		conn := newTestConn(t)
		// Seed the accounts alone first, so what the transactions did to them
		// is the difference between the two readings.
		if _, err := seed(accounts.NewStore(conn)); err != nil {
			t.Fatal(err)
		}
		before, err := accounts.NewStore(conn).ByCode("INTER")
		if err != nil {
			t.Fatal(err)
		}
		seedEverything(t, conn)
		after, err := accounts.NewStore(conn).ByCode("INTER")
		if err != nil {
			t.Fatal(err)
		}
		if before.Balance == after.Balance {
			t.Fatal("INTER's balance did not move; want the transactions to have reached it")
		}
	})
}

func TestSeedInstallments(t *testing.T) {
	conn := newTestConn(t)
	seedEverything(t, conn)

	all, err := transactions.NewStore(conn).List(transactions.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	var series []transactions.Transaction
	for _, tr := range all {
		if tr.IsInstallment() {
			series = append(series, tr)
		}
	}
	if len(series) != 5 {
		t.Fatalf("%d installment rows; want 5", len(series))
	}
	var sum int64
	group := series[0].Installment.Group
	for _, tr := range series {
		if tr.Installment.Group != group {
			t.Fatalf("the series is split across groups %d and %d", group, tr.Installment.Group)
		}
		if tr.Card.Code != "NUCRD" {
			t.Fatalf("an installment landed on %q", tr.Target().Code)
		}
		sum += tr.Value
	}
	if sum != 100000 {
		t.Fatalf("the installments sum to %d; want the whole 100000", sum)
	}
}

func TestSeedBillPayment(t *testing.T) {
	t.Run("leaves a bill partly paid", func(t *testing.T) {
		conn := newTestConn(t)
		seedEverything(t, conn)

		n, err := seedBillPayment(conn)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("seeded %d payments; want 1", n)
		}

		card, err := cards.NewStore(conn).ByCode("NUCRD")
		if err != nil {
			t.Fatal(err)
		}
		found, err := bills.NewStore(conn).List(card)
		if err != nil {
			t.Fatal(err)
		}
		partial := 0
		for _, b := range found {
			if b.Status == bills.StatusPartial {
				partial++
			}
		}
		if partial != 1 {
			t.Fatalf("%d partial bill(s); want exactly 1", partial)
		}
	})

	t.Run("running twice pays nothing more", func(t *testing.T) {
		conn := newTestConn(t)
		seedEverything(t, conn)
		if _, err := seedBillPayment(conn); err != nil {
			t.Fatal(err)
		}

		n, err := seedBillPayment(conn)
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("the second run paid %d more", n)
		}
	})
}
