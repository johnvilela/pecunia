// Package cards holds the credit card domain: the model, its storage and its
// UI. Currencies, colors, codes and amounts come from kakei/internal/core.
package cards

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"kakei/internal/core"
)

// Card is one credit card. Limit and Balance are minor units at the currency's
// scale, like an account's balance; Balance is what the open invoice already
// owes, so Available is what is left to spend.
type Card struct {
	ID          int64
	Code        string
	Name        string
	Description string
	Color       string
	Limit       int64
	Balance     int64
	Currency    string
	ClosingDay  int // day of the month, every month
	DueDay      int
	// OverLimitAllowed is whether the issuer lets the card be used past its
	// limit. Off by default, and while it is off the balance may not exceed
	// the limit at all.
	OverLimitAllowed bool
	CreatedAt        string
	UpdatedAt        string
}

func (c Card) Cur() core.Currency { return core.CurrencyByCode(c.Currency) }
func (c Card) Col() core.Color    { return core.ColorByName(c.Color) }

// Available is what is left to spend. It goes negative when the card is over
// its limit, which is a real state — nothing clamps it.
func (c Card) Available() int64 { return c.Limit - c.Balance }

// Fmt renders any of the card's amounts with its currency symbol.
func (c Card) Fmt(v int64) string { return c.Cur().Symbol + core.FormatAmount(v, c.Cur()) }

var ErrNotFound = errors.New("credit card not found")

// ValidateBalance keeps a card that declines at its limit from carrying a
// balance past it. A card whose issuer allows over-limit use may go past, and
// nothing caps how far.
func (c Card) ValidateBalance() error {
	if !c.OverLimitAllowed && c.Balance > c.Limit {
		return fmt.Errorf("balance %s is over the %s limit — raise the limit, or allow this card to be used over it",
			c.Fmt(c.Balance), c.Fmt(c.Limit))
	}
	return nil
}

// ParseDay reads a day of the month. 29 to 31 are allowed — real cards do bill
// on the 30th — and NextDate is what clamps them in a short month.
func ParseDay(s string) (int, error) {
	d, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("day must be a number between 1 and 31")
	}
	if d < 1 || d > 31 {
		return 0, fmt.Errorf("day must be between 1 and 31")
	}
	return d, nil
}

// NextDate is the next time day comes round, counting today. A day the month is
// too short for lands on its last day: a card that closes on the 31st closes on
// the 28th in February.
func NextDate(from time.Time, day int) time.Time {
	y, m := from.Year(), from.Month()
	if clamp(y, m, day) < from.Day() {
		// m+1 rather than AddDate: adding a month to the 31st overshoots into
		// the one after, since Go normalizes 31 September into 1 October.
		next := time.Date(y, m+1, 1, 0, 0, 0, 0, from.Location())
		y, m = next.Year(), next.Month()
	}
	return time.Date(y, m, clamp(y, m, day), 0, 0, 0, 0, from.Location())
}

// AddMonths is d moved n months, keeping its day of the month and clamping it
// into a month too short for it: 31 January plus one month is 28 February.
// time.AddDate alone normalizes instead of clamping, which turns that into
// 3 March — and an installment series built on it drifts a day at a time.
func AddMonths(d time.Time, n int) time.Time {
	// Day 1 of the target month, so the shift itself can never overflow; the day
	// is put back afterwards, clamped.
	m := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, d.Location()).AddDate(0, n, 0)
	return time.Date(m.Year(), m.Month(), clamp(m.Year(), m.Month(), d.Day()),
		0, 0, 0, 0, d.Location())
}

// clamp caps day at the last day of that month. Day 0 of the next month is the
// last day of this one, which is how the stdlib spells "how long is February".
func clamp(y int, m time.Month, day int) int {
	last := time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if day > last {
		return last
	}
	return day
}
