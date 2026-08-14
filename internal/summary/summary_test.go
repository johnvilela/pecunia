package summary

import (
	"database/sql"
	"path/filepath"
	"slices"
	"testing"

	"kakei/internal/accounts"
	"kakei/internal/bills"
	"kakei/internal/budgets"
	"kakei/internal/cards"
	"kakei/internal/db"
	"kakei/internal/goals"
	"kakei/internal/recurring"
	"kakei/internal/transactions"
)

// world is a database with something of everything a summary reads. Call it
// inside the subtest, not the parent, or the cases go back to sharing one file.
type world struct {
	conn   *sql.DB
	inter  accounts.Account // BRL 1000.00
	paypal accounts.Account // USD 200.00
	shut   accounts.Account // BRL, frozen
	nucrd  cards.Card       // BRL, closes on the 10th, due on the 20th
}

func newWorld(t *testing.T) *world {
	t.Helper()
	t.Setenv("KAKEI_DB", filepath.Join(t.TempDir(), "kakei.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	w := &world{conn: conn}
	as := accounts.NewStore(conn)
	w.inter = accounts.Account{Code: "INTER", Name: "Inter", Color: "orange", Currency: "BRL", Balance: 100000}
	w.paypal = accounts.Account{Code: "PAYPL", Name: "PayPal", Color: "blue", Currency: "USD", Balance: 20000}
	w.shut = accounts.Account{Code: "OLDAC", Name: "Antiga", Color: "red", Currency: "BRL"}
	for _, a := range []*accounts.Account{&w.inter, &w.paypal, &w.shut} {
		if err := as.Create(a); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := as.ToggleFreeze(w.shut.ID); err != nil {
		t.Fatal(err)
	}

	w.nucrd = cards.Card{Code: "NUCRD", Name: "Nubank", Color: "violet", Currency: "BRL",
		Limit: 500000, ClosingDay: 10, DueDay: 20}
	if err := cards.NewStore(conn).Create(&w.nucrd); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *world) file(t *testing.T, tr transactions.Transaction) transactions.Transaction {
	t.Helper()
	if err := transactions.NewStore(w.conn).Create(&tr); err != nil {
		t.Fatalf("file %q: %v", tr.Title, err)
	}
	return tr
}

// spend is the shorthand every ledger case starts from: money out of INTER.
func (w *world) spend(t *testing.T, date string, value int64) transactions.Transaction {
	t.Helper()
	return w.file(t, transactions.Transaction{Title: "Feira", Value: value,
		Kind: transactions.KindOutcome, Date: date, Account: transactions.Ref{ID: w.inter.ID}})
}

// bill seeds a recurring bill and back-dates it. created_at is where the cycle
// walk starts, and a row written at the real now would make every case here a
// different case next month.
func (w *world) bill(t *testing.T, code string, openDay, dueDay int) recurring.Bill {
	t.Helper()
	b := recurring.Bill{Code: code, Name: code, Color: "blue", Expected: 21490,
		OpenDay: openDay, DueDay: dueDay, Account: transactions.Ref{ID: w.inter.ID}}
	if err := recurring.NewStore(w.conn).Create(&b); err != nil {
		t.Fatal(err)
	}
	// August is the month every case below stands in, so a bill made at the top
	// of it has exactly one cycle behind it — no unpaid June hiding in front of
	// the state the case is named for.
	const made = "2026-08-01 09:00:00"
	if _, err := w.conn.Exec(`UPDATE recurring_bills SET created_at = ? WHERE id = ?`, made, b.ID); err != nil {
		t.Fatal(err)
	}
	b.CreatedAt = made
	return b
}

// settle files the payment that marks one cycle of a bill paid.
func (w *world) settle(t *testing.T, b recurring.Bill, cycle, date string) {
	t.Helper()
	w.file(t, transactions.Transaction{Title: "pago " + b.Code, Value: b.Expected,
		Kind: transactions.KindOutcome, Date: date, Account: transactions.Ref{ID: w.inter.ID},
		Recurring: transactions.Ref{ID: b.ID}, Cycle: cycle})
}

// charge puts money on the card, which is what gives it a statement to owe.
func (w *world) charge(t *testing.T, date string, value int64) {
	t.Helper()
	w.file(t, transactions.Transaction{Title: "Compra", Value: value,
		Kind: transactions.KindOutcome, Date: date, Card: transactions.Ref{ID: w.nucrd.ID}})
}

func (w *world) collect(t *testing.T, p Period, today string) Summary {
	t.Helper()
	s, err := Collect(w.conn, p, on(today))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func aug13() Period { return Period{From: "2026-08-13", To: "2026-08-13"} }

func codes(bs []recurring.Bill) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Code
	}
	slices.Sort(out)
	return out
}

func closings(bs []bills.Bill) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.ClosesOn
	}
	slices.Sort(out)
	return out
}

