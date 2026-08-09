package transactions

import (
	"strings"
	"testing"
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
