package recurring

import (
	"strings"
	"testing"
	"time"

	"pecunia/internal/transactions"
)

// on is a date written the way the rest of pecunia writes them.
func on(s string) time.Time {
	d, err := time.Parse(DateLayout, s)
	if err != nil {
		panic(err)
	}
	return d
}

// energy is the bill every case here starts from: opens on the 5th, late after
// the 15th, paid from an account in BRL. Made in August, so a case that says
// nothing about earlier months really has none — a bill owes nothing for the
// months before it existed.
func energy() Bill {
	return Bill{
		ID: 1, Code: "ENERG", Name: "Energy", Expected: 21490,
		OpenDay: 5, DueDay: 15, Active: true,
		Account: transactions.Ref{ID: 1, Code: "INTER"}, Currency: "BRL",
		CreatedAt: "2026-08-01 09:00:00",
	}
}

// sinceJuly is the same bill with a July behind it, for the cases about a month
// left unpaid.
func sinceJuly() Bill {
	b := energy()
	b.CreatedAt = "2026-07-01 09:00:00"
	return b
}

func TestOccurrence(t *testing.T) {
	cases := []struct {
		name     string
		bill     Bill
		today    string
		payments map[string]Tally
		cycle    string
		status   string
		openOn   string
		dueOn    string
		late     int
	}{
		{
			name: "before the open day the cycle has not started", bill: energy(),
			today: "2026-08-02", cycle: "2026-08", status: StatusUpcoming,
			openOn: "2026-08-05", dueOn: "2026-08-15",
		},
		{
			name: "on the open day it can be paid", bill: energy(),
			today: "2026-08-05", cycle: "2026-08", status: StatusOpen,
			openOn: "2026-08-05", dueOn: "2026-08-15",
		},
		{
			name: "the due day itself is still open", bill: energy(),
			today: "2026-08-15", cycle: "2026-08", status: StatusOpen,
			openOn: "2026-08-05", dueOn: "2026-08-15",
		},
		{
			name: "the day after the due day is late", bill: energy(),
			today: "2026-08-18", cycle: "2026-08", status: StatusOverdue,
			openOn: "2026-08-05", dueOn: "2026-08-15", late: 3,
		},
		{
			name: "a payment for the cycle settles it", bill: energy(),
			today: "2026-08-18", payments: map[string]Tally{"2026-08": {Value: 20000, Count: 1}},
			cycle: "2026-08", status: StatusPaid, openOn: "2026-08-05", dueOn: "2026-08-15",
		},
		{
			// The shape of a real energy bill: it arrives at the end of one month
			// and is due early in the next.
			name:  "a due day before the open day falls in the next month",
			bill:  func() Bill { b := energy(); b.OpenDay, b.DueDay = 28, 5; return b }(),
			today: "2026-08-30", cycle: "2026-08", status: StatusOpen,
			openOn: "2026-08-28", dueOn: "2026-09-05",
		},
		{
			name:  "a day the month is too short for lands on its last",
			bill:  func() Bill { b := energy(); b.OpenDay, b.DueDay = 30, 31; return b }(),
			today: "2026-02-27", cycle: "2026-02", status: StatusUpcoming,
			openOn: "2026-02-28", dueOn: "2026-02-28",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.bill.Payments = c.payments
			occ := c.bill.Occurrence(c.cycle, on(c.today))
			if occ.Status != c.status {
				t.Errorf("status = %q, want %q", occ.Status, c.status)
			}
			if occ.OpenOn != c.openOn {
				t.Errorf("opens on %s, want %s", occ.OpenOn, c.openOn)
			}
			if occ.DueOn != c.dueOn {
				t.Errorf("due on %s, want %s", occ.DueOn, c.dueOn)
			}
			if occ.Late != c.late {
				t.Errorf("late by %d days, want %d", occ.Late, c.late)
			}
		})
	}
}

