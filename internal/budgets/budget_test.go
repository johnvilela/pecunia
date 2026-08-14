package budgets

import (
	"strings"
	"testing"
	"time"
)

// food is a R$800.00 monthly cap on the food category, R$540.00 spent so far in
// August 2026. Every case builds on a copy, so none of them share state.
func food() Budget {
	return Budget{
		ID: 1, Code: "FOOD1", Name: "Food", Description: "groceries and eating out",
		Amount: 80000, Currency: "BRL", Color: "green", Active: true,
		Cycle: "2026-08", Spent: 54000,
	}
}

// on is a date in the cycle food() is read for, so a case only has to say which
// day of it.
func on(day int) time.Time {
	return time.Date(2026, time.August, day, 12, 0, 0, 0, time.UTC)
}

func TestRemainingAndOver(t *testing.T) {
	cases := []struct {
		name      string
		spent     int64
		remaining int64
		over      bool
	}{
		{"part way through the month", 54000, 26000, false},
		{"nothing spent yet", 0, 80000, false},
		{"one centavo short of the cap", 79999, 1, false},
		{"exactly at the cap", 80000, 0, false},
		{"one centavo past it", 80001, -1, true},
		{"well past it", 95000, -15000, true},
		{"refunds took it negative", -2000, 82000, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := food()
			b.Spent = tc.spent
			if got := b.Remaining(); got != tc.remaining {
				t.Errorf("Remaining() = %d; want %d", got, tc.remaining)
			}
			if got := b.Over(); got != tc.over {
				t.Errorf("Over() = %v; want %v", got, tc.over)
			}
		})
	}
}

// The cap is only reached at the end of the month, so a budget spent exactly to
// its amount on the 1st is very different news from the same figure on the 31st.
// That difference is the whole point of the module.
func TestPace(t *testing.T) {
	cases := []struct {
		name string
		day  int
		want int64
	}{
		// August is 31 days: 80000 * day / 31, integer division throughout.
		{"the first day is one day of it", 1, 2580},
		{"a third of the way in", 10, 25806},
		{"two thirds", 20, 51612},
		{"the last day is the whole cap", 31, 80000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := food().Pace(on(tc.day)); got != tc.want {
				t.Fatalf("Pace(day %d) = %d; want %d", tc.day, got, tc.want)
			}
		})
	}

	t.Run("a month that has not started has nothing to be ahead of", func(t *testing.T) {
		if got := food().Pace(time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC)); got != 0 {
			t.Fatalf("Pace(before the cycle) = %d; want 0", got)
		}
	})

	t.Run("a month that is over is paced at the whole cap", func(t *testing.T) {
		if got := food().Pace(time.Date(2026, time.September, 2, 0, 0, 0, 0, time.UTC)); got != 80000 {
			t.Fatalf("Pace(after the cycle) = %d; want the full 80000", got)
		}
	})

	t.Run("february is short and paces faster", func(t *testing.T) {
		b := food()
		b.Cycle = "2026-02"
		// 28 days, not 31: the same day is further through a shorter month.
		if got := b.Pace(time.Date(2026, time.February, 14, 0, 0, 0, 0, time.UTC)); got != 40000 {
			t.Fatalf("Pace(14 Feb) = %d; want 40000, half of a 28-day month", got)
		}
	})

	t.Run("a leap february is longer than a plain one", func(t *testing.T) {
		b := food()
		b.Cycle = "2024-02"
		if got := b.Pace(time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC)); got != 80000 {
			t.Fatalf("Pace(29 Feb 2024) = %d; want the full 80000", got)
		}
	})

	t.Run("an unreadable cycle paces at nothing rather than panicking", func(t *testing.T) {
		b := food()
		b.Cycle = "nonsense"
		if got := b.Pace(on(20)); got != 0 {
			t.Fatalf("Pace(broken cycle) = %d; want 0", got)
		}
	})
}

func TestDrift(t *testing.T) {
	cases := []struct {
		name  string
		spent int64
		day   int
		want  int64
	}{
		// Pace on day 20 of August is 51612.
		{"ahead of the month", 54000, 20, 2388},
		{"behind the month", 40000, 20, -11612},
		{"exactly on pace", 51612, 20, 0},
		{"nothing spent is the whole pace behind", 0, 20, -51612},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := food()
			b.Spent = tc.spent
			if got := b.Drift(on(tc.day)); got != tc.want {
				t.Fatalf("Drift() = %d; want %d", got, tc.want)
			}
		})
	}
}

