package bills

import (
	"testing"
	"time"

	"kakei/internal/cards"
)

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(dateLayout, s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func card(closingDay, dueDay int) cards.Card {
	return cards.Card{
		ID: 1, Code: "NUCRD", Name: "Nubank", Color: "violet", Currency: "BRL",
		Limit: 500000, ClosingDay: closingDay, DueDay: dueDay,
	}
}

func TestPeriod(t *testing.T) {
	cases := []struct {
		name       string
		closingDay int
		closesOn   string
		wantFrom   string
	}{
		{"the day after the last closing", 10, "2026-08-10", "2026-07-11"},
		{"across the year", 10, "2026-01-10", "2025-12-11"},
		// A card closing on the 31st closes on the 28th in February and the 30th
		// in September, so the period before one of those still has to start the
		// day after the *real* previous closing, not a month back from a clamped
		// date.
		{"after a short february", 31, "2026-03-31", "2026-03-01"},
		{"a clamped february itself", 31, "2026-02-28", "2026-02-01"},
		{"a clamped september after a 31-day august", 31, "2026-09-30", "2026-09-01"},
		{"the 31st after a 30-day september", 31, "2026-10-31", "2026-10-01"},
		{"the first of the month", 1, "2026-08-01", "2026-07-02"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := Bill{ClosesOn: tc.closesOn, Card: card(tc.closingDay, 20)}
			from, to := b.Period()
			if from != tc.wantFrom || to != tc.closesOn {
				t.Fatalf("Period() = %s → %s; want %s → %s", from, to, tc.wantFrom, tc.closesOn)
			}
		})
	}
}

// Every day belongs to exactly one bill: no gap between one period and the next,
// and no overlap. This is the property the clamping is there for.
func TestPeriodsTile(t *testing.T) {
	for _, day := range []int{1, 10, 28, 29, 30, 31} {
		t.Run("closing day "+string(rune('0'+day/10))+string(rune('0'+day%10)), func(t *testing.T) {
			c := card(day, 20)
			closing := cards.NextDate(mustDate(t, "2025-12-01"), day)
			var prevTo string
			for range 15 {
				b := Bill{ClosesOn: closing.Format(dateLayout), Card: c}
				from, to := b.Period()
				if prevTo != "" {
					want := mustDate(t, prevTo).AddDate(0, 0, 1).Format(dateLayout)
					if from != want {
						t.Fatalf("bill closing %s starts %s; the one before ended %s", to, from, prevTo)
					}
				}
				prevTo = to
				closing = cards.NextDate(closing.AddDate(0, 0, 1), day)
			}
		})
	}
}

func TestDueDate(t *testing.T) {
	cases := []struct {
		name               string
		closesOn           string
		closingDay, dueDay int
		want               string
	}{
		{"a due day after the closing day is the same month", "2026-08-10", 10, 20, "2026-08-20"},
		{"a due day before the closing day rolls over", "2026-08-25", 25, 5, "2026-09-05"},
		{"a due day equal to the closing day is that day", "2026-08-10", 10, 10, "2026-08-10"},
		{"a due day the month is too short for clamps", "2026-01-31", 31, 30, "2026-02-28"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DueDate(mustDate(t, tc.closesOn), tc.dueDay).Format(dateLayout)
			if got != tc.want {
				t.Fatalf("DueDate(%s, %d) = %s; want %s", tc.closesOn, tc.dueDay, got, tc.want)
			}
		})
	}
}

func TestStatusFor(t *testing.T) {
	cases := []struct {
		name        string
		total, paid int64
		closed      bool
		want        string
	}{
		{"still open", 89050, 0, false, StatusOpen},
		{"open and already part paid", 89050, 40000, false, StatusOpen},
		{"closed, untouched", 89050, 0, true, StatusClosed},
		{"closed, part paid", 89050, 40000, true, StatusPartial},
		{"closed, paid to the cent", 89050, 89050, true, StatusPaid},
		{"closed, overpaid", 89050, 90000, true, StatusPaid},
		// A cycle with nothing on it, or one refunds left in credit, owes
		// nothing — there is no bill to chase.
		{"closed with nothing on it", 0, 0, true, StatusPaid},
		{"closed in credit", -5000, 0, true, StatusPaid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StatusFor(tc.total, tc.paid, tc.closed); got != tc.want {
				t.Fatalf("StatusFor(%d, %d, %v) = %q; want %q", tc.total, tc.paid, tc.closed, got, tc.want)
			}
		})
	}
}

func TestRemaining(t *testing.T) {
	cases := []struct {
		name        string
		total, paid int64
		want        int64
	}{
		{"nothing paid", 89050, 0, 89050},
		{"part paid", 89050, 40000, 49050},
		{"paid off", 89050, 89050, 0},
		// Never negative: "R$-95.00 left" is a riddle, and overpaying is real.
		{"overpaid", 89050, 90000, 0},
		{"a bill in credit", -5000, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := Bill{Total: tc.total, Paid: tc.paid}
			if got := b.Remaining(); got != tc.want {
				t.Fatalf("Remaining() = %d; want %d", got, tc.want)
			}
		})
	}
}

func TestOwed(t *testing.T) {
	// An open bill is a running total, not a debt: nothing is owed until the
	// cycle stops taking charges.
	cases := []struct {
		name        string
		status      string
		total, paid int64
		want        int64
	}{
		{"open, whatever is on it", StatusOpen, 89050, 0, 0},
		{"closed", StatusClosed, 89050, 0, 89050},
		{"partial", StatusPartial, 89050, 40000, 49050},
		{"paid", StatusPaid, 89050, 89050, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := Bill{Total: tc.total, Paid: tc.paid, Status: tc.status}
			if got := b.Owed(); got != tc.want {
				t.Fatalf("Owed() = %d; want %d", got, tc.want)
			}
		})
	}
}
