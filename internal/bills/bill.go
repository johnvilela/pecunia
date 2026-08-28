// Package bills holds the credit-card bill domain: one closing cycle of one
// card, everything charged in it, and what has been paid against it.
//
// It reads the transactions table directly rather than importing
// pecunia/internal/transactions — that package imports this one, because a payment
// is a transaction that names a bill.
package bills

import (
	"errors"
	"time"

	"pecunia/internal/cards"
)

// dateLayout is the one date format pecunia reads or writes, everywhere. It is
// spelled out here rather than imported from transactions, which imports this
// package.
const dateLayout = "2006-01-02"

// The four states a bill can be in. open and closed are what the calendar says;
// partial and paid are what the payments say.
const (
	StatusOpen    = "open"
	StatusClosed  = "closed"
	StatusPartial = "partial"
	StatusPaid    = "paid"
)

var ErrNotFound = errors.New("bill not found")

// Bill is one closing cycle of one card. Total is a snapshot — frozen when the
// cycle closes — and Status is stored beside it; Paid, Card and Charges are
// filled in by the store on read.
type Bill struct {
	ID       int64
	ClosesOn string // YYYY-MM-DD, the last day the cycle takes charges
	DueOn    string
	Total    int64 // minor units at the card's scale
	Status   string
	Paid     int64 // summed from the transactions pointing at this bill
	Card     cards.Card
}

// Charge is one transaction inside a bill. Bills cannot use
// transactions.Transaction without an import cycle, and a bill only ever needs
// enough of a row to list it.
type Charge struct {
	ID       int64
	Date     string
	Title    string
	Value    int64
	Kind     string // "income" or "outcome"
	Seq      int64  // installment position, 0 when the charge is not one
	Count    int64
	Category string // the category's code, empty when it has none
}

// Period is the span of dates this bill takes charges from, both ends
// inclusive: the day after the previous closing, through the closing date.
//
// The previous closing is worked out from the *card's* closing day rather than
// by stepping a month back from ClosesOn, because ClosesOn may already be
// clamped. A card closing on the 31st closes on 30 September, and a month back
// from that is 30 August — a day short of the 31 August the previous cycle
// really ended on, which would put 31 August in two bills at once.
func (b Bill) Period() (from, to string) {
	closes, err := time.Parse(dateLayout, b.ClosesOn)
	if err != nil {
		return "", b.ClosesOn
	}
	// From the 1st of the month before, the next closing day is that month's own
	// — clamped by NextDate if the month is too short for it.
	firstOfPrev := time.Date(closes.Year(), closes.Month(), 1, 0, 0, 0, 0, closes.Location()).
		AddDate(0, -1, 0)
	prev := cards.NextDate(firstOfPrev, b.Card.ClosingDay)
	return prev.AddDate(0, 0, 1).Format(dateLayout), b.ClosesOn
}

// Month is the cycle's name — the month it closes in, which is how a statement
// is spoken about out loud ("March's bill") even though its period starts in
// the month before.
func (b Bill) Month() string {
	closes, err := time.Parse(dateLayout, b.ClosesOn)
	if err != nil {
		return ""
	}
	return closes.Format("January")
}

// DueDate is when a cycle closing on closesOn has to be paid. A due day after
// the closing day falls in the same month; one before it falls in the next,
// which is the normal shape of a card that closes late in the month.
func DueDate(closesOn time.Time, dueDay int) time.Time {
	return cards.NextDate(closesOn, dueDay)
}

// StatusFor is the one place the four states are decided, so the writer and the
// reader can never disagree about what "partial" means.
func StatusFor(total, paid int64, closed bool) string {
	switch {
	case !closed:
		return StatusOpen
	// A cycle with nothing on it, or one left in credit by a refund, owes
	// nothing — there is no bill to chase.
	case total <= 0, paid >= total:
		return StatusPaid
	case paid > 0:
		return StatusPartial
	default:
		return StatusClosed
	}
}

// Remaining is what is still owed, never negative — overpaying is real, and
// "R$-95.00 left" is a riddle.
func (b Bill) Remaining() int64 {
	return max(0, b.Total-b.Paid)
}

// Owed is what has to be paid, which is nothing at all while the cycle is still
// open: an open bill is a running total, not a debt. It is what the list column
// shows and what decides whether a bill turns up in `pecunia cc pay`.
func (b Bill) Owed() int64 {
	if b.Status == StatusOpen {
		return 0
	}
	return b.Remaining()
}

func (b Bill) Fmt(v int64) string { return b.Card.Fmt(v) }
