// Package recurring holds the bills that come round every month: energy,
// Netflix, rent. A bill is a template — what it usually costs, where it is paid
// from, what it is filed under, and the two days that say when it can be paid
// and when it is late. It holds no money of its own.
//
// What has been paid is the transactions carrying the bill's id, which is why
// this package imports pecunia/internal/transactions rather than reading the
// table behind its back: nothing in transactions imports this, so there is no
// cycle to dodge.
package recurring

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"pecunia/internal/cards"
	"pecunia/internal/core"
	"pecunia/internal/transactions"
)

// DateLayout is the one date format pecunia reads or writes, everywhere.
const DateLayout = transactions.DateLayout

// CycleLayout is how a cycle is written: the month a bill is for, and no day.
// February's bill paid on 3 March is a payment dated 2026-03-03 for the cycle
// 2026-02, and that difference is the point of the column.
const CycleLayout = transactions.CycleLayout

// The four states one month of a bill can be in. Together they are a function
// of the calendar and the payments, so nothing is stored and nothing can drift.
const (
	StatusUpcoming = "upcoming" // the cycle has not opened yet
	StatusOpen     = "open"     // payable, not yet late
	StatusOverdue  = "overdue"  // past its due date with nothing filed against it
	StatusPaid     = "paid"
	// StatusArchived is a bill that has been put away — a cancelled
	// subscription. It is not a state of a cycle but of the bill itself, which
	// is why only Current ever returns it: an archived bill owes nothing, and
	// months it never paid are not debts to chase.
	StatusArchived = "archived"
)

// LookBack is as far as a board reaches for a cycle nobody paid. A bill left
// unpaid for a year is not news a list can break, and without a floor every
// board would walk back to the day the bill was made.
const LookBack = 12

var ErrNotFound = errors.New("bill not found")

// Tally is what has been filed against one cycle. Count is what decides whether
// the cycle is settled — a payment of any size means somebody paid it.
type Tally struct {
	Value int64
	Count int
}

// Bill is one recurring cost. Currency, the refs and Payments are filled in by
// the store on read; nothing about a cycle is stored.
type Bill struct {
	ID          int64
	Code        string
	Name        string
	Description string
	Color       string
	// Expected is what it usually costs, in the minor units of whatever it is
	// paid from. Zero means unknown, which is honest for a bill nobody has seen
	// yet — it is a starting point for the amount, never a fact.
	Expected int64
	OpenDay  int // day of the month the bill can be paid from
	DueDay   int // day of the month it is late after
	Active   bool
	Tags     []string
	Category transactions.Ref // ID 0 when the bill has none
	Account  transactions.Ref // exactly one of Account.ID and Card.ID is set
	Card     transactions.Ref
	Currency string // inherited from whichever source is set
	// Payments is what has been paid, by cycle. The store fills it; Current and
	// Occurrence read it, so a status never needs a second trip to the database.
	Payments  map[string]Tally
	CreatedAt string
	UpdatedAt string
}

func (b Bill) IsCard() bool { return b.Card.ID != 0 }

// Source is the account or credit card the bill is paid from.
func (b Bill) Source() transactions.Ref {
	if b.IsCard() {
		return b.Card
	}
	return b.Account
}

func (b Bill) Cur() core.Currency { return core.CurrencyByCode(b.Currency) }

// Fmt renders any of the bill's amounts with its currency symbol.
func (b Bill) Fmt(v int64) string { return b.Cur().Symbol + core.FormatAmount(v, b.Cur()) }

// Occurrence is one month of a bill: when it opens, when it is late, and where
// it stands today.
type Occurrence struct {
	Cycle  string // YYYY-MM
	OpenOn string // YYYY-MM-DD
	DueOn  string
	Status string
	Paid   int64
	Count  int
	Late   int // days past the due date, 0 unless overdue
}

// Occurrence works out where one cycle of this bill stands on a given day.
//
// The due date is the next due day on or after the open date, which is
// cards.NextDate's whole job — so an energy bill opening on the 28th and due on
// the 5th lands in the month after, and a day the month is too short for lands
// on its last, both without a case here.
func (b Bill) Occurrence(cycle string, today time.Time) Occurrence {
	start, err := time.Parse(CycleLayout, cycle)
	if err != nil {
		return Occurrence{Cycle: cycle}
	}
	open := cards.NextDate(start, b.OpenDay)
	due := cards.NextDate(open, b.DueDay)

	occ := Occurrence{
		Cycle:  cycle,
		OpenOn: open.Format(DateLayout),
		DueOn:  due.Format(DateLayout),
	}
	if t, ok := b.Payments[cycle]; ok && t.Count > 0 {
		occ.Paid, occ.Count, occ.Status = t.Value, t.Count, StatusPaid
		return occ
	}
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	switch {
	case today.Before(open):
		occ.Status = StatusUpcoming
	case today.After(due):
		// The due day itself is still a day to pay on, so lateness starts the
		// morning after it.
		occ.Status, occ.Late = StatusOverdue, int(today.Sub(due).Hours()/24)
	default:
		occ.Status = StatusOpen
	}
	return occ
}

