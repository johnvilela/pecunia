package bills

import (
	"strings"
	"testing"

	"kakei/internal/core"
)

// Everything here asserts on substrings: the ANSI escapes lipgloss wraps text in
// change with the terminal profile, the text does not.

func bill(status string, total, paid int64) Bill {
	return Bill{
		ID: 1, ClosesOn: "2026-08-10", DueOn: "2026-08-20",
		Total: total, Paid: paid, Status: status, Card: card(10, 20),
	}
}

func TestTable(t *testing.T) {
	got := Table([]Bill{
		bill(StatusOpen, 31200, 0),
		bill(StatusPartial, 89050, 40000),
		bill(StatusPaid, 124000, 124000),
	})

	for _, want := range []string{
		"CARD", "MONTH", "CLOSES", "DUE", "TOTAL", "LEFT", "STATUS",
		"NUCRD", "August", "2026-08-10", "2026-08-20",
		"R$312.00", "R$890.50", "R$1240.00",
		StatusOpen, StatusPartial, StatusPaid,
		"R$490.50", // what a partial bill still owes
	} {
		if !strings.Contains(got, want) {
			t.Errorf("table is missing %q:\n%s", want, got)
		}
	}
}

func TestTableIsNeverGreen(t *testing.T) {
	// Same rule the cards table is pinned to: a credit card is never good news,
	// so a green anywhere on one would only be noise.
	got := Table([]Bill{bill(StatusPaid, 124000, 124000), bill(StatusOpen, 31200, 0)})
	if strings.Contains(got, core.ColorByName("green").Hex) {
		t.Errorf("a bill was rendered green:\n%s", got)
	}
}

func TestDetails(t *testing.T) {
	b := bill(StatusPartial, 89050, 40000)
	charges := []Charge{
		{ID: 7, Date: "2026-07-15", Title: "Groceries", Value: 69050, Kind: "outcome"},
		{ID: 8, Date: "2026-08-01", Title: "Phone", Value: 20000, Kind: "outcome", Seq: 3, Count: 5},
	}
	got := Details(b, charges, b.Total)

	for _, want := range []string{
		"NUCRD",
		"2026-07-11", "2026-08-10", // the period, both ends
		"2026-08-20", // due
		"R$890.50",   // total
		"R$400.00",   // paid
		"R$490.50",   // left
		StatusPartial,
		"Groceries", "Phone", "(3/5)",
		"#7", "#8",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("details is missing %q:\n%s", want, got)
		}
	}
}

func TestDetailsFlagsADriftedTotal(t *testing.T) {
	b := bill(StatusClosed, 89050, 0)

	t.Run("says so when the ledger has moved on", func(t *testing.T) {
		got := Details(b, nil, 99050)
		if !strings.Contains(got, "R$990.50") {
			t.Errorf("details does not show what the ledger sums to now:\n%s", got)
		}
	})

	t.Run("stays quiet when it has not", func(t *testing.T) {
		got := Details(b, nil, b.Total)
		if strings.Contains(got, "≠") {
			t.Errorf("details flagged a drift that is not there:\n%s", got)
		}
	})
}

func TestDetailsOfAnEmptyBill(t *testing.T) {
	// A cycle with nothing on it should read as nothing owed, not as a blank.
	got := Details(bill(StatusPaid, 0, 0), nil, 0)
	if !strings.Contains(got, "nothing") {
		t.Errorf("an empty bill does not say so:\n%s", got)
	}
}