func titles(ts []transactions.Transaction) []string {
	out := make([]string, len(ts))
	for i, tr := range ts {
		out[i] = tr.Title
	}
	slices.Sort(out)
	return out
}

func TestCollectLedger(t *testing.T) {
	t.Run("covers only the day it was asked for", func(t *testing.T) {
		w := newWorld(t)
		w.spend(t, "2026-08-13", 84000)
		w.spend(t, "2026-08-12", 10000)

		s := w.collect(t, aug13(), "2026-08-13")
		if got := len(s.Ledger); got != 1 {
			t.Fatalf("ledger has %d rows; want only the day's one — %v", got, titles(s.Ledger))
		}
		if s.Out["BRL"] != 84000 {
			t.Errorf("out BRL = %d; want yesterday left out of it", s.Out["BRL"])
		}
	})

	t.Run("covers the whole month when the period is a month", func(t *testing.T) {
		w := newWorld(t)
		w.spend(t, "2026-08-13", 84000)
		w.spend(t, "2026-08-01", 10000)
		w.spend(t, "2026-07-31", 50000)

		s := w.collect(t, Period{From: "2026-08-01", To: "2026-08-31"}, "2026-08-13")
		if got := len(s.Ledger); got != 2 {
			t.Fatalf("ledger has %d rows; want August's two — %v", got, titles(s.Ledger))
		}
		if s.Out["BRL"] != 94000 {
			t.Errorf("out BRL = %d; want 84000 + 10000 and July left out", s.Out["BRL"])
		}
	})

	t.Run("counts income and outcome apart", func(t *testing.T) {
		w := newWorld(t)
		w.spend(t, "2026-08-13", 84000)
		w.file(t, transactions.Transaction{Title: "Salário", Value: 120000,
			Kind: transactions.KindIncome, Date: "2026-08-13", Account: transactions.Ref{ID: w.inter.ID}})

		s := w.collect(t, aug13(), "2026-08-13")
		if s.In["BRL"] != 120000 || s.Out["BRL"] != 84000 {
			t.Errorf("in = %d, out = %d; want them counted apart", s.In["BRL"], s.Out["BRL"])
		}
	})

	t.Run("counts each currency on its own", func(t *testing.T) {
		w := newWorld(t)
		w.spend(t, "2026-08-13", 84000)
		w.file(t, transactions.Transaction{Title: "Refund", Value: 4000,
			Kind: transactions.KindIncome, Date: "2026-08-13", Account: transactions.Ref{ID: w.paypal.ID}})

		s := w.collect(t, aug13(), "2026-08-13")
		if s.In["USD"] != 4000 {
			t.Errorf("in USD = %d; want the dollars kept apart", s.In["USD"])
		}
		if s.Out["BRL"] != 84000 {
			t.Errorf("out BRL = %d; want the reais untouched by the dollars", s.Out["BRL"])
		}
	})

	t.Run("counts a credit-card charge in the day's outcome", func(t *testing.T) {
		w := newWorld(t)
		w.charge(t, "2026-08-13", 15000)

		s := w.collect(t, aug13(), "2026-08-13")
		if s.Out["BRL"] != 15000 {
			t.Errorf("out BRL = %d; want the card charge counted", s.Out["BRL"])
		}
	})

	t.Run("adds up what the month has cost so far", func(t *testing.T) {
		w := newWorld(t)
		w.spend(t, "2026-08-13", 84000)
		w.spend(t, "2026-08-02", 10000)
		w.spend(t, "2026-07-31", 50000) // last month is not this month

		s := w.collect(t, aug13(), "2026-08-13")
		if s.MTD["BRL"] != 94000 {
			t.Errorf("month so far = %d; want 84000 + 10000", s.MTD["BRL"])
		}
	})

	t.Run("leaves the month so far out of a month summary", func(t *testing.T) {
		w := newWorld(t)
		w.spend(t, "2026-08-13", 84000)

		s := w.collect(t, Period{From: "2026-08-01", To: "2026-08-31"}, "2026-08-13")
		if s.MTD != nil {
			t.Errorf("month so far = %v; want nothing, the totals already are the month", s.MTD)
		}
	})

	t.Run("names the day and the month it is for", func(t *testing.T) {
		w := newWorld(t)
		if got := w.collect(t, aug13(), "2026-08-13").Title; got != "Thursday, 13 August 2026" {
			t.Errorf("title = %q; want the weekday and the date", got)
		}
		month := w.collect(t, Period{From: "2026-08-01", To: "2026-08-31"}, "2026-08-13")
		if month.Title != "August 2026" {
			t.Errorf("title = %q; want the month alone", month.Title)
		}
	})
}

