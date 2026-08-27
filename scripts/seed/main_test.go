package main

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"kakei/internal/accounts"
	"kakei/internal/bills"
	"kakei/internal/budgets"
	"kakei/internal/cards"
	"kakei/internal/categories"
	"kakei/internal/core"
	"kakei/internal/db"
	"kakei/internal/goals"
	"kakei/internal/recurring"
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
	if _, err := seedGoals(conn); err != nil {
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

func TestSeedGoals(t *testing.T) {
	t.Run("inserts every fixture", func(t *testing.T) {
		conn := newTestConn(t)
		n, err := seedGoals(conn)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(goalFixtures) {
			t.Fatalf("seedGoals inserted %d; want %d", n, len(goalFixtures))
		}

		all, err := goals.NewStore(conn).List()
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != len(goalFixtures) {
			t.Fatalf("database holds %d goals; want %d", len(all), len(goalFixtures))
		}
	})

	t.Run("running twice changes nothing", func(t *testing.T) {
		conn := newTestConn(t)
		if _, err := seedGoals(conn); err != nil {
			t.Fatal(err)
		}
		n, err := seedGoals(conn)
		if err != nil {
			t.Fatalf("second seed: %v", err)
		}
		if n != 0 {
			t.Fatalf("second seed inserted %d; want 0", n)
		}
	})
}

func TestGoalFixturesAreValid(t *testing.T) {
	t.Run("every fixture passes its own validation", func(t *testing.T) {
		for _, f := range goalFixtures {
			if err := f.Validate(); err != nil {
				t.Errorf("%s: %v", f.Name, err)
			}
		}
	})

	t.Run("the fixtures cover the render branches", func(t *testing.T) {
		var saving, paying, described, bare bool
		for _, f := range goalFixtures {
			switch f.Kind {
			case goals.KindSaving:
				saving = true
			case goals.KindPaying:
				paying = true
			}
			if f.Description == "" {
				bare = true
			} else {
				described = true
			}
		}
		if !saving || !paying {
			t.Error("the fixtures do not cover both kinds of goal")
		}
		if !described || !bare {
			t.Error("the fixtures do not cover a goal with and without a description")
		}
	})

	t.Run("every linked transaction is in its goal's currency", func(t *testing.T) {
		// The store would refuse the seed outright, but failing here says which
		// fixture is wrong instead of which insert.
		currency := map[string]string{}
		for _, g := range goalFixtures {
			currency[g.Name] = g.Currency
		}
		accountCurrency := map[string]string{}
		for _, a := range fixtures {
			accountCurrency[a.Code] = a.Currency
		}
		for _, c := range cardFixtures {
			accountCurrency[c.Code] = c.Currency
		}
		for _, f := range txFixtures {
			if f.Goal == "" {
				continue
			}
			want, ok := currency[f.Goal]
			if !ok {
				t.Errorf("%s names goal %q, which is not a fixture", f.Title, f.Goal)
				continue
			}
			source := f.Account
			if source == "" {
				source = f.Card
			}
			if got := accountCurrency[source]; got != want {
				t.Errorf("%s is filed in %s against a goal counting %s", f.Title, got, want)
			}
		}
	})
}

func TestSeedLinksTransactionsToGoals(t *testing.T) {
	conn := newTestConn(t)
	seedEverything(t, conn)

	all, err := goals.NewStore(conn).List()
	if err != nil {
		t.Fatal(err)
	}
	var moved int
	for _, g := range all {
		if g.Progress() != 0 {
			moved++
		}
	}
	if moved == 0 {
		t.Fatal("no seeded goal has any progress; the dev database would render only empty bars")
	}
}

func TestSeedTargetChange(t *testing.T) {
	t.Run("cuts one goal's target and says why", func(t *testing.T) {
		conn := newTestConn(t)
		if _, err := seedGoals(conn); err != nil {
			t.Fatal(err)
		}
		n, err := seedTargetChange(conn)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("seedTargetChange made %d changes; want 1", n)
		}

		g, err := goalByName(conn, "Quitar o Itaú")
		if err != nil {
			t.Fatal(err)
		}
		log, err := goals.NewStore(conn).TargetLog(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(log) != 1 {
			t.Fatalf("the log has %d entries; want 1", len(log))
		}
		if log[0].Target != g.Target || log[0].Previous == log[0].Target {
			t.Errorf("entry = %d → %d against a live target of %d",
				log[0].Previous, log[0].Target, g.Target)
		}
		if log[0].Note == "" || log[0].CreatedAt == "" {
			t.Errorf("entry = %+v; want a reason and a date", log[0])
		}
	})

	t.Run("running twice changes nothing", func(t *testing.T) {
		conn := newTestConn(t)
		if _, err := seedGoals(conn); err != nil {
			t.Fatal(err)
		}
		if _, err := seedTargetChange(conn); err != nil {
			t.Fatal(err)
		}
		n, err := seedTargetChange(conn)
		if err != nil {
			t.Fatalf("second run: %v", err)
		}
		if n != 0 {
			t.Fatalf("second run made %d changes; want 0", n)
		}
	})

	t.Run("a database without the fixture is left alone", func(t *testing.T) {
		conn := newTestConn(t)
		n, err := seedTargetChange(conn)
		if err != nil {
			t.Fatalf("no goals at all = %v; want it to do nothing quietly", err)
		}
		if n != 0 {
			t.Fatalf("made %d changes with no goals seeded; want 0", n)
		}
	})
}

func TestSeedRecurring(t *testing.T) {
	// The bills point at accounts and cards by code, so those come first.
	prepare := func(t *testing.T) *sql.DB {
		t.Helper()
		conn := newTestConn(t)
		if _, err := seed(accounts.NewStore(conn)); err != nil {
			t.Fatal(err)
		}
		if _, err := seedCards(cards.NewStore(conn)); err != nil {
			t.Fatal(err)
		}
		if _, err := categories.Seed(categories.NewStore(conn)); err != nil {
			t.Fatal(err)
		}
		return conn
	}

	t.Run("inserts every fixture", func(t *testing.T) {
		conn := prepare(t)
		n, err := seedRecurring(conn)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(recurringFixtures) {
			t.Fatalf("seedRecurring inserted %d; want %d", n, len(recurringFixtures))
		}
		all, err := recurring.NewStore(conn).List(true)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != len(recurringFixtures) {
			t.Fatalf("database holds %d bills; want %d", len(all), len(recurringFixtures))
		}
	})

	t.Run("running twice changes nothing", func(t *testing.T) {
		conn := prepare(t)
		if _, err := seedRecurring(conn); err != nil {
			t.Fatal(err)
		}
		n, err := seedRecurring(conn)
		if err != nil {
			t.Fatalf("second seed: %v", err)
		}
		if n != 0 {
			t.Fatalf("second seed inserted %d; want 0", n)
		}
	})

	t.Run("the fixtures cover the board's states", func(t *testing.T) {
		conn := prepare(t)
		if _, err := seedRecurring(conn); err != nil {
			t.Fatal(err)
		}
		if _, err := seedRecurringPayments(conn); err != nil {
			t.Fatal(err)
		}

		all, err := recurring.NewStore(conn).List(true)
		if err != nil {
			t.Fatal(err)
		}
		states := map[string]bool{}
		var archived, onCard bool
		for _, b := range all {
			states[b.Current(time.Now()).Status] = true
			archived = archived || !b.Active
			onCard = onCard || b.IsCard()
		}
		// A dev database whose board is all one colour is not worth looking at,
		// and every state has its own row rendering to check.
		want := []string{
			recurring.StatusOverdue, recurring.StatusOpen,
			recurring.StatusUpcoming, recurring.StatusPaid, recurring.StatusArchived,
		}
		// Except on the month's last day: a cycle opening today is open, not
		// upcoming, and no open day lands later than the last day — so that one
		// date cannot render upcoming for any bill whatsoever.
		today := time.Now()
		if today.Month() != today.AddDate(0, 0, 1).Month() {
			want = []string{
				recurring.StatusOverdue, recurring.StatusOpen,
				recurring.StatusPaid, recurring.StatusArchived,
			}
		}
		for _, w := range want {
			if !states[w] {
				t.Errorf("no fixture renders %q; the board shows %v", w, states)
			}
		}
		if !archived {
			t.Error("no archived bill, so the --all branch renders nothing new")
		}
		if !onCard {
			t.Error("no bill paid on a card, so that branch renders nothing")
		}
	})

	t.Run("nothing is dated into the future", func(t *testing.T) {
		conn := prepare(t)
		if _, err := seedRecurring(conn); err != nil {
			t.Fatal(err)
		}
		if _, err := seedRecurringPayments(conn); err != nil {
			t.Fatal(err)
		}
		today := time.Now().Format(recurring.DateLayout)
		paid, err := transactions.NewStore(conn).List(transactions.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range paid {
			// A bill paid for the month it is still in may open after today —
			// the payment is real, its date cannot be.
			if p.Date > today {
				t.Errorf("%s is dated %s, which has not happened yet", p.Title, p.Date)
			}
		}
	})

	t.Run("a payment names the month it was for", func(t *testing.T) {
		conn := prepare(t)
		if _, err := seedRecurring(conn); err != nil {
			t.Fatal(err)
		}
		n, err := seedRecurringPayments(conn)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Fatal("no payments were seeded")
		}
		paid, err := transactions.NewStore(conn).List(transactions.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		var linked int
		for _, p := range paid {
			if p.Recurring.ID != 0 {
				linked++
				if p.Cycle == "" {
					t.Errorf("payment %q names a bill but no month", p.Title)
				}
			}
		}
		if linked != n {
			t.Errorf("%d payments linked, %d reported", linked, n)
		}
	})
}

func TestSeedBudgets(t *testing.T) {
	t.Run("inserts every fixture", func(t *testing.T) {
		conn := newTestConn(t)
		// A budget caps a category, so the starter set has to be there first.
		if _, err := categories.Seed(categories.NewStore(conn)); err != nil {
			t.Fatal(err)
		}
		n, err := seedBudgets(conn)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(budgetFixtures) {
			t.Fatalf("seedBudgets inserted %d; want %d", n, len(budgetFixtures))
		}

		all, err := budgets.NewStore(conn).List(budgets.ThisCycle(time.Now()), true)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != len(budgetFixtures) {
			t.Fatalf("database holds %d budgets; want %d", len(all), len(budgetFixtures))
		}
	})

	t.Run("running twice changes nothing", func(t *testing.T) {
		conn := newTestConn(t)
		if _, err := categories.Seed(categories.NewStore(conn)); err != nil {
			t.Fatal(err)
		}
		if _, err := seedBudgets(conn); err != nil {
			t.Fatal(err)
		}
		n, err := seedBudgets(conn)
		if err != nil {
			t.Fatalf("second seed: %v", err)
		}
		if n != 0 {
			t.Fatalf("second seed inserted %d; want 0", n)
		}
	})

	t.Run("every fixture names a category that exists", func(t *testing.T) {
		conn := newTestConn(t)
		if _, err := categories.Seed(categories.NewStore(conn)); err != nil {
			t.Fatal(err)
		}
		if _, err := seedBudgets(conn); err != nil {
			t.Fatal(err)
		}
		all, err := budgets.NewStore(conn).List(budgets.ThisCycle(time.Now()), true)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range all {
			if b.Category.ID == 0 || b.Category.Code == "" {
				t.Fatalf("budget %s caps nothing: %+v", b.Code, b.Category)
			}
		}
	})

	t.Run("one budget has been archived and one has moved its cap", func(t *testing.T) {
		// The dev database is what the renderers are looked at through, so both
		// branches need something to render.
		conn := newTestConn(t)
		if _, err := categories.Seed(categories.NewStore(conn)); err != nil {
			t.Fatal(err)
		}
		if _, err := seedBudgets(conn); err != nil {
			t.Fatal(err)
		}
		s := budgets.NewStore(conn)
		all, err := s.List(budgets.ThisCycle(time.Now()), true)
		if err != nil {
			t.Fatal(err)
		}

		archived, moved := 0, 0
		for _, b := range all {
			if !b.Active {
				archived++
			}
			log, err := s.AmountLog(b.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(log) > 0 {
				moved++
			}
		}
		if archived == 0 {
			t.Error("no archived budget in the dev database")
		}
		if moved == 0 {
			t.Error("no budget with a cap that has moved in the dev database")
		}
	})
}

// The dev database is what the budget renderers are looked at through, and an
// over-spent budget is the one state the plain fixtures never reach on their
// own — every other cap sits comfortably inside its month.
func TestSeedLeavesABudgetOverSpent(t *testing.T) {
	conn := newTestConn(t)
	seedEverything(t, conn)
	if _, err := seedBudgets(conn); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	all, err := budgets.NewStore(conn).List(budgets.ThisCycle(now), false)
	if err != nil {
		t.Fatal(err)
	}

	over := 0
	for _, b := range all {
		if b.Status(now) == budgets.StatusOver {
			over++
		}
	}
	if over == 0 {
		t.Fatalf("no budget is over its cap in %s; the over branch has nothing to render:\n%+v",
			budgets.ThisCycle(now), all)
	}
}

// Whatever day of the month the seeder runs on, the over-spend has to land in
// the month being looked at — a fixture dated a few days back falls into last
// month when the seeder runs on the 2nd.
func TestOverSpendFixturesAreDatedToday(t *testing.T) {
	found := false
	for _, f := range txFixtures {
		if f.Category != "LSURE" {
			continue
		}
		found = true
		if f.Days != 0 {
			t.Errorf("%q is %d days back; want 0, or it misses the month it is meant to blow",
				f.Title, f.Days)
		}
	}
	if !found {
		t.Fatal("no fixture spends against LSURE, so the Lazer budget can never go over")
	}
}
