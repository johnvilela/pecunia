package budgets

import (
	"strings"
	"testing"
	"time"
)

// rendered is what a lipgloss string says once the escapes stop mattering.
// Every assertion here is a substring, because the ANSI around the text changes
// with the terminal profile and the text does not.
func has(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("rendered output does not contain %q:\n%s", want, got)
	}
}

func hasNot(t *testing.T, got, want string) {
	t.Helper()
	if strings.Contains(got, want) {
		t.Fatalf("rendered output contains %q and should not:\n%s", want, got)
	}
}

func TestTable(t *testing.T) {
	t.Run("a budget reads as its cap, its spend and where it stands", func(t *testing.T) {
		got := Table([]Budget{food()}, on(20))
		has(t, got, "Food")
		has(t, got, "R$540.00")
		has(t, got, "R$800.00")
		has(t, got, "R$260.00")
		// Spent past the pace on the 20th, still inside the cap.
		has(t, got, StatusAhead)
	})

	t.Run("an over budget says so and says by how much", func(t *testing.T) {
		b := food()
		b.Spent = 87000
		got := Table([]Budget{b}, on(20))
		has(t, got, StatusOver)
		has(t, got, "R$70.00")
	})

	t.Run("a quiet budget is on track", func(t *testing.T) {
		b := food()
		b.Spent = 10000
		got := Table([]Budget{b}, on(20))
		has(t, got, StatusOnTrack)
	})

	t.Run("an archived budget reads archived, not on track or over", func(t *testing.T) {
		b := food()
		b.Spent, b.Active = 95000, false
		got := Table([]Budget{b}, on(20))
		has(t, got, StatusArchived)
		// What it spent against its cap is still a fact worth printing, so
		// "R$150.00 over" stays in the LEFT column. It is the verdict on the
		// month that must not be there.
		hasNot(t, got, StatusOnTrack)
		hasNot(t, got, StatusAhead)
	})

	t.Run("the header is there whatever the rows", func(t *testing.T) {
		got := Table([]Budget{food()}, on(20))
		has(t, got, "BUDGET")
		has(t, got, "SPENT")
	})
}

func TestDetails(t *testing.T) {
	t.Run("the card carries the cap, the spend and what is left", func(t *testing.T) {
		got := Details(food(), nil, nil, on(20))
		has(t, got, "Food")
		has(t, got, "groceries and eating out")
		has(t, got, "R$540.00")
		has(t, got, "R$800.00")
		has(t, got, "R$260.00")
		has(t, got, "left")
	})

	t.Run("the month it is for is named", func(t *testing.T) {
		has(t, Details(food(), nil, nil, on(20)), "August 2026")
	})

	t.Run("being ahead of the month is said in money, not in percent alone", func(t *testing.T) {
		// Pace on the 20th is R$516.12 and R$540.00 has gone.
		has(t, Details(food(), nil, nil, on(20)), "R$23.88")
	})

	t.Run("being under the month is said the other way round", func(t *testing.T) {
		b := food()
		b.Spent = 40000
		got := Details(b, nil, nil, on(20))
		has(t, got, "R$116.12")
		has(t, got, "under")
	})

	t.Run("past the cap is said as how far past", func(t *testing.T) {
		b := food()
		b.Spent = 87000
		got := Details(b, nil, nil, on(20))
		has(t, got, "R$70.00")
		has(t, got, "over")
	})

	t.Run("the amount history is shown when there is one", func(t *testing.T) {
		log := []AmountChange{
			{Previous: 80000, Amount: 95000, Note: "rice went up", CreatedAt: "2026-08-02 09:12:00"},
		}
		got := Details(food(), log, nil, on(20))
		has(t, got, "rice went up")
		has(t, got, "R$950.00")
		// The day only — a cap does not move twice in an afternoon.
		has(t, got, "2026-08-02")
		hasNot(t, got, "09:12:00")
	})

	t.Run("no history means no divider and no heading", func(t *testing.T) {
		hasNot(t, Details(food(), nil, nil, on(20)), "updates")
	})

	t.Run("past months are shown when they are asked for", func(t *testing.T) {
		history := []CycleSpend{
			{Cycle: "2026-06", Spent: 70000},
			{Cycle: "2026-07", Spent: 82000},
			{Cycle: "2026-08", Spent: 54000},
		}
		got := Details(food(), nil, history, on(20))
		has(t, got, "R$700.00")
		has(t, got, "R$820.00")
		// A month that went over the cap is the whole reason to look back.
		has(t, got, "2026-07")
	})

	t.Run("an archived budget is not paced against a month nobody is watching", func(t *testing.T) {
		b := food()
		b.Active = false
		got := Details(b, nil, nil, on(20))
		has(t, got, StatusArchived)
		hasNot(t, got, "ahead of the month")
		hasNot(t, got, "under the month")
	})
}

func TestLabel(t *testing.T) {
	got := Label(food())
	has(t, got, "Food")
	has(t, got, "R$800.00")
}

// The bar is the one thing that must not mislead: a budget past its cap reads
// full rather than overflowing the column it is drawn in.
func TestBar(t *testing.T) {
	cases := []struct {
		name  string
		spent int64
		want  int // filled cells
	}{
		{"nothing spent is empty", 0, 0},
		{"half the cap is half", 40000, barWidth / 2},
		{"the whole cap is full", 80000, barWidth},
		{"past the cap is still full, not longer", 200000, barWidth},
		{"refunds past zero read empty rather than negative", -5000, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := food()
			b.Spent = tc.spent
			got := bar(b, on(20))
			if n := strings.Count(got, "█"); n != tc.want {
				t.Fatalf("bar filled %d cells; want %d", n, tc.want)
			}
			if total := strings.Count(got, "█") + strings.Count(got, "░"); total != barWidth {
				t.Fatalf("bar is %d cells wide; want %d", total, barWidth)
			}
		})
	}

}

func TestNothingYet(t *testing.T) {
	if got := Table(nil, time.Now()); got != "" {
		t.Fatalf("Table(nil) = %q; want nothing at all, so the caller can say why", got)
	}
}
