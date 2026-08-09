// Package transactions holds the transaction domain: the model, its storage and
// its UI. A transaction is the only thing in kakei that moves money, so this is
// the only package whose writes reach past their own table — into the balance of
// the account or credit card the transaction is filed against.
package transactions

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"kakei/internal/core"
)

// The two directions money goes. value is always positive, so this is what
// carries the sign — nothing has to agree on what a negative number meant.
const (
	KindIncome  = "income"
	KindOutcome = "outcome"
)

// MaxTags is how many tags one transaction may carry.
const MaxTags = 5

// DateLayout is the one date format kakei reads or writes, everywhere.
const DateLayout = "2006-01-02"

// Ref is a resolved pointer to another module's row. The id is what a write
// uses; the code, name and color are what the store's joins fill in, so a listed
// transaction can be rendered without going back to the database.
type Ref struct {
	ID    int64
	Code  string
	Name  string
	Color string
}

func (r Ref) Col() core.Color { return core.ColorByName(r.Color) }

type Transaction struct {
	ID          int64
	Title       string
	Description string
	Value       int64  // minor units at Currency's scale, always positive
	Kind        string // KindIncome or KindOutcome
	Date        string // YYYY-MM-DD
	Tags        []string
	Category    Ref // ID 0 when the transaction has no category
	Account     Ref // exactly one of Account.ID and Card.ID is set
	Card        Ref
	Currency    string // inherited from whichever target is set
	CreatedAt   string
	UpdatedAt   string
}

var ErrNotFound = errors.New("transaction not found")

func (t Transaction) IsCard() bool { return t.Card.ID != 0 }

// Target is the account or credit card the money moved through.
func (t Transaction) Target() Ref {
	if t.IsCard() {
		return t.Card
	}
	return t.Account
}

func (t Transaction) Cur() core.Currency { return core.CurrencyByCode(t.Currency) }
func (t Transaction) Amount() string     { return core.FormatAmount(t.Value, t.Cur()) }

// Signed is how much an account's balance moves: an account holds money, so
// spending lowers it.
func (t Transaction) Signed() int64 {
	if t.Kind == KindOutcome {
		return -t.Value
	}
	return t.Value
}

// CardDelta is how much a credit card's balance moves. A card's balance is what
// the open invoice owes, so it runs the other way from an account's: spending
// raises the debt and paying the bill lowers it.
func (t Transaction) CardDelta() int64 { return -t.Signed() }

// ValidateTitle is the guard on the one field nothing can default. It is here
// rather than core because core.ValidateName says "name", and a transaction has
// a title.
//
// ponytail: near-copy of core.ValidateName. Lift both into
// core.Required(field, s) if a third field ever needs the same sentence.
func ValidateTitle(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("title is required")
	}
	return nil
}

// Validate is the store boundary guard. It has to hold on its own: huh returns
// without running its validators when stdin ends mid-form, so the form alone
// cannot keep a broken row out of the database.
func (t Transaction) Validate() error {
	if err := ValidateTitle(t.Title); err != nil {
		return err
	}
	if t.Value <= 0 {
		return errors.New("amount must be more than zero")
	}
	if t.Kind != KindIncome && t.Kind != KindOutcome {
		return fmt.Errorf("kind must be income or outcome, not %q", t.Kind)
	}
	if _, err := ParseDate(t.Date); err != nil {
		return err
	}
	if (t.Account.ID == 0) == (t.Card.ID == 0) {
		return errors.New("a transaction belongs to either an account or a credit card, never both and never neither")
	}
	if len(t.Tags) > MaxTags {
		return fmt.Errorf("a transaction carries at most %d tags", MaxTags)
	}
	return nil
}

// NormalizeTags is what makes a tag reusable: trimmed, lowercased so "Food" and
// "food" are the same tag in the autocomplete, deduped, and sorted so two equal
// sets of tags compare equal. Commas go too — they are what separates one tag
// from the next everywhere a set of them is typed or read back.
func NormalizeTags(tags []string) []string {
	var out []string
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(tag, ",", "")))
		if tag == "" || slices.Contains(out, tag) {
			continue
		}
		out = append(out, tag)
	}
	slices.Sort(out)
	return out
}

// ParseTags reads the comma-separated list the form's free-text tag field takes.
func ParseTags(s string) []string { return NormalizeTags(strings.Split(s, ",")) }

// ParseDate accepts YYYY-MM-DD and nothing else, and hands back the canonical
// form. Rejecting 2026-02-30 is time.Parse's own doing.
func ParseDate(s string) (string, error) {
	d, err := time.Parse(DateLayout, strings.TrimSpace(s))
	if err != nil {
		return "", fmt.Errorf("date must be YYYY-MM-DD, like %s", Today())
	}
	return d.Format(DateLayout), nil
}

// Today is what a new transaction's date starts as.
func Today() string { return time.Now().Format(DateLayout) }
