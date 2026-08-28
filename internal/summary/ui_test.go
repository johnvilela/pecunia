package summary

import (
	"strings"
	"testing"
	"time"

	"pecunia/internal/accounts"
	"pecunia/internal/bills"
	"pecunia/internal/budgets"
	"pecunia/internal/cards"
	"pecunia/internal/goals"
	"pecunia/internal/recurring"
	"pecunia/internal/transactions"
)

// on is the day a case is rendered against. Nothing here may ask the wall clock
// what today is, or the suite is a different suite every morning.
func on(s string) time.Time {
	d, err := time.Parse(transactions.DateLayout, s)
	if err != nil {
		panic(err)
	}
	return d
}

func inter() accounts.Account {
	return accounts.Account{ID: 1, Code: "INTER", Name: "Inter", Color: "orange",
		Balance: 482350, Currency: "BRL"}
}

func nubank() cards.Card {
	return cards.Card{ID: 1, Code: "NUCRD", Name: "Nubank", Color: "purple",
		Limit: 500000, Balance: 120000, Currency: "BRL", ClosingDay: 20, DueDay: 28}
}

// energy opened on the 5th and was due on the 10th, so it is overdue on the
// 13th — the day every case below renders against.
func energy() recurring.Bill {
	return recurring.Bill{ID: 1, Code: "ENERG", Name: "Energia", Color: "yellow",
		Expected: 21490, OpenDay: 5, DueDay: 10, Active: true, Currency: "BRL",
		CreatedAt: "2026-08-01 09:00:00"}
}

func statement() bills.Bill {
	return bills.Bill{ID: 1, ClosesOn: "2026-07-20", DueOn: "2026-07-28",
		Total: 98000, Status: bills.StatusClosed, Card: nubank()}
}

func notebook() goals.Goal {
	return goals.Goal{ID: 1, Name: "Notebook novo", Target: 300000,
		Currency: "BRL", Kind: goals.KindSaving, Net: 120000}
}

// day is the full screen: a day with money moving both ways, something late,
// something landing next week, and every list behind it filled in.
func day() Summary {
	return Summary{
		Period: Period{From: "2026-08-13", To: "2026-08-13"},
		Title:  "Thursday, 13 August 2026",
		Today:  on("2026-08-13"),
		Ledger: []transactions.Transaction{
			{ID: 2, Title: "Salário", Date: "2026-08-13", Value: 120000,
				Kind: transactions.KindIncome, Currency: "BRL"},
			{ID: 1, Title: "Feira", Date: "2026-08-13", Value: 84000,
				Kind: transactions.KindOutcome, Currency: "BRL"},
		},
		In:       map[string]int64{"BRL": 120000, "USD": 4000},
		Out:      map[string]int64{"BRL": 84000},
		MTD:      map[string]int64{"BRL": 641230},
		Due:      Board{Bills: []recurring.Bill{energy()}, Statements: []bills.Bill{statement()}},
		Soon:     Board{Bills: []recurring.Bill{rent()}},
		Accounts: []accounts.Account{inter()},
		Cards:    []cards.Card{nubank()},
		Goals:    []goals.Goal{notebook()},
	}
}

// rent opens on the 18th, five days after the day every case renders against.
func rent() recurring.Bill {
	b := energy()
	b.ID, b.Code, b.Name = 2, "RENTX", "Aluguel"
	b.Expected, b.OpenDay, b.DueDay = 180000, 18, 25
	return b
}

func TestNet(t *testing.T) {
	t.Run("nets each currency on its own", func(t *testing.T) {
		got := net(day())
		if got["BRL"] != 36000 {
			t.Errorf("net BRL = %d; want 120000 - 84000", got["BRL"])
		}
	})

	t.Run("keeps a currency that only came in", func(t *testing.T) {
		// USD earned and nothing spent: iterating the outcome map alone loses it.
		if got := net(day()); got["USD"] != 4000 {
			t.Errorf("net USD = %d; want the income-only currency kept", got["USD"])
		}
	})

	t.Run("keeps a currency that only went out", func(t *testing.T) {
		s := day()
		s.In = nil
		if got := net(s); got["BRL"] != -84000 {
			t.Errorf("net BRL = %d; want the outcome-only currency kept", got["BRL"])
		}
	})
}