func TestCollectDue(t *testing.T) {
	t.Run("finds the bill that is open today", func(t *testing.T) {
		w := newWorld(t)
		w.bill(t, "OPENX", 5, 20) // opened the 5th, due the 20th

		s := w.collect(t, aug13(), "2026-08-13")
		if got := codes(s.Due.Bills); !slices.Equal(got, []string{"OPENX"}) {
			t.Errorf("due = %v; want the open bill", got)
		}
	})

	t.Run("finds the bill that is already late", func(t *testing.T) {
		w := newWorld(t)
		w.bill(t, "LATEX", 1, 5) // due the 5th, eight days ago

		s := w.collect(t, aug13(), "2026-08-13")
		if got := codes(s.Due.Bills); !slices.Equal(got, []string{"LATEX"}) {
			t.Errorf("due = %v; want the overdue bill", got)
		}
	})

	t.Run("leaves a paid bill out of what is due", func(t *testing.T) {
		w := newWorld(t)
		b := w.bill(t, "PAIDX", 1, 5)
		w.settle(t, b, "2026-08", "2026-08-04")

		s := w.collect(t, aug13(), "2026-08-13")
		if got := codes(s.Due.Bills); len(got) != 0 {
			t.Errorf("due = %v; want a settled bill left out", got)
		}
	})

	t.Run("leaves an archived bill out of what is due", func(t *testing.T) {
		w := newWorld(t)
		b := w.bill(t, "GYMXX", 1, 5)
		if err := recurring.NewStore(w.conn).SetActive(b.ID, false); err != nil {
			t.Fatal(err)
		}

		s := w.collect(t, aug13(), "2026-08-13")
		if got := codes(s.Due.Bills); len(got) != 0 {
			t.Errorf("due = %v; want an archived bill left out", got)
		}
	})

	t.Run("skips what is due when the period is a month gone by", func(t *testing.T) {
		w := newWorld(t)
		w.bill(t, "LATEX", 1, 5)

		// A March report must not claim anything is overdue "today".
		s := w.collect(t, Period{From: "2026-03-01", To: "2026-03-31"}, "2026-08-13")
		if !s.Due.Empty() || !s.Soon.Empty() {
			t.Errorf("due = %v, soon = %v; want neither on a period that is over",
				codes(s.Due.Bills), codes(s.Soon.Bills))
		}
		if len(s.Accounts) == 0 {
			t.Error("the balances went away with the bills; they are current either way")
		}
	})
}

