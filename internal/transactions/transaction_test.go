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

func TestSplitInstallments(t *testing.T) {
	cases := []struct {
		name  string
		total int64
		n     int
		want  []int64
	}{
		{"a clean split", 100000, 5, []int64{20000, 20000, 20000, 20000, 20000}},
		{"one installment is the whole thing", 100000, 1, []int64{100000}},
		// The odd cents go on the first one: it is the one that has already been
		// agreed, and the later ones are the round number you expect to see.
		{"the remainder rides on the first", 100000, 3, []int64{33334, 33333, 33333}},
		{"a single cent over", 100003, 2, []int64{50002, 50001}},
		{"less money than installments", 3, 5, []int64{3, 0, 0, 0, 0}},
		{"eight decimal places", 100000001, 3, []int64{33333335, 33333333, 33333333}},
		{"zero is not a count", 5000, 0, []int64{5000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitInstallments(tc.total, tc.n)
			if len(got) != len(tc.want) {
				t.Fatalf("SplitInstallments(%d, %d) = %v; want %v", tc.total, tc.n, got, tc.want)
			}
			var sum int64
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("SplitInstallments(%d, %d) = %v; want %v", tc.total, tc.n, got, tc.want)
				}
				sum += got[i]
			}
			// The property that matters more than the shares themselves: nothing
			// is invented and nothing goes missing.
			if sum != tc.total {
				t.Fatalf("the parts of %d sum to %d", tc.total, sum)
			}
		})
	}
}

func TestValidateInstallments(t *testing.T) {
	card := func(count int64) Transaction {
		return Transaction{Title: "Phone", Value: 100000, Kind: KindOutcome, Date: "2026-08-14",
			Card: Ref{ID: 1}, Installment: Installment{Seq: 1, Count: count}}
	}

	t.Run("a card series is fine", func(t *testing.T) {
		if err := card(5).Validate(); err != nil {
			t.Fatalf("a 5x card purchase was refused: %v", err)
		}
	})

	t.Run("an account cannot be split", func(t *testing.T) {
		// Only a credit card has bills to spread a purchase over.
		tr := card(5)
		tr.Card, tr.Account = Ref{}, Ref{ID: 1}
		err := tr.Validate()
		if err == nil {
			t.Fatal("an account transaction was split into installments")
		}
		if !strings.Contains(err.Error(), "credit card") {
			t.Errorf("error %q does not say why", err)
		}
	})

	t.Run("refuses more than the cap", func(t *testing.T) {
		if err := card(MaxInstallments + 1).Validate(); err == nil {
			t.Fatalf("%d installments were accepted", MaxInstallments+1)
		}
	})

	t.Run("refuses a negative count", func(t *testing.T) {
		if err := card(-1).Validate(); err == nil {
			t.Fatal("a negative installment count was accepted")
		}
	})
}

func TestValidatePayment(t *testing.T) {
	pay := func() Transaction {
		return Transaction{Title: "Bill NUCRD", Value: 89050, Kind: KindOutcome,
			Date: "2026-08-20", Account: Ref{ID: 1}, PaysBillID: 7}
	}

	t.Run("an account outcome is what pays a bill", func(t *testing.T) {
		if err := pay().Validate(); err != nil {
			t.Fatalf("a payment was refused: %v", err)
		}
	})

	t.Run("a card cannot pay its own bill", func(t *testing.T) {
		// The money has to come from somewhere; a card paying itself is a loop.
		tr := pay()
		tr.Account, tr.Card = Ref{}, Ref{ID: 1}
		if err := tr.Validate(); err == nil {
			t.Fatal("a card transaction was allowed to pay a bill")
		}
	})

	t.Run("an income cannot pay a bill", func(t *testing.T) {
		tr := pay()
		tr.Kind = KindIncome
		if err := tr.Validate(); err == nil {
			t.Fatal("an income was allowed to pay a bill")
		}
	})
}

func TestIsInstallment(t *testing.T) {
	cases := []struct {
		count int64
		want  bool
	}{{0, false}, {1, false}, {2, true}, {5, true}}
	for _, tc := range cases {
		tr := Transaction{Installment: Installment{Count: tc.count}}
		if got := tr.IsInstallment(); got != tc.want {
			t.Errorf("count %d: IsInstallment() = %v; want %v", tc.count, got, tc.want)
		}
	}
}
