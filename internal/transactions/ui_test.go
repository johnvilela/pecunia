package transactions

import (
	"strings"
	"testing"

	"pecunia/internal/goals"
)

// shown is what a rendered transaction looks like once the joins have filled it
// in — the state every view actually receives.
func shown() Transaction {
	return Transaction{
		ID:          7,
		Title:       "Groceries",
		Description: "supermarket runs",
		Value:       12000,
		Kind:        KindOutcome,
		Date:        "2026-08-08",
		Tags:        []string{"food", "weekly"},
		Category:    Ref{ID: 3, Code: "FOOD1", Name: "Food & Groceries", Color: "lime"},
		Account:     Ref{ID: 1, Code: "INTER", Name: "Banco Inter", Color: "orange"},
		Currency:    "BRL",
		CreatedAt:   "2026-08-08 10:00:00",
		UpdatedAt:   "2026-08-09 11:00:00",
	}
}

// contains asserts on substrings, not whole output: the ANSI escapes lipgloss
// wraps text in change with the terminal profile, the text does not.
func contains(t *testing.T, what, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Fatalf("%s is missing %q:\n%s", what, w, got)
		}
	}
}

func TestAmountRender(t *testing.T) {
	t.Run("an outcome reads as money leaving", func(t *testing.T) {
		contains(t, "Amount", Amount(shown()), "-", "R$", "120.00")
	})

	t.Run("an income reads as money arriving", func(t *testing.T) {
		tr := shown()
		tr.Kind = KindIncome
		contains(t, "Amount", Amount(tr), "+", "R$", "120.00")
	})

	t.Run("the currency sets the precision", func(t *testing.T) {
		tr := shown()
		tr.Currency = "BTC"
		contains(t, "Amount", Amount(tr), "₿", "0.00012000")
	})
}

func TestLabelRender(t *testing.T) {
	contains(t, "Label", Label(shown()), "2026-08-08", "Groceries")
}

func TestTable(t *testing.T) {
	t.Run("shows every column and every row", func(t *testing.T) {
		second := shown()
		second.ID, second.Title, second.Kind, second.Date = 8, "Salary", KindIncome, "2026-08-01"
		second.Category = Ref{ID: 4, Code: "SLRY1", Name: "Salary", Color: "green"}

		got := Table([]Transaction{shown(), second})
		contains(t, "the table", got,
			"DATE", "TITLE", "CATEGORY", "SOURCE", "AMOUNT",
			"2026-08-08", "Groceries", "FOOD1", "INTER", "120.00",
			"2026-08-01", "Salary", "SLRY1")
	})

	t.Run("a transaction with no category still renders", func(t *testing.T) {
		tr := shown()
		tr.Category = Ref{}
		contains(t, "the table", Table([]Transaction{tr}), "Groceries", "INTER")
	})

	t.Run("a card transaction shows the card as its source", func(t *testing.T) {
		tr := shown()
		tr.Account = Ref{}
		tr.Card = Ref{ID: 2, Code: "NUCRD", Name: "Nubank", Color: "violet"}
		contains(t, "the table", Table([]Transaction{tr}), "NUCRD")
	})
}

func TestDetails(t *testing.T) {
	t.Run("says everything the table had no room for", func(t *testing.T) {
		contains(t, "the details card", Details(shown()),
			"Groceries", "supermarket runs", "120.00", "2026-08-08",
			"FOOD1", "Food & Groceries", "INTER", "Banco Inter",
			"food", "weekly", createdIcon, updatedIcon)
	})

	t.Run("names what kind of source it is", func(t *testing.T) {
		contains(t, "the details card", Details(shown()), "account")

		tr := shown()
		tr.Account = Ref{}
		tr.Card = Ref{ID: 2, Code: "NUCRD", Name: "Nubank", Color: "violet"}
		contains(t, "the details card", Details(tr), "credit card")
	})

	t.Run("drops the lines it has nothing for", func(t *testing.T) {
		tr := shown()
		tr.Description, tr.Tags, tr.Category = "", nil, Ref{}
		got := Details(tr)
		for _, gone := range []string{"supermarket runs", "FOOD1", "#food"} {
			if strings.Contains(got, gone) {
				t.Fatalf("the details card still shows %q with nothing to show:\n%s", gone, got)
			}
		}
		contains(t, "the details card", got, "Groceries", "120.00")
	})

	t.Run("an unsaved transaction has no timestamps to show", func(t *testing.T) {
		tr := shown()
		tr.CreatedAt, tr.UpdatedAt = "", ""
		if strings.Contains(Details(tr), createdIcon) {
			t.Fatal("the details card shows a created icon with no timestamp")
		}
	})
}

func TestPickerRow(t *testing.T) {
	row := pickerRow(shown())
	contains(t, "the picker row", row.Label, "2026-08-08", "Groceries")
	contains(t, "the picker row", row.Desc, "120.00", "INTER")
	// The filter is what typing in the picker matches against, so everything a
	// person might remember the transaction by belongs in it.
	contains(t, "the picker row", row.Filter, "Groceries", "food", "weekly", "FOOD1", "INTER")
}