func TestCollectSoon(t *testing.T) {
	t.Run("finds the bill that opens inside the next seven days", func(t *testing.T) {
		w := newWorld(t)
		w.bill(t, "SOONX", 18, 25) // opens the 18th, five days out

		s := w.collect(t, aug13(), "2026-08-13")
		if got := codes(s.Soon.Bills); !slices.Equal(got, []string{"SOONX"}) {
			t.Errorf("soon = %v; want the bill opening next week", got)
		}
	})

	t.Run("counts the seventh day in", func(t *testing.T) {
		w := newWorld(t)
		w.bill(t, "DAY7X", 20, 25) // opens the 20th, exactly seven days out

		s := w.collect(t, aug13(), "2026-08-13")
		if got := codes(s.Soon.Bills); !slices.Equal(got, []string{"DAY7X"}) {
			t.Errorf("soon = %v; want the seventh day counted in", got)
		}
	})

	t.Run("leaves a bill opening on the eighth day out", func(t *testing.T) {
		w := newWorld(t)
		w.bill(t, "DAY8X", 21, 25) // opens the 21st, one day past the week

		s := w.collect(t, aug13(), "2026-08-13")
		if got := codes(s.Soon.Bills); len(got) != 0 {
			t.Errorf("soon = %v; want the eighth day left out", got)
		}
	})

	t.Run("leaves a bill that opens today out of the next seven days", func(t *testing.T) {
		w := newWorld(t)
		w.bill(t, "TODAY", 13, 25) // opens today: payable now, so not upcoming

		s := w.collect(t, aug13(), "2026-08-13")
		if got := codes(s.Soon.Bills); len(got) != 0 {
			t.Errorf("soon = %v; want today's bill in the due section instead", got)
		}
		if got := codes(s.Due.Bills); !slices.Equal(got, []string{"TODAY"}) {
			t.Errorf("due = %v; want the bill opening today", got)
		}
	})

	t.Run("finds next month's bill when this month's is paid", func(t *testing.T) {
		w := newWorld(t)
		// Rent opens on the 18th. On the 13th its August cycle is settled, so
		// the cycle it stands at says nothing about the one opening in five
		// days — and rent is exactly the bill nobody wants surprised by.
		b := w.bill(t, "RENTX", 18, 25)
		w.settle(t, b, "2026-08", "2026-08-19")

		s := w.collect(t, Period{From: "2026-09-15", To: "2026-09-15"}, "2026-09-15")
		if got := codes(s.Soon.Bills); !slices.Equal(got, []string{"RENTX"}) {
			t.Errorf("soon = %v; want next month's cycle found behind the paid one", got)
		}
	})

	t.Run("keeps a late bill that reopens next week out of the next seven days", func(t *testing.T) {
		w := newWorld(t)
		// Overdue for August and opening again inside the week: one bill, and
		// it belongs in the section that says pay it now.
		w.bill(t, "TWICE", 18, 25)

		s := w.collect(t, Period{From: "2026-08-30", To: "2026-08-30"}, "2026-08-30")
		if got := codes(s.Due.Bills); !slices.Equal(got, []string{"TWICE"}) {
			t.Fatalf("due = %v; want the overdue August cycle", got)
		}
		if got := codes(s.Soon.Bills); len(got) != 0 {
			t.Errorf("soon = %v; want it listed once, not twice", got)
		}
	})
}

func TestCollectStatements(t *testing.T) {
	t.Run("finds a credit-card statement that is past its due date", func(t *testing.T) {
		w := newWorld(t)
		w.charge(t, "2026-07-05", 98000) // the cycle closing 2026-07-10, due the 20th

		s := w.collect(t, aug13(), "2026-08-13")
		if got := closings(s.Due.Statements); !slices.Equal(got, []string{"2026-07-10"}) {
			t.Errorf("due statements = %v; want July's, due on the 20th", got)
		}
	})

	t.Run("puts a statement due next week in the next seven days", func(t *testing.T) {
		w := newWorld(t)
		w.charge(t, "2026-08-05", 31200) // closes 2026-08-10, due 2026-08-20

		s := w.collect(t, aug13(), "2026-08-13")
		if got := closings(s.Soon.Statements); !slices.Equal(got, []string{"2026-08-10"}) {
			t.Errorf("soon statements = %v; want August's, due in a week", got)
		}
		if got := closings(s.Due.Statements); len(got) != 0 {
			t.Errorf("due statements = %v; want it counted once", got)
		}
	})

	t.Run("leaves the open statement alone", func(t *testing.T) {
		w := newWorld(t)
		w.charge(t, "2026-08-12", 31200) // the cycle still taking charges

		s := w.collect(t, aug13(), "2026-08-13")
		if got := closings(s.Due.Statements); len(got) != 0 {
			t.Errorf("due statements = %v; want a total that is still moving left alone", got)
		}
		if got := closings(s.Soon.Statements); len(got) != 0 {
			t.Errorf("soon statements = %v; want a total that is still moving left alone", got)
		}
	})

	t.Run("asks the same clock for everything", func(t *testing.T) {
		w := newWorld(t)
		w.charge(t, "2026-04-05", 31200)

		// Every status here is judged against the day it was handed. A card
		// statement invented against the wall clock instead would be the one
		// answer on the screen that disagreed with the rest.
		w.collect(t, Period{From: "2026-05-13", To: "2026-05-13"}, "2026-05-13")

		var last string
		if err := w.conn.QueryRow(`SELECT COALESCE(max(closes_on), '') FROM card_bills`).Scan(&last); err != nil {
			t.Fatal(err)
		}
		// On 13 May a card closing on the 10th has its June cycle open and
		// nothing beyond it. The real clock would have run the card months
		// further on.
		if last != "2026-06-10" {
			t.Errorf("the last statement closes on %s; want 2026-06-10, the cycle open on the day it was given", last)
		}
	})
}

