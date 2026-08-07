package cards

import (
	"strings"
	"testing"

	"kakei/internal/core"
)

// The renderers below are the only part of ui.go that runs without a TTY —
// Form and the picker block on a real terminal, so they are exercised through
// the commands instead. Assertions are on substrings, never on whole lines:
// lipgloss wraps text in escape codes when the profile allows color.

func TestLabel(t *testing.T) {
	if got := Label(nubank()); got != "[NUCRD] Nubank" {
		t.Fatalf("Label = %q; want %q", got, "[NUCRD] Nubank")
	}
}

func TestAvailableColor(t *testing.T) {
	cases := []struct {
		name    string
		limit   int64
		balance int64
		want    string
	}{
		{"room left is green", 500000, 1, core.ColorByName("green").Hex},
		{"over the limit is red", 500000, 600000, core.ColorByName("red").Hex},
		{"exactly at the limit is left alone", 500000, 500000, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Card{Limit: tc.limit, Balance: tc.balance}
			if got := availableColor(c); got != tc.want {
				t.Fatalf("availableColor(%d/%d) = %q; want %q", tc.balance, tc.limit, got, tc.want)
			}
		})
	}
}

func TestUsageBar(t *testing.T) {
	filled := func(s string) int { return strings.Count(s, "█") }

	cases := []struct {
		name    string
		limit   int64
		balance int64
		want    int
	}{
		{"nothing used", 500000, 0, 0},
		{"a quarter used", 400000, 100000, barWidth / 4},
		{"fully used", 500000, 500000, barWidth},
		{"over the limit stops at full", 500000, 900000, barWidth},
		{"a refund does not go negative", 500000, -100, 0},
		{"no limit reads as full", 0, 1, barWidth},
		{"no limit and nothing used reads as empty", 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := usageBar(Card{Limit: tc.limit, Balance: tc.balance})
			if n := filled(got); n != tc.want {
				t.Fatalf("usageBar(%d/%d) filled %d of %d cells; want %d",
					tc.balance, tc.limit, n, barWidth, tc.want)
			}
			if n := filled(got) + strings.Count(got, "░"); n != barWidth {
				t.Fatalf("bar is %d cells wide; want %d", n, barWidth)
			}
		})
	}
}

func TestTable(t *testing.T) {
	t.Run("shows one row per card", func(t *testing.T) {
		other := nubank()
		other.Code, other.Name, other.Currency = "ITAU1", "Itaú", "USD"
		other.Limit, other.Balance = 100000, 0
		other.ClosingDay, other.DueDay = 3, 9

		got := Table([]Card{nubank(), other})
		for _, want := range []string{
			"CARD", "AVAILABLE", "CLOSE/DUE", // headers
			"[NUCRD] Nubank", "R$3761.50", "R$5000.00", "15/22",
			"[ITAU1] Itaú", "$1000.00", "3/9",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("table is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("is exactly three columns wide", func(t *testing.T) {
		header := strings.Split(Table([]Card{nubank()}), "\n")[0]
		if n := strings.Count(header, "┬"); n != 2 {
			t.Fatalf("header has %d column dividers; want 2 (three columns):\n%s", n, header)
		}
	})

	t.Run("has no currency column", func(t *testing.T) {
		// The symbol is already in the amount.
		got := Table([]Card{nubank()})
		for _, gone := range []string{"CURRENCY", "BRL", "LIMIT"} {
			if strings.Contains(got, gone) {
				t.Errorf("table still carries %q:\n%s", gone, got)
			}
		}
	})

	t.Run("renders an empty list without panicking", func(t *testing.T) {
		if got := Table(nil); !strings.Contains(got, "CARD") {
			t.Errorf("empty table lost its headers:\n%s", got)
		}
	})
}

func TestDetails(t *testing.T) {
	c := nubank()
	c.ID = 7
	c.Description = "cartão principal"
	c.CreatedAt, c.UpdatedAt = "2026-01-02 03:04:05", "2026-02-03 04:05:06"

	t.Run("carries every value", func(t *testing.T) {
		got := Details(c)
		for _, want := range []string{
			"NUCRD", "Nubank", "cartão principal",
			"R$1238.50", "BRL", // what the open invoice owes
			"R$3761.50", "R$5000.00", // available of the limit
			"closes 15", "due 22",
			createdIcon + " 2026-01-02 03:04:05",
			updatedIcon + " 2026-02-03 04:05:06",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("card is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("shows the usage bar", func(t *testing.T) {
		if got := Details(c); !strings.Contains(got, "█") || !strings.Contains(got, "░") {
			t.Errorf("card has no usage bar:\n%s", got)
		}
	})

	t.Run("names no fields", func(t *testing.T) {
		got := Details(c)
		for _, label := range []string{"Code", "Name", "Description", "Balance", "Color", "ID"} {
			if strings.Contains(got, label+" ") || strings.Contains(got, label+":") {
				t.Errorf("card still labels the %q field:\n%s", label, got)
			}
		}
	})

	t.Run("is a bordered card", func(t *testing.T) {
		got := Details(c)
		for _, corner := range []string{"╭", "╮", "╰", "╯"} {
			if !strings.Contains(got, corner) {
				t.Errorf("card has no %q corner:\n%s", corner, got)
			}
		}
	})

	t.Run("skips the line for an empty description", func(t *testing.T) {
		bare := c
		bare.Description = ""
		full, short := strings.Count(Details(c), "\n"), strings.Count(Details(bare), "\n")
		if short != full-1 {
			t.Errorf("empty description did not drop a line: %d vs %d\n%s", short, full, Details(bare))
		}
	})

	t.Run("survives a hand-edited row", func(t *testing.T) {
		got := Details(Card{Code: "HAND1", Name: "Hand", Color: "puce", Currency: "XXX",
			ClosingDay: 1, DueDay: 2})
		if !strings.Contains(got, "HAND1") {
			t.Errorf("details bailed on unknown color/currency:\n%s", got)
		}
	})
}

func TestPickerRow(t *testing.T) {
	row := pickerRow(nubank())

	if !strings.Contains(row.Label, "NUCRD") || !strings.Contains(row.Label, "Nubank") {
		t.Errorf("label = %q", row.Label)
	}
	if row.Desc != "R$3761.50 available" {
		t.Errorf("description = %q", row.Desc)
	}
	if row.Filter != "NUCRD Nubank" {
		t.Errorf("filter value = %q", row.Filter)
	}
}
