// Package budgets holds the budget domain: the model, its storage and its UI.
//
// A budget is a monthly cap on what one category may cost. It keeps no money
// and records no spend — what it is at is the sum of the transactions filed
// under its category in that month, which the store works out on every read,
// the same call goals made.
//
// Nothing links a transaction to a budget. A goal needs its own column because
// linking one is a choice nobody can infer; a budget needs none, because the
// category, the date and the kind already say everything it wants to know. That
// is why this package reads the transactions table through its store rather
// than owning any link of its own — and why recording a transaction never has
// to think about budgets at all.
package budgets

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"kakei/internal/core"
	"kakei/internal/transactions"
)

// CycleLayout is how the month a budget is read for is written, the same
// spelling a recurring bill's payment uses.
const CycleLayout = transactions.CycleLayout

// DateLayout is the one date format kakei reads or writes, everywhere.
const DateLayout = transactions.DateLayout

// The states one month of a budget can be in. Together they are a function of
// the calendar and the ledger, so nothing is stored and nothing can drift.
//
// The pace is what separates the first three: a cap is only reached at the end
// of the month, so R$800.00 spent on the 1st and the same figure on the 31st
// are very different news, and a bare number cannot tell them apart.
const (
	StatusOnTrack = "on track" // at or under what the month has earned by today
	StatusAhead   = "ahead"    // past the pace, still inside the cap
	StatusOver    = "over"     // past the cap
	// StatusArchived is a state of the budget itself, not of a month: a cap
	// nobody is tracking any more is neither on track nor over.
	StatusArchived = "archived"
)

var ErrNotFound = errors.New("budget not found")

// Budget is one monthly cap. Cycle and Spent are the reading, not the row:
// both are set by the store on every read, and neither is ever a column.
type Budget struct {
	ID          int64
	Code        string
	Name        string
	Description string
	Color       string
	// Amount is the cap, in minor units at Currency's scale.
	Amount int64
	// Currency is the budget's own, like a goal's. Only transactions in it are
	// counted: centavos and satoshis do not add up, and there is no rate
	// anywhere in kakei to make them.
	Currency string
	Category transactions.Ref
	Active   bool
	// Cycle is the month this reading is for, as YYYY-MM, and Spent is what the
	// store summed for it — outcome less income, so a refund gives the budget
	// its money back.
	Cycle     string
	Spent     int64
	CreatedAt string
	UpdatedAt string
}

// CycleSpend is what one month cost, which is what a budget's history is a list
// of. It carries no cap: the cap is the budget's, and it is the same number
// across every month here.
type CycleSpend struct {
	Cycle string // YYYY-MM
	Spent int64
}

// AmountChange is one move of a budget's cap, with the day it happened and the
// reason if one was given. Previous is carried alongside Amount so an entry
// explains itself without walking back through the ones before it.
type AmountChange struct {
	ID        int64
	Previous  int64
	Amount    int64
	Note      string
	CreatedAt string
}

func (b Budget) Cur() core.Currency { return core.CurrencyByCode(b.Currency) }
func (b Budget) Col() core.Color    { return core.ColorByName(b.Color) }

// Fmt renders any of the budget's amounts with its currency symbol.
func (b Budget) Fmt(v int64) string { return b.Cur().Symbol + core.FormatAmount(v, b.Cur()) }

// Remaining is what is left of the cap. It goes negative once the budget is
// past it, which is a real state and not one to hide — nothing clamps it.
func (b Budget) Remaining() int64 { return b.Amount - b.Spent }

func (b Budget) Over() bool { return b.Spent > b.Amount }

// Pace is what should have been spent by today if the month were spent evenly:
// the cap, times the days gone, over the days there are.
//
// Integer arithmetic, like every other amount in kakei — no float ever touches
// money. A month that has not started paces at nothing and one that is over
// paces at the whole cap, which is what makes every case downstream a
// comparison rather than a calendar question.
func (b Budget) Pace(today time.Time) int64 {
	start, err := time.Parse(CycleLayout, b.Cycle)
	if err != nil {
		return 0
	}
	// Day 0 of the next month is the last day of this one, which is how the
	// stdlib spells "how long is February".
	days := time.Date(start.Year(), start.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()

	var elapsed int
	switch y, m := today.Year(), today.Month(); {
	case y < start.Year() || (y == start.Year() && m < start.Month()):
		elapsed = 0
	case y > start.Year() || m > start.Month():
		elapsed = days
	default:
		elapsed = today.Day()
	}
	return b.Amount * int64(elapsed) / int64(days)
}

// Drift is how far off the pace the month is running. Positive is spending
// faster than the month, which is the number worth acting on while there is
// still month left to act in.
func (b Budget) Drift(today time.Time) int64 { return b.Spent - b.Pace(today) }

// Status is where this month stands. Over the cap outranks the pace: a budget
// already spent is past being ahead of anything.
func (b Budget) Status(today time.Time) string {
	switch {
	case !b.Active:
		return StatusArchived
	case b.Over():
		return StatusOver
	case b.Spent > b.Pace(today):
		return StatusAhead
	default:
		return StatusOnTrack
	}
}

// Pct is how much of the cap has gone, and unlike the bar it is not clamped:
// 180% is news, and a full bar cannot tell it from 100%.
func (b Budget) Pct() int64 {
	if b.Amount <= 0 {
		return 0
	}
	return b.Spent * 100 / b.Amount
}

// Validate is the store boundary guard. It has to hold on its own: huh returns
// without running its validators when stdin ends mid-form, so the form alone
// cannot keep a broken row out of the database.
func (b Budget) Validate() error {
	if err := core.ValidateCode(b.Code); err != nil {
		return err
	}
	if err := core.ValidateName(b.Name); err != nil {
		return err
	}
	if b.Amount <= 0 {
		return errors.New("a budget must be more than zero — a category costing nothing needs no cap")
	}
	// Unlike an account, a budget is a promise about an amount, and
	// CurrencyByCode falls back rather than failing — a hand-edited BTC budget
	// would quietly start reading its satoshis as cents.
	if !slices.ContainsFunc(core.Currencies, func(c core.Currency) bool { return c.Code == b.Currency }) {
		return fmt.Errorf("%q is not a currency kakei knows", b.Currency)
	}
	// A budget with no category caps nothing at all. This is the one reference
	// in kakei that is required rather than offered.
	if b.Category.ID == 0 {
		return errors.New("a budget caps one category — pick the one it is for")
	}
	return nil
}

// CycleRange is the first and last day of a cycle, which is what the spend
// query compares against. Dates are compared as text everywhere in kakei:
// YYYY-MM-DD sorts the way it reads, and text is what lets the index be used —
// strftime on the column would scan every row instead.
func CycleRange(cycle string) (string, string, error) {
	start, err := time.Parse(CycleLayout, cycle)
	if err != nil {
		return "", "", fmt.Errorf("a cycle is a month, written YYYY-MM, not %q", cycle)
	}
	last := time.Date(start.Year(), start.Month()+1, 0, 0, 0, 0, 0, time.UTC)
	return start.Format(DateLayout), last.Format(DateLayout), nil
}

// ParseCycle accepts YYYY-MM and hands back the canonical form.
func ParseCycle(s string) (string, error) { return transactions.ParseCycle(s) }

// CycleOf is the month a date falls in, which is what an unasked-for cycle
// defaults to.
func CycleOf(date string) string { return transactions.CycleOf(date) }

// ThisCycle is the month today falls in.
func ThisCycle(today time.Time) string { return today.Format(CycleLayout) }