func TestCollectBalancesAndGoals(t *testing.T) {
	t.Run("leaves frozen accounts out of the balances", func(t *testing.T) {
		w := newWorld(t)

		s := w.collect(t, aug13(), "2026-08-13")
		for _, a := range s.Accounts {
			if a.Code == "OLDAC" {
				t.Errorf("the frozen account is on the balances; `kakei ac` hides it")
			}
		}
		if len(s.Accounts) != 2 {
			t.Errorf("balances have %d accounts; want the two still in play", len(s.Accounts))
		}
		if len(s.Cards) != 1 {
			t.Errorf("balances have %d cards; want the one", len(s.Cards))
		}
	})

	t.Run("brings the goals back as they stand", func(t *testing.T) {
		w := newWorld(t)
		g := goals.Goal{Name: "Notebook novo", Target: 300000, Currency: "BRL", Kind: goals.KindSaving}
		if err := goals.NewStore(w.conn).Create(&g); err != nil {
			t.Fatal(err)
		}
		w.file(t, transactions.Transaction{Title: "Guardado", Value: 120000,
			Kind: transactions.KindIncome, Date: "2026-08-13",
			Account: transactions.Ref{ID: w.inter.ID}, Goal: transactions.Ref{ID: g.ID}})

		s := w.collect(t, aug13(), "2026-08-13")
		if len(s.Goals) != 1 || s.Goals[0].Progress() != 120000 {
			t.Errorf("goals = %+v; want the one, at what the ledger says", s.Goals)
		}
	})

	t.Run("comes back empty on a database with nothing in it", func(t *testing.T) {
		t.Setenv("KAKEI_DB", filepath.Join(t.TempDir(), "kakei.db"))
		conn, err := db.Open()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })

		s, err := Collect(conn, aug13(), on("2026-08-13"))
		if err != nil {
			t.Fatal(err)
		}
		if !s.empty() {
			t.Errorf("a fresh database came back with something on it: %+v", s)
		}
	})
}

// budget caps a category and hands back what a summary should read for it.
func (w *world) budget(t *testing.T, code, name string, amount int64) budgets.Budget {
	t.Helper()
	res, err := w.conn.Exec(
		`INSERT INTO categories (code, name, color) VALUES (?, ?, 'green')`, code[:4]+"C", name)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	b := budgets.Budget{Code: code, Name: name, Amount: amount, Currency: "BRL",
		Color: "green", Category: transactions.Ref{ID: cat}}
	if err := budgets.NewStore(w.conn).Create(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCollectBudgets(t *testing.T) {
	t.Run("a day summary reads the budgets of the month it falls in", func(t *testing.T) {
		w := newWorld(t)
		b := w.budget(t, "FOOD1", "Food", 80000)
		w.file(t, transactions.Transaction{Title: "Feira", Value: 54000,
			Kind: transactions.KindOutcome, Date: "2026-08-10",
			Account:  transactions.Ref{ID: w.inter.ID},
			Category: transactions.Ref{ID: b.Category.ID}})

		s, err := Collect(w.conn, Period{From: "2026-08-13", To: "2026-08-13"}, on("2026-08-13"))
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Budgets) != 1 {
			t.Fatalf("Budgets = %d; want the one budget", len(s.Budgets))
		}
		// The day is one day, but a budget is a month — R$540.00 went out on the
		// 10th and it is still what the month has spent.
		if s.Budgets[0].Spent != 54000 {
			t.Fatalf("Spent = %d; want the whole month's 54000", s.Budgets[0].Spent)
		}
		if s.Budgets[0].Cycle != "2026-08" {
			t.Fatalf("Cycle = %q; want the month the day falls in", s.Budgets[0].Cycle)
		}
	})

	t.Run("a month summary reads that month", func(t *testing.T) {
		w := newWorld(t)
		b := w.budget(t, "FOOD1", "Food", 80000)
		w.file(t, transactions.Transaction{Title: "Feira", Value: 30000,
			Kind: transactions.KindOutcome, Date: "2026-07-04",
			Account:  transactions.Ref{ID: w.inter.ID},
			Category: transactions.Ref{ID: b.Category.ID}})

		s, err := Collect(w.conn, Period{From: "2026-07-01", To: "2026-07-31"}, on("2026-08-13"))
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Budgets) != 1 || s.Budgets[0].Spent != 30000 {
			t.Fatalf("Budgets = %+v; want July's 30000", s.Budgets)
		}
	})

	t.Run("an archived budget is left out, as the list leaves it out", func(t *testing.T) {
		w := newWorld(t)
		b := w.budget(t, "FOOD1", "Food", 80000)
		if err := budgets.NewStore(w.conn).SetActive(b.ID, false); err != nil {
			t.Fatal(err)
		}

		s, err := Collect(w.conn, Period{From: "2026-08-13", To: "2026-08-13"}, on("2026-08-13"))
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Budgets) != 0 {
			t.Fatalf("Budgets = %d; want the archived one left out", len(s.Budgets))
		}
	})

	t.Run("a window that is already over still has its budgets", func(t *testing.T) {
		// Unlike what is due, a budget is a fact about a month rather than about
		// now, so an old month still has one worth reading.
		w := newWorld(t)
		w.budget(t, "FOOD1", "Food", 80000)

		s, err := Collect(w.conn, Period{From: "2026-06-01", To: "2026-06-30"}, on("2026-08-13"))
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Budgets) != 1 {
			t.Fatalf("Budgets = %d; want the budget read for June anyway", len(s.Budgets))
		}
	})
}

