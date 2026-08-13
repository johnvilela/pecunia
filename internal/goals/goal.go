// Package goals holds the goal domain: the model, its storage and its UI.
//
// A goal keeps no money of its own. What it is at is the sum of the
// transactions linked to it, which this package reads straight out of the
// transactions table rather than importing the module that owns it — that
// module imports this one, because a transaction names the goal it feeds.
package goals

import (
	"errors"
	"fmt"
	"slices"

	"kakei/internal/core"
)

// The two things a goal can be. A saving goal climbs when money arrives; a
// paying one climbs when money leaves, so both read as progress toward a
// positive target.
const (
	KindSaving = "saving"
	KindPaying = "paying"
)

type Goal struct {
	ID          int64
	Name        string
	Description string
	Target      int64  // minor units at Currency's scale, always positive
	Currency    string // the goal's own; only transactions in it may be linked
	Kind        string // KindSaving or KindPaying
	// Net is income minus outcome over the linked transactions, straight from
	// SQL and with no idea what kind of goal this is. It is never a column: the
	// store works it out on every read. Progress is what it means once the kind
	// has had its say.
	Net       int64
	CreatedAt string
	UpdatedAt string
}

var ErrNotFound = errors.New("goal not found")

func (g Goal) Cur() core.Currency { return core.CurrencyByCode(g.Currency) }

// Fmt renders any of the goal's amounts with its currency symbol.
func (g Goal) Fmt(v int64) string { return g.Cur().Symbol + core.FormatAmount(v, g.Cur()) }

// Progress is how far along the goal is.
//
// The sign flip lives here rather than in the SQL sum: it is the one thing the
// kind decides, it belongs beside the wording it drives, and here it can be
// tested without a database — which is what lets every view case downstream be
// a struct literal.
func (g Goal) Progress() int64 {
	if g.Kind == KindPaying {
		return -g.Net
	}
	return g.Net
}

// Remaining is what is left to go. It goes negative once the goal is past its
// target, which is a real state and not one to hide — nothing clamps it.
func (g Goal) Remaining() int64 { return g.Target - g.Progress() }

func (g Goal) Reached() bool { return g.Progress() >= g.Target }

// Verb is what this goal's progress is called, so one sentence covers both
// kinds without the caller branching.
func (g Goal) Verb() string {
	if g.Kind == KindPaying {
		return "paid off"
	}
	return "saved"
}

// Validate is the store boundary guard. It has to hold on its own: huh returns
// without running its validators when stdin ends mid-form, so the form alone
// cannot keep a broken row out of the database.
func (g Goal) Validate() error {
	if err := core.ValidateName(g.Name); err != nil {
		return err
	}
	if g.Target <= 0 {
		return errors.New("target must be more than zero")
	}
	if g.Kind != KindSaving && g.Kind != KindPaying {
		return fmt.Errorf("a goal is for saving or paying, not %q", g.Kind)
	}
	// Unlike an account, a goal is a promise about an amount, and CurrencyByCode
	// falls back rather than failing — a hand-edited BTC goal would quietly
	// start reading its satoshis as cents.
	if !slices.ContainsFunc(core.Currencies, func(c core.Currency) bool { return c.Code == g.Currency }) {
		return fmt.Errorf("%q is not a currency kakei knows", g.Currency)
	}
	return nil
}