func TestCurrent(t *testing.T) {
	t.Run("is this month when nothing is behind", func(t *testing.T) {
		occ := energy().Current(on("2026-08-08"))
		if occ.Cycle != "2026-08" || occ.Status != StatusOpen {
			t.Fatalf("got %s %s, want 2026-08 open", occ.Cycle, occ.Status)
		}
	})

	t.Run("is the oldest cycle still unpaid", func(t *testing.T) {
		b := sinceJuly()
		b.Payments = map[string]Tally{"2026-08": {Value: 21490, Count: 1}}
		// August is paid, but July never was — and a paid August must not hide it.
		occ := b.Current(on("2026-08-20"))
		if occ.Cycle != "2026-07" || occ.Status != StatusOverdue {
			t.Fatalf("got %s %s, want 2026-07 overdue", occ.Cycle, occ.Status)
		}
	})

	t.Run("a late payment for the month it was for clears it", func(t *testing.T) {
		b := sinceJuly()
		b.Payments = map[string]Tally{
			"2026-07": {Value: 20000, Count: 1},
			"2026-08": {Value: 21490, Count: 1},
		}
		occ := b.Current(on("2026-08-20"))
		if occ.Cycle != "2026-08" || occ.Status != StatusPaid {
			t.Fatalf("got %s %s, want 2026-08 paid", occ.Cycle, occ.Status)
		}
	})

	t.Run("does not look back past the month the bill was made in", func(t *testing.T) {
		// July is unpaid and would win — but the bill did not exist in July.
		b := energy()
		b.Payments = map[string]Tally{"2026-08": {Value: 21490, Count: 1}}
		occ := b.Current(on("2026-08-20"))
		if occ.Cycle != "2026-08" {
			t.Fatalf("cycle = %s, want 2026-08 — a bill owes nothing for the months before it existed", occ.Cycle)
		}
	})

	t.Run("an archived bill owes nothing", func(t *testing.T) {
		b := sinceJuly()
		b.Active = false
		// July and August are both unpaid and long past due — and none of that is
		// owed, because the bill was put away.
		occ := b.Current(on("2026-08-20"))
		if occ.Status != StatusArchived {
			t.Fatalf("status = %q, want %q", occ.Status, StatusArchived)
		}
		if occ.Cycle != "2026-08" {
			t.Errorf("cycle = %s, want this month", occ.Cycle)
		}
	})

	t.Run("stops looking back after a year", func(t *testing.T) {
		b := energy()
		b.CreatedAt = "2020-01-01 09:00:00"
		occ := b.Current(on("2026-08-20"))
		if occ.Cycle != "2025-08" {
			t.Fatalf("cycle = %s, want 2025-08 — twelve months back is as far as a board reaches", occ.Cycle)
		}
	})

	t.Run("an upcoming month is still what is current when the last one is paid", func(t *testing.T) {
		b := sinceJuly()
		b.Payments = map[string]Tally{
			"2026-07": {Value: 20000, Count: 1},
			"2026-08": {Value: 21490, Count: 1},
		}
		// Nothing has opened yet in September, and August is settled.
		occ := b.Current(on("2026-09-02"))
		if occ.Cycle != "2026-09" || occ.Status != StatusUpcoming {
			t.Fatalf("got %s %s, want 2026-09 upcoming", occ.Cycle, occ.Status)
		}
	})
}

func TestCycleOf(t *testing.T) {
	if got := CycleOf("2026-08-08"); got != "2026-08" {
		t.Errorf("CycleOf = %q, want 2026-08", got)
	}
	if got := CycleOf("nonsense"); got != "" {
		t.Errorf("CycleOf of a non-date = %q, want empty", got)
	}
}

func TestParseCycle(t *testing.T) {
	if got, err := ParseCycle(" 2026-8 "); err != nil || got != "2026-08" {
		t.Errorf("ParseCycle = %q, %v — want a canonical 2026-08", got, err)
	}
	if _, err := ParseCycle("2026-13"); err == nil {
		t.Error("expected month 13 to be refused")
	}
	if _, err := ParseCycle("2026-08-08"); err == nil {
		t.Error("expected a full date to be refused — a cycle is a month")
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		bill Bill
		want string
	}{
		{"a whole bill", energy(), ""},
		{"no name", func() Bill { b := energy(); b.Name = ""; return b }(), "name is required"},
		{"a short code", func() Bill { b := energy(); b.Code = "EN"; return b }(), "exactly 5"},
		{
			"a negative expected amount",
			func() Bill { b := energy(); b.Expected = -1; return b }(),
			"cannot be negative",
		},
		{"a day off the calendar", func() Bill { b := energy(); b.DueDay = 32; return b }(), "between 1 and 31"},
		{
			"no source at all",
			func() Bill { b := energy(); b.Account = transactions.Ref{}; return b }(),
			"either an account or a credit card",
		},
		{
			"both a card and an account",
			func() Bill { b := energy(); b.Card = transactions.Ref{ID: 2}; return b }(),
			"either an account or a credit card",
		},
		{
			"too many tags",
			func() Bill { b := energy(); b.Tags = []string{"a", "b", "c", "d", "e", "f"}; return b }(),
			"at most",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.bill.Validate()
			if c.want == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want one mentioning %q", err, c.want)
			}
		})
	}
}

func TestStats(t *testing.T) {
	paid := []transactions.Transaction{
		{Value: 21000}, {Value: 19000}, {Value: 23000},
	}
	s := Stats(paid)
	if s.Count != 3 {
		t.Errorf("count = %d, want 3", s.Count)
	}
	if s.Total != 63000 {
		t.Errorf("total = %d, want 63000", s.Total)
	}
	if s.Avg != 21000 {
		t.Errorf("average = %d, want 21000", s.Avg)
	}
	if s.Min != 19000 || s.Max != 23000 {
		t.Errorf("range = %d..%d, want 19000..23000", s.Min, s.Max)
	}

	// Nothing paid yet is not a divide by zero.
	if got := Stats(nil); got.Count != 0 || got.Avg != 0 {
		t.Errorf("empty stats = %+v, want a zero value", got)
	}
}
