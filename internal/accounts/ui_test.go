package accounts

import (
	"strings"
	"testing"

	"kakei/internal/core"
)

// The renderers below are the only part of ui.go that runs without a TTY —
// Form, Confirm and Pick all block on a real terminal, so they are exercised
// through the commands instead. Assertions are on substrings, never on whole
// lines: lipgloss wraps text in escape codes when the profile allows color.

func TestLabel(t *testing.T) {
	cases := []struct {
		name string
		a    Account
		want string
	}{
		{"code and name", Account{Code: "WLLT2", Name: "Wallet"}, "[WLLT2] Wallet"},
		{"frozen gets a mark", Account{Code: "OLDAC", Name: "Antiga", IsFrozen: true}, "[OLDAC] Antiga ❄"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Label(tc.a); got != tc.want {
				t.Fatalf("Label = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestBalanceColor(t *testing.T) {
	cases := []struct {
		name string
		a    Account
		want string
	}{
		{"positive is green", Account{Balance: 1}, core.ColorByName("green").Hex},
		{"negative is red", Account{Balance: -1}, core.ColorByName("red").Hex},
		{"zero is left alone", Account{Balance: 0}, ""},
		{"frozen credit is dimmed", Account{Balance: 1, IsFrozen: true}, core.DimColor},
		{"frozen debit is dimmed", Account{Balance: -1, IsFrozen: true}, core.DimColor},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := balanceColor(tc.a); got != tc.want {
				t.Fatalf("balanceColor(%+v) = %q; want %q", tc.a, got, tc.want)
			}
		})
	}
}

func TestLabelColor(t *testing.T) {
	t.Run("an active account keeps its own color", func(t *testing.T) {
		a := Account{Color: "teal"}
		if got := labelColor(a); got != core.ColorByName("teal").Hex {
			t.Fatalf("labelColor = %q; want the teal hex", got)
		}
	})

	t.Run("a frozen account is dimmed", func(t *testing.T) {
		a := Account{Color: "teal", IsFrozen: true}
		if got := labelColor(a); got != core.DimColor {
			t.Fatalf("labelColor = %q; want the dim color", got)
		}
	})
}

func TestTable(t *testing.T) {
	t.Run("shows one row per account", func(t *testing.T) {
		got := Table([]Account{
			{Code: "WLLT2", Name: "Wallet", Currency: "BTC", Balance: 150000000},
			{Code: "SAVE1", Name: "Savings", Currency: "BRL", Balance: -1234},
		})

		for _, want := range []string{
			"ACCOUNT", "BALANCE", // headers
			"[WLLT2] Wallet", "₿1.50000000",
			"[SAVE1] Savings", "R$-12.34",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("table is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("has no code, name or currency column", func(t *testing.T) {
		got := Table([]Account{{Code: "WLLT2", Name: "Wallet", Currency: "BTC", Balance: 1}})
		for _, gone := range []string{"CODE", "NAME", "CURRENCY", "BTC"} {
			if strings.Contains(got, gone) {
				t.Errorf("table still carries %q:\n%s", gone, got)
			}
		}
	})

	t.Run("is exactly two columns wide", func(t *testing.T) {
		// The trailing frozen column used to render as an empty cell on every
		// row; count the dividers on the header line to keep it gone.
		header := strings.Split(Table([]Account{{Code: "WLLT2", Name: "Wallet", Currency: "USD"}}), "\n")[0]
		if n := strings.Count(header, "┬"); n != 1 {
			t.Fatalf("header has %d column dividers; want 1 (two columns):\n%s", n, header)
		}
	})

	t.Run("marks only the frozen accounts", func(t *testing.T) {
		active := Table([]Account{{Code: "WLLT2", Name: "Wallet", Currency: "USD"}})
		if strings.Contains(active, "❄") {
			t.Errorf("active account got a frozen mark:\n%s", active)
		}

		frozen := Table([]Account{{Code: "WLLT2", Name: "Wallet", Currency: "USD", IsFrozen: true}})
		if !strings.Contains(frozen, "❄") {
			t.Errorf("frozen account has no mark:\n%s", frozen)
		}
	})

	t.Run("renders an empty list without panicking", func(t *testing.T) {
		if got := Table(nil); !strings.Contains(got, "ACCOUNT") {
			t.Errorf("empty table lost its headers:\n%s", got)
		}
	})
}

func TestDetails(t *testing.T) {
	a := Account{
		ID: 7, Code: "WLLT2", Name: "Wallet", Description: "cold storage",
		Color: "violet", Currency: "BTC", Balance: 150000000,
		CreatedAt: "2026-01-02 03:04:05", UpdatedAt: "2026-02-03 04:05:06",
	}

	t.Run("carries every value", func(t *testing.T) {
		got := Details(a)
		for _, want := range []string{
			"WLLT2", "Wallet", "cold storage",
			"₿1.50000000", "BTC",
			createdIcon + " 2026-01-02 03:04:05",
			updatedIcon + " 2026-02-03 04:05:06",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("card is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("drops the id and the state word", func(t *testing.T) {
		got := Details(a)
		for _, gone := range []string{"#7", "active"} {
			if strings.Contains(got, gone) {
				t.Errorf("card still shows %q:\n%s", gone, got)
			}
		}

		b := a
		b.IsFrozen = true
		if got := Details(b); strings.Contains(got, "frozen") {
			t.Errorf("card spells out the frozen state:\n%s", got)
		}
	})

	t.Run("names no fields", func(t *testing.T) {
		// The card shows values only — no "Code: WLLT2" labelling.
		got := Details(a)
		for _, label := range []string{"Code", "Name", "Description", "Balance", "Color", "State", "ID"} {
			if strings.Contains(got, label+" ") || strings.Contains(got, label+":") {
				t.Errorf("card still labels the %q field:\n%s", label, got)
			}
		}
	})

	t.Run("is a bordered card", func(t *testing.T) {
		got := Details(a)
		for _, corner := range []string{"╭", "╮", "╰", "╯"} {
			if !strings.Contains(got, corner) {
				t.Errorf("card has no %q corner:\n%s", corner, got)
			}
		}
	})

	t.Run("skips the line for an empty description", func(t *testing.T) {
		b := a
		b.Description = ""
		full, bare := strings.Count(Details(a), "\n"), strings.Count(Details(b), "\n")
		if bare != full-1 {
			t.Errorf("empty description did not drop a line: %d vs %d\n%s", bare, full, Details(b))
		}
	})

	t.Run("marks a frozen account", func(t *testing.T) {
		// ❄ beside the code is the whole signal now.
		b := a
		b.IsFrozen = true
		if got := Details(b); !strings.Contains(got, "❄") {
			t.Errorf("frozen account has no mark:\n%s", got)
		}
	})

	t.Run("survives a hand-edited row", func(t *testing.T) {
		// Unknown color and currency fall back instead of crashing the command.
		got := Details(Account{Code: "HAND1", Name: "Hand", Color: "puce", Currency: "XXX"})
		if !strings.Contains(got, "HAND1") {
			t.Errorf("details bailed on unknown color/currency:\n%s", got)
		}
	})
}

func TestPickerRow(t *testing.T) {
	a := Account{Code: "WLLT2", Name: "Wallet", Currency: "BTC", Balance: 150000000}

	t.Run("title carries code and name", func(t *testing.T) {
		got := pickerRow(a).Label
		if !strings.Contains(got, "WLLT2") || !strings.Contains(got, "Wallet") {
			t.Fatalf("title = %q", got)
		}
		if strings.Contains(got, "❄") {
			t.Fatalf("active account marked frozen: %q", got)
		}
	})

	t.Run("title marks a frozen account", func(t *testing.T) {
		b := a
		b.IsFrozen = true
		if got := pickerRow(b).Label; !strings.Contains(got, "❄") {
			t.Fatalf("frozen title = %q", got)
		}
	})

	t.Run("description is the formatted balance", func(t *testing.T) {
		if got := pickerRow(a).Desc; got != "₿1.50000000 BTC" {
			t.Fatalf("description = %q", got)
		}
	})

	t.Run("filters on code and name", func(t *testing.T) {
		if got := pickerRow(a).Filter; got != "WLLT2 Wallet" {
			t.Fatalf("filter value = %q", got)
		}
	})
}
