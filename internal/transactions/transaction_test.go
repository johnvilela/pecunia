package transactions

import (
	"strings"
	"testing"
)

// onAccount is the shape most cases start from: a valid outcome filed against an
// account, which each case then breaks in exactly one way.
func onAccount() Transaction {
	return Transaction{
		Title:    "Groceries",
		Value:    12000,
		Kind:     KindOutcome,
		Date:     "2026-08-08",
		Account:  Ref{ID: 1, Code: "INTER", Name: "Banco Inter", Color: "orange"},
		Currency: "BRL",
	}
}

func onCard() Transaction {
	t := onAccount()
	t.Account = Ref{}
	t.Card = Ref{ID: 1, Code: "NUCRD", Name: "Nubank", Color: "violet"}
	return t
}

func TestSigned(t *testing.T) {
	cases := []struct {
		name         string
		kind         string
		wantAccount  int64
		wantCardMove int64
	}{
		// An account holds money, so spending lowers it.
		{"outcome", KindOutcome, -12000, 12000},
		// A card holds debt, so spending raises it and paying lowers it.
		{"income", KindIncome, 12000, -12000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx := onAccount()
			tx.Kind = tc.kind
			if got := tx.Signed(); got != tc.wantAccount {
				t.Fatalf("Signed() = %d; want %d", got, tc.wantAccount)
			}
			if got := tx.CardDelta(); got != tc.wantCardMove {
				t.Fatalf("CardDelta() = %d; want %d", got, tc.wantCardMove)
			}
		})
	}
}

func TestTarget(t *testing.T) {
	t.Run("an account transaction targets its account", func(t *testing.T) {
		tx := onAccount()
		if tx.IsCard() {
			t.Fatal("IsCard() = true on an account transaction")
		}
		if got := tx.Target().Code; got != "INTER" {
			t.Fatalf("Target().Code = %q; want INTER", got)
		}
	})

	t.Run("a card transaction targets its card", func(t *testing.T) {
		tx := onCard()
		if !tx.IsCard() {
			t.Fatal("IsCard() = false on a card transaction")
		}
		if got := tx.Target().Code; got != "NUCRD" {
			t.Fatalf("Target().Code = %q; want NUCRD", got)
		}
	})
}

func TestAmount(t *testing.T) {
	tx := onAccount()
	if got := tx.Amount(); got != "120.00" {
		t.Fatalf("Amount() = %q; want 120.00 at BRL's two places", got)
	}
	tx.Currency = "BTC"
	if got := tx.Amount(); got != "0.00012000" {
		t.Fatalf("Amount() = %q; want eight places for BTC", got)
	}
}

func TestValidate(t *testing.T) {
	t.Run("a well-formed transaction passes", func(t *testing.T) {
		if err := onAccount().Validate(); err != nil {
			t.Fatalf("Validate() = %v; want nil", err)
		}
		if err := onCard().Validate(); err != nil {
			t.Fatalf("Validate() on a card = %v; want nil", err)
		}
	})

	cases := []struct {
		name   string
		break_ func(*Transaction)
		want   string
	}{
		{"a blank title", func(t *Transaction) { t.Title = "" }, "title is required"},
		{"a whitespace title", func(t *Transaction) { t.Title = "  \t " }, "title is required"},
		{"a zero value", func(t *Transaction) { t.Value = 0 }, "amount"},
		{"a negative value", func(t *Transaction) { t.Value = -1 }, "amount"},
		{"an unknown kind", func(t *Transaction) { t.Kind = "refund" }, "income or outcome"},
		{"a blank kind", func(t *Transaction) { t.Kind = "" }, "income or outcome"},
		{"a malformed date", func(t *Transaction) { t.Date = "08/08/2026" }, "date"},
		{"a blank date", func(t *Transaction) { t.Date = "" }, "date"},
		{"no target", func(t *Transaction) { t.Account = Ref{} }, "account or a credit card"},
		{"both targets", func(t *Transaction) { t.Card = Ref{ID: 2} }, "account or a credit card"},
		{"six tags", func(t *Transaction) { t.Tags = []string{"a", "b", "c", "d", "e", "f"} }, "at most 5 tags"},
	}
	for _, tc := range cases {
		t.Run(tc.name+" is rejected", func(t *testing.T) {
			tx := onAccount()
			tc.break_(&tx)
			err := tx.Validate()
			if err == nil {
				t.Fatalf("Validate() with %s = nil; want an error", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() with %s = %q; want it to mention %q", tc.name, err, tc.want)
			}
		})
	}

	t.Run("five tags are allowed", func(t *testing.T) {
		tx := onAccount()
		tx.Tags = []string{"a", "b", "c", "d", "e"}
		if err := tx.Validate(); err != nil {
			t.Fatalf("Validate() with five tags = %v; want nil", err)
		}
	})
}

func TestNormalizeTags(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"trims and lowercases", []string{" Food ", "WORK"}, []string{"food", "work"}},
		{"drops blanks", []string{"food", "", "   "}, []string{"food"}},
		{"dedupes across case", []string{"Food", "food", "FOOD"}, []string{"food"}},
		{"sorts, so two equal sets compare equal", []string{"work", "food"}, []string{"food", "work"}},
		{"drops the commas that would split a tag in two", []string{"food,work"}, []string{"foodwork"}},
		{"nothing at all stays nothing", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeTags(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("NormalizeTags(%q) = %q; want %q", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("NormalizeTags(%q) = %q; want %q", tc.in, got, tc.want)
				}
			}
		})
	}
}

func TestParseTags(t *testing.T) {
	got := ParseTags(" Food, work ,, FOOD ")
	want := []string{"food", "work"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ParseTags = %q; want %q", got, want)
	}
	if got := ParseTags(""); got != nil {
		t.Fatalf("ParseTags(\"\") = %q; want nothing", got)
	}
}

func TestParseDate(t *testing.T) {
	t.Run("accepts and returns the canonical form", func(t *testing.T) {
		got, err := ParseDate(" 2026-08-08 ")
		if err != nil {
			t.Fatal(err)
		}
		if got != "2026-08-08" {
			t.Fatalf("ParseDate = %q; want 2026-08-08", got)
		}
	})

	for _, in := range []string{"", "08/08/2026", "2026-8-8", "2026-02-30", "yesterday"} {
		t.Run("rejects "+in, func(t *testing.T) {
			if _, err := ParseDate(in); err == nil {
				t.Fatalf("ParseDate(%q) = nil; want an error", in)
			}
		})
	}
}

func TestToday(t *testing.T) {
	got, err := ParseDate(Today())
	if err != nil {
		t.Fatalf("Today() = %q, which ParseDate rejects: %v", Today(), err)
	}
	if got != Today() {
		t.Fatalf("Today() = %q; want it already canonical", Today())
	}
}