func TestRender(t *testing.T) {
	out := Render(day())

	t.Run("names the day it is for", func(t *testing.T) {
		if !strings.Contains(out, "Thursday, 13 August 2026") {
			t.Errorf("summary does not say what day it is for:\n%s", out)
		}
	})

	t.Run("totals what came in and what went out", func(t *testing.T) {
		for _, want := range []string{"R$1200.00", "$40.00", "R$840.00"} {
			if !strings.Contains(out, want) {
				t.Errorf("summary is missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("nets the day out", func(t *testing.T) {
		if !strings.Contains(out, "R$360.00") {
			t.Errorf("summary does not net the day out:\n%s", out)
		}
	})

	t.Run("says what the month has cost so far", func(t *testing.T) {
		for _, want := range []string{"month", "R$6412.30"} {
			if !strings.Contains(out, want) {
				t.Errorf("summary is missing %q from the month so far:\n%s", want, out)
			}
		}
	})

	t.Run("leaves the month so far out of a month summary", func(t *testing.T) {
		s := day()
		s.Period, s.Title, s.MTD = Period{From: "2026-08-01", To: "2026-08-31"}, "August 2026", nil
		if got := Render(s); strings.Contains(got, "month") {
			t.Errorf("a month summary repeated itself as a month-to-date line:\n%s", got)
		}
	})

	t.Run("lists the transactions", func(t *testing.T) {
		for _, want := range []string{"TRANSACTIONS", "Salário", "Feira"} {
			if !strings.Contains(out, want) {
				t.Errorf("summary is missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("says so when nothing was recorded", func(t *testing.T) {
		s := day()
		s.Ledger = nil
		got := Render(s)
		if !strings.Contains(got, "no transactions") {
			t.Errorf("an empty day does not say so:\n%s", got)
		}
		if strings.Contains(got, "DATE") {
			t.Errorf("an empty day rendered a table with no rows:\n%s", got)
		}
	})

	t.Run("puts what is due where it is seen first", func(t *testing.T) {
		dueAt, txAt := strings.Index(out, "DUE"), strings.Index(out, "TRANSACTIONS")
		if dueAt == -1 || txAt == -1 {
			t.Fatalf("summary is missing a section:\n%s", out)
		}
		if dueAt > txAt {
			t.Errorf("what needs paying came after what was spent:\n%s", out)
		}
	})

	t.Run("shows the bill that is late", func(t *testing.T) {
		for _, want := range []string{"ENERG", "overdue"} {
			if !strings.Contains(out, want) {
				t.Errorf("summary is missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("shows the card statement that is owed", func(t *testing.T) {
		if !strings.Contains(out, "NUCRD") {
			t.Errorf("summary never names the card with a statement owing:\n%s", out)
		}
	})

	t.Run("says so when nothing is due", func(t *testing.T) {
		s := day()
		s.Due = Board{}
		got := Render(s)
		if !strings.Contains(got, "nothing due") {
			t.Errorf("a clear day does not say so:\n%s", got)
		}
		if strings.Contains(got, "no bills yet") {
			t.Errorf("an empty due section borrowed the empty-board sentence:\n%s", got)
		}
	})

	t.Run("says nothing about what is due in a window that is over", func(t *testing.T) {
		// Nothing was read, so there is nothing to report — and "nothing due"
		// under a month that ended in March is a claim about today that this
		// screen never checked.
		s := day()
		s.Period, s.Title = Period{From: "2026-03-01", To: "2026-03-31"}, "March 2026"
		s.Due, s.Soon = Board{}, Board{}
		got := Render(s)
		if strings.Contains(got, "nothing due") {
			t.Errorf("a window that is over claimed nothing was due:\n%s", got)
		}
		// A heading never appears without a body, so an empty due section is
		// the whole section gone. Anchored on the transactions heading because
		// the cards table carries a CLOSE/DUE column of its own.
		if before, _, _ := strings.Cut(got, "TRANSACTIONS"); strings.Contains(before, "DUE") {
			t.Errorf("a window that is over still printed the due heading:\n%s", got)
		}
	})

	t.Run("shows what lands in the next seven days", func(t *testing.T) {
		for _, want := range []string{"NEXT 7 DAYS", "RENTX"} {
			if !strings.Contains(out, want) {
				t.Errorf("summary is missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("leaves the next seven days out when nothing lands", func(t *testing.T) {
		s := day()
		s.Soon = Board{}
		if got := Render(s); strings.Contains(got, "NEXT 7 DAYS") {
			t.Errorf("an empty week still printed its heading:\n%s", got)
		}
	})

	t.Run("shows the balances", func(t *testing.T) {
		for _, want := range []string{"BALANCES", "INTER", "R$4823.50", "NUCRD"} {
			if !strings.Contains(out, want) {
				t.Errorf("summary is missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("leaves the balances out when there are none", func(t *testing.T) {
		s := day()
		s.Accounts, s.Cards = nil, nil
		if got := Render(s); strings.Contains(got, "BALANCES") {
			t.Errorf("an empty balances section still printed its heading:\n%s", got)
		}
	})

	t.Run("shows where the goals stand", func(t *testing.T) {
		for _, want := range []string{"GOALS", "Notebook novo", "R$1200.00"} {
			if !strings.Contains(out, want) {
				t.Errorf("summary is missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("leaves the goals out when there are none", func(t *testing.T) {
		s := day()
		s.Goals = nil
		if got := Render(s); strings.Contains(got, "GOALS") {
			t.Errorf("an empty goals section still printed its heading:\n%s", got)
		}
	})

	t.Run("points a fresh database at the command that fills it", func(t *testing.T) {
		fresh := Summary{
			Period: Period{From: "2026-08-13", To: "2026-08-13"},
			Title:  "Thursday, 13 August 2026",
			Today:  on("2026-08-13"),
		}
		got := Render(fresh)
		if !strings.Contains(got, "pecunia ac n") {
			t.Errorf("an empty database is not told where to start:\n%s", got)
		}
		if strings.Contains(got, "TRANSACTIONS") {
			t.Errorf("an empty database rendered the whole screen anyway:\n%s", got)
		}
	})
}

func foodBudget() budgets.Budget {
	return budgets.Budget{ID: 1, Code: "FOOD1", Name: "Food", Amount: 80000,
		Currency: "BRL", Color: "green", Active: true, Cycle: "2026-08", Spent: 54000,
		Category: transactions.Ref{ID: 1, Code: "FOODC", Name: "Food"}}
}

func TestRenderBudgets(t *testing.T) {
	t.Run("the budgets get their own section", func(t *testing.T) {
		got := Render(Summary{
			Title: "Thursday, 13 August 2026", Today: on("2026-08-13"),
			Period:  Period{From: "2026-08-13", To: "2026-08-13"},
			Budgets: []budgets.Budget{foodBudget()},
		})
		for _, want := range []string{"BUDGETS", "Food", "R$540.00", "R$800.00"} {
			if !strings.Contains(got, want) {
				t.Errorf("the budgets section is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("no budgets means no section at all", func(t *testing.T) {
		got := Render(Summary{
			Title: "Thursday, 13 August 2026", Today: on("2026-08-13"),
			Period:   Period{From: "2026-08-13", To: "2026-08-13"},
			Accounts: []accounts.Account{inter()},
		})
		if strings.Contains(got, "BUDGETS") {
			t.Errorf("an empty budgets section was printed:\n%s", got)
		}
	})
}