func TestStatus(t *testing.T) {
	cases := []struct {
		name   string
		spent  int64
		day    int
		active bool
		want   string
	}{
		{"under the pace is on track", 40000, 20, true, StatusOnTrack},
		{"exactly on the pace is on track", 51612, 20, true, StatusOnTrack},
		{"past the pace but inside the cap is ahead", 54000, 20, true, StatusAhead},
		{"past the cap is over", 85000, 20, true, StatusOver},
		{"exactly at the cap is not over yet", 80000, 31, true, StatusOnTrack},
		{"the whole cap spent on the first is ahead, not over", 80000, 1, true, StatusAhead},
		{"an archived budget says so whatever it spent", 85000, 20, false, StatusArchived},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := food()
			b.Spent, b.Active = tc.spent, tc.active
			if got := b.Status(on(tc.day)); got != tc.want {
				t.Fatalf("Status() = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestPct(t *testing.T) {
	cases := []struct {
		name  string
		spent int64
		want  int64
	}{
		{"part way", 54000, 67},
		{"nothing", 0, 0},
		{"all of it", 80000, 100},
		// Not clamped: 180% is news, and a full bar cannot tell it from 100%.
		{"past it", 144000, 180},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := food()
			b.Spent = tc.spent
			if got := b.Pct(); got != tc.want {
				t.Fatalf("Pct() = %d; want %d", got, tc.want)
			}
		})
	}
}

func TestFmt(t *testing.T) {
	t.Run("a real is two decimal places", func(t *testing.T) {
		if got := food().Fmt(80000); got != "R$800.00" {
			t.Fatalf("Fmt(80000) = %q; want R$800.00", got)
		}
	})

	t.Run("bitcoin is eight", func(t *testing.T) {
		b := food()
		b.Currency = "BTC"
		if got := b.Fmt(150000000); got != "₿1.50000000" {
			t.Fatalf("Fmt(150000000) = %q; want ₿1.50000000", got)
		}
	})
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Budget)
		want string // a substring of the error, or empty for "accepted"
	}{
		{"a budget is accepted", func(*Budget) {}, ""},
		{"a nameless budget is refused", func(b *Budget) { b.Name = "" }, "name"},
		{"a blank name is refused", func(b *Budget) { b.Name = "   " }, "name"},
		{"an amount of zero is refused", func(b *Budget) { b.Amount = 0 }, "more than zero"},
		{"a negative amount is refused", func(b *Budget) { b.Amount = -1 }, "more than zero"},
		{"a short code is refused", func(b *Budget) { b.Code = "FOO" }, "5 characters"},
		{"an unknown currency is refused", func(b *Budget) { b.Currency = "ZZZ" }, "currency"},
		{"a budget over no category is refused", func(b *Budget) { b.Category.ID = 0 }, "category"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := food()
			b.Category.ID = 7
			tc.edit(&b)
			err := b.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Validate() = %v; want it accepted", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v; want an error mentioning %q", err, tc.want)
			}
		})
	}
}

func TestCycleHelpers(t *testing.T) {
	t.Run("a cycle's bounds are its first and last day", func(t *testing.T) {
		from, to, err := CycleRange("2026-08")
		if err != nil {
			t.Fatal(err)
		}
		if from != "2026-08-01" || to != "2026-08-31" {
			t.Fatalf("CycleRange = %s..%s; want 2026-08-01..2026-08-31", from, to)
		}
	})

	t.Run("february stops where february stops", func(t *testing.T) {
		_, to, err := CycleRange("2026-02")
		if err != nil {
			t.Fatal(err)
		}
		if to != "2026-02-28" {
			t.Fatalf("CycleRange end = %s; want 2026-02-28", to)
		}
	})

	t.Run("an unreadable cycle is refused", func(t *testing.T) {
		if _, _, err := CycleRange("2026"); err == nil {
			t.Fatal("CycleRange(\"2026\") succeeded; want it refused")
		}
	})
}