// Current is the cycle this bill is at today: the oldest one still unpaid, and
// the month itself when nothing is behind.
//
// The oldest unpaid wins because a paid August must not hide a July nobody
// paid — which is the whole reason a payment carries the cycle it was for.
func (b Bill) Current(today time.Time) Occurrence {
	cycles := b.cycles(today)
	if !b.Active {
		occ := b.Occurrence(cycles[len(cycles)-1], today)
		occ.Status, occ.Late = StatusArchived, 0
		return occ
	}
	for _, c := range cycles {
		if occ := b.Occurrence(c, today); occ.Status == StatusOverdue || occ.Status == StatusOpen {
			return occ
		}
	}
	// Nothing owing: this month is what there is to say, whether it is settled
	// or has not started yet.
	return b.Occurrence(cycles[len(cycles)-1], today)
}

// cycles is every month this bill could owe for, oldest first: from the month
// it was made in, floored at LookBack months back, through the month of today.
// It always has at least one entry, so Current can index the last of it.
func (b Bill) cycles(today time.Time) []string {
	last := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
	first := last.AddDate(0, -LookBack, 0)
	if made, err := time.Parse(DateLayout, day(b.CreatedAt)); err == nil && made.After(first) {
		first = time.Date(made.Year(), made.Month(), 1, 0, 0, 0, 0, today.Location())
	}

	var out []string
	for d := first; !d.After(last); d = d.AddDate(0, 1, 0) {
		out = append(out, d.Format(CycleLayout))
	}
	return out
}

// day is the date out of a stored timestamp. SQLite writes them as
// "2026-08-13 09:12:00", so the day is everything up to the space.
func day(stamp string) string {
	date, _, _ := strings.Cut(stamp, " ")
	return date
}

// CycleOf is the month a date falls in, which is what the pay form starts from
// when a bill has nothing outstanding. It lives in transactions, beside the
// column it writes; this is the name this package's own callers reach for.
func CycleOf(date string) string { return transactions.CycleOf(date) }

// ParseCycle accepts YYYY-MM and hands back the canonical form.
func ParseCycle(s string) (string, error) { return transactions.ParseCycle(s) }

// Validate is the store boundary guard. It has to hold on its own: huh returns
// without running its validators when stdin ends mid-form, so the form alone
// cannot keep a broken row out of the database.
func (b Bill) Validate() error {
	if err := core.ValidateCode(b.Code); err != nil {
		return err
	}
	if err := core.ValidateName(b.Name); err != nil {
		return err
	}
	// Zero is a bill nobody has seen a number for yet. Below zero is a bill that
	// pays you, and that is not a bill.
	if b.Expected < 0 {
		return errors.New("what a bill costs cannot be negative")
	}
	if err := validateDay(b.OpenDay); err != nil {
		return err
	}
	if err := validateDay(b.DueDay); err != nil {
		return err
	}
	if (b.Account.ID == 0) == (b.Card.ID == 0) {
		return errors.New("a bill is paid from either an account or a credit card, never both and never neither")
	}
	if len(b.Tags) > transactions.MaxTags {
		return fmt.Errorf("a bill carries at most %d tags", transactions.MaxTags)
	}
	return nil
}

func validateDay(d int) error {
	if d < 1 || d > 31 {
		return fmt.Errorf("day must be between 1 and 31, not %d", d)
	}
	return nil
}

// Summary is what a bill has really cost, over the payments it is given. An
// energy bill is a different number every month, and the average is the number
// worth budgeting against.
type Summary struct {
	Count         int
	Total         int64
	Avg, Min, Max int64
}

// Stats sums a bill's payments. Integer division for the average, like every
// other amount in pecunia — no float ever touches money.
func Stats(ts []transactions.Transaction) Summary {
	var s Summary
	for _, t := range ts {
		s.Count++
		s.Total += t.Value
		if s.Min == 0 || t.Value < s.Min {
			s.Min = t.Value
		}
		if t.Value > s.Max {
			s.Max = t.Value
		}
	}
	if s.Count > 0 {
		s.Avg = s.Total / int64(s.Count)
	}
	return s
}