// The whole point of transfers. Money moving between two accounts you own is
// not income and not an expense — counting it as both is what made a month read
// worse and better than it was.
func TestTransfersAreNotTotalled(t *testing.T) {
	// transfer moves R$500.00 from INTER to CASH1 on the given date.
	transfer := func(t *testing.T, w *world, date string) {
		t.Helper()
		cash := accounts.Account{Code: "CASH1", Name: "Carteira", Color: "green",
			Currency: "BRL", Balance: 15000}
		if err := accounts.NewStore(w.conn).Create(&cash); err != nil {
			t.Fatal(err)
		}
		tr := transactions.Transfer{
			Title: "Transferência", Date: date,
			From: transactions.Ref{ID: w.inter.ID}, To: transactions.Ref{ID: cash.ID},
			FromValue: 50000, ToValue: 50000,
		}
		if err := transactions.NewStore(w.conn).Transfer(&tr); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("a transfer is in neither in nor out", func(t *testing.T) {
		w := newWorld(t)
		transfer(t, w, "2026-08-13")

		s, err := Collect(w.conn, Period{From: "2026-08-13", To: "2026-08-13"}, on("2026-08-13"))
		if err != nil {
			t.Fatal(err)
		}
		if s.In["BRL"] != 0 {
			t.Errorf("in = %d; want the arriving leg not counted as income", s.In["BRL"])
		}
		if s.Out["BRL"] != 0 {
			t.Errorf("out = %d; want the leaving leg not counted as spending", s.Out["BRL"])
		}
	})

	t.Run("a transfer is not month-to-date spending either", func(t *testing.T) {
		w := newWorld(t)
		transfer(t, w, "2026-08-04")

		s, err := Collect(w.conn, Period{From: "2026-08-13", To: "2026-08-13"}, on("2026-08-13"))
		if err != nil {
			t.Fatal(err)
		}
		if s.MTD["BRL"] != 0 {
			t.Errorf("month to date = %d; want a transfer left out of it", s.MTD["BRL"])
		}
	})

	t.Run("both legs are still listed", func(t *testing.T) {
		w := newWorld(t)
		transfer(t, w, "2026-08-13")

		s, err := Collect(w.conn, Period{From: "2026-08-13", To: "2026-08-13"}, on("2026-08-13"))
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Ledger) != 2 {
			t.Fatalf("ledger has %d rows; want both legs, since both are real movements", len(s.Ledger))
		}
	})

	t.Run("real money either side of it still counts", func(t *testing.T) {
		w := newWorld(t)
		transfer(t, w, "2026-08-13")
		w.spend(t, "2026-08-13", 8400)

		s, err := Collect(w.conn, Period{From: "2026-08-13", To: "2026-08-13"}, on("2026-08-13"))
		if err != nil {
			t.Fatal(err)
		}
		if s.Out["BRL"] != 8400 {
			t.Fatalf("out = %d; want only the R$84.00 that really left", s.Out["BRL"])
		}
	})
}