func TestSourceOptions(t *testing.T) {
	t.Run("an account and a card are told apart", func(t *testing.T) {
		acc, err := parseSource(sourceValue("account", 12))
		if err != nil {
			t.Fatal(err)
		}
		if acc.IsCard || acc.ID != 12 {
			t.Fatalf("parseSource = %+v; want account 12", acc)
		}
		card, err := parseSource(sourceValue("card", 3))
		if err != nil {
			t.Fatal(err)
		}
		if !card.IsCard || card.ID != 3 {
			t.Fatalf("parseSource = %+v; want card 3", card)
		}
	})

	t.Run("nonsense is refused rather than guessed at", func(t *testing.T) {
		for _, in := range []string{"", "12", "wallet:12", "account:", "account:abc"} {
			if _, err := parseSource(in); err == nil {
				t.Fatalf("parseSource(%q) = nil; want an error", in)
			}
		}
	})
}

func TestInstallmentRender(t *testing.T) {
	one := func() Transaction {
		tr := shown()
		tr.Title = "Phone"
		tr.Card = Ref{ID: 2, Code: "NUCRD", Name: "Nubank", Color: "violet"}
		tr.Account = Ref{}
		tr.Installment = Installment{Group: 101, Seq: 3, Count: 5}
		return tr
	}

	t.Run("a series row says where it sits", func(t *testing.T) {
		contains(t, "Label", Label(one()), "Phone", "(3/5)")
		contains(t, "Table", Table([]Transaction{one()}), "Phone", "(3/5)")
		contains(t, "Details", Details(one()), "Phone", "(3/5)")
	})

	t.Run("an ordinary transaction says nothing", func(t *testing.T) {
		got := Label(shown())
		if strings.Contains(got, "/") && strings.Contains(got, "(") {
			t.Errorf("a plain transaction was rendered as a series: %q", got)
		}
	})

	t.Run("the title itself is left alone", func(t *testing.T) {
		// The position lives in its own column, so an edit never has to strip
		// "(3/5)" back out of what the user typed.
		if one().Title != "Phone" {
			t.Errorf("the title carries the position: %q", one().Title)
		}
	})
}

func TestDetailsNamesTheBillItPays(t *testing.T) {
	tr := shown()
	tr.Title = "Bill payment"
	tr.PaysBillID = 7

	got := Details(tr)
	contains(t, "Details", got, "bill")
}

func TestDetailsNamesTheGoalItFeeds(t *testing.T) {
	t.Run("says which goal", func(t *testing.T) {
		tr := shown()
		tr.Goal = Ref{ID: 3, Name: "New laptop"}
		contains(t, "Details", Details(tr), "New laptop")
	})

	t.Run("leaves the line out when there is none", func(t *testing.T) {
		if got := Details(shown()); strings.Contains(got, "goal") {
			t.Fatalf("the details card talks about a goal it does not have:\n%s", got)
		}
	})
}

func TestGoalOptions(t *testing.T) {
	d := FormData{Goals: []goals.Goal{
		{ID: 1, Name: "New laptop", Target: 500000, Currency: "BRL", Kind: goals.KindSaving},
		{ID: 2, Name: "Satoshis", Target: 100000000, Currency: "BTC", Kind: goals.KindSaving},
	}}

	t.Run("only the goals in the source's currency are offered", func(t *testing.T) {
		opts := d.goalOptions("BRL")
		if len(opts) != 2 {
			t.Fatalf("goalOptions(BRL) = %d options; want the sentinel and the one BRL goal", len(opts))
		}
		if opts[1].Value != 1 {
			t.Errorf("the offered goal is %d; want the BRL one", opts[1].Value)
		}
	})

	t.Run("the none sentinel always comes first", func(t *testing.T) {
		for _, currency := range []string{"BRL", "BTC", "", "ZZZ"} {
			opts := d.goalOptions(currency)
			if len(opts) == 0 || opts[0].Value != 0 {
				t.Fatalf("goalOptions(%q) does not start with the none option", currency)
			}
		}
	})

	t.Run("no goals at all is just the sentinel", func(t *testing.T) {
		if opts := (FormData{}).goalOptions("BRL"); len(opts) != 1 {
			t.Fatalf("goalOptions with no goals = %d options; want only the sentinel", len(opts))
		}
	})
}

func TestGoalCurrency(t *testing.T) {
	d := FormData{Goals: []goals.Goal{
		{ID: 1, Name: "New laptop", Target: 500000, Currency: "BRL", Kind: goals.KindSaving},
	}}
	if got := d.goalCurrency(1); got != "BRL" {
		t.Errorf("goalCurrency(1) = %q; want BRL", got)
	}
	if got := d.goalCurrency(0); got != "" {
		t.Errorf("goalCurrency(0) = %q; want empty — there is no goal", got)
	}
}

func TestTableHasNoGoalColumn(t *testing.T) {
	// A goal column would be empty on nearly every row, which is why the goal
	// lives on the details card instead. This is the decision, so it should
	// break loudly if someone adds one.
	tr := shown()
	tr.Goal = Ref{ID: 3, Name: "New laptop"}
	got := Table([]Transaction{tr})
	if strings.Contains(got, "GOAL") || strings.Contains(got, "New laptop") {
		t.Fatalf("the list table grew a goal column:\n%s", got)
	}
}

// outLeg and inLeg are the two sides of one R$500.00 transfer, as the store
// hands them back with the counterpart joined in.
func outLeg() Transaction {
	return Transaction{
		ID: 42, Title: "Transferência", Value: 50000, Kind: KindOutcome,
		Date: "2026-08-14", Currency: "BRL", TransferGroup: 42,
		Account: Ref{ID: 1, Code: "NUBON", Name: "Nubank", Color: "violet"},
		Counterpart: Counterpart{
			Ref:   Ref{ID: 2, Code: "INTER", Name: "Inter", Color: "orange"},
			Value: 50000, Currency: "BRL",
		},
	}
}

func inLeg() Transaction {
	t := outLeg()
	t.ID, t.Kind = 43, KindIncome
	t.Account, t.Counterpart.Ref = t.Counterpart.Ref, t.Account
	return t
}

func TestTransferRendering(t *testing.T) {
	t.Run("the leaving leg points at where the money went", func(t *testing.T) {
		got := Table([]Transaction{outLeg()})
		for _, want := range []string{"NUBON", "INTER", "→", "R$500.00"} {
			if !strings.Contains(got, want) {
				t.Errorf("the table is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("the arriving leg points back at where it came from", func(t *testing.T) {
		got := Table([]Transaction{inLeg()})
		if !strings.Contains(got, "←") {
			t.Errorf("the arriving leg does not point back at its origin:\n%s", got)
		}
	})

	t.Run("a transfer has no category to show", func(t *testing.T) {
		// The category column is where the arrow goes instead: a transfer never
		// has one, so the column would otherwise be blank on every transfer.
		got := Table([]Transaction{outLeg()})
		if strings.Contains(got, "FOOD1") {
			t.Errorf("a transfer rendered a category:\n%s", got)
		}
	})

	t.Run("the details card names both ends", func(t *testing.T) {
		got := Details(outLeg())
		for _, want := range []string{"NUBON", "INTER", "R$500.00"} {
			if !strings.Contains(got, want) {
				t.Errorf("details is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("a fee is the two legs differing in one currency", func(t *testing.T) {
		leg := outLeg()
		leg.Counterpart.Value = 49500 // R$5.00 taken on the way
		got := Details(leg)
		for _, want := range []string{"R$495.00", "fee", "R$5.00"} {
			if !strings.Contains(got, want) {
				t.Errorf("details does not explain the fee (%q):\n%s", want, got)
			}
		}
	})

	t.Run("a cross-currency transfer shows both amounts and calls it no fee", func(t *testing.T) {
		leg := outLeg()
		leg.Counterpart.Value, leg.Counterpart.Currency = 9200, "USD"
		got := Details(leg)
		if !strings.Contains(got, "$92.00") {
			t.Errorf("details does not show what arrived:\n%s", got)
		}
		// Two currencies cannot be subtracted, so the difference is not a fee
		// and must not be printed as one.
		if strings.Contains(got, "fee") {
			t.Errorf("details called a rate a fee:\n%s", got)
		}
	})

	t.Run("an ordinary transaction is untouched", func(t *testing.T) {
		got := Table([]Transaction{{ID: 1, Title: "Feira", Value: 8400,
			Kind: KindOutcome, Date: "2026-08-14", Currency: "BRL",
			Account:  Ref{ID: 1, Code: "NUBON", Name: "Nubank", Color: "violet"},
			Category: Ref{ID: 1, Code: "FOOD1", Name: "Food", Color: "lime"}}})
		if strings.Contains(got, "→") {
			t.Errorf("an ordinary transaction grew an arrow:\n%s", got)
		}
		if !strings.Contains(got, "FOOD1") {
			t.Errorf("an ordinary transaction lost its category:\n%s", got)
		}
	})
}

func TestAmountRenderAdjustment(t *testing.T) {
	t.Run("a negative adjustment carries one minus, before the symbol", func(t *testing.T) {
		got := Amount(Transaction{Kind: KindAdjustment, Value: -5000, Currency: "BRL"})
		contains(t, "Amount", got, "-", "R$", "50.00")
		if strings.Contains(got, "R$-") {
			t.Fatalf("Amount = %q; the sign leaked into the figure", got)
		}
	})

	t.Run("a positive adjustment reads as money in", func(t *testing.T) {
		contains(t, "Amount", Amount(Transaction{Kind: KindAdjustment, Value: 5000, Currency: "BRL"}),
			"+", "R$", "50.00")
	})
}
