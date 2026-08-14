// Package transactions holds the transaction domain: the model, its storage and
// its UI. A transaction is the only thing in kakei that moves money, so this is
// the only package whose writes reach past their own table — into the balance of
// the account or credit card the transaction is filed against.
package transactions

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
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

// MaxInstallments is as far as a purchase may be spread. Five years of bills is
// past any real card, and the cap is what stops a typo writing a thousand rows.
const MaxInstallments = 60

// DateLayout is the one date format kakei reads or writes, everywhere.
const DateLayout = "2006-01-02"

// CycleLayout is how the month a recurring bill's payment is for is written.
const CycleLayout = "2006-01"

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

// Installment is where one charge sits in a series. A purchase split over five
// bills is five real transactions, one per bill, sharing a Group — the id of the
// first of them. Count is stored rather than counted so a row can render "(3/5)"
// on its own, and so the title stays what the user typed.
type Installment struct {
	Group int64
	Seq   int64 // 1-based
	Count int64 // 0 or 1 on an ordinary transaction
}

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
	Installment Installment
	// Goal is what this transaction feeds, ID 0 when it feeds none. Only the id
	// and the name are joined in — a goal has no code and no colour of its own.
	Goal Ref
	// GoalCurrency is that goal's own currency, carried so Validate can refuse a
	// mismatch. The store fills it from the goals table before every write, so a
	// caller that only knew the id is checked too.
	GoalCurrency string
	// PaysBillID is the credit-card bill this transaction pays, 0 when it pays
	// none. A payment is an ordinary outcome on whichever account paid, which is
	// why it never shows up as spending on the card it settles.
	PaysBillID int64
	// Recurring is the recurring bill this pays — energy, Netflix, rent — and is
	// nothing like PaysBillID above it: that one settles a credit-card
	// statement and moves the card's balance, this one is a label saying which
	// monthly cost the money went to. ID 0 when it pays none.
	Recurring Ref
	// Cycle is the month that payment was *for*, as YYYY-MM, which is not always
	// the month it was made in: February's energy bill paid on 3 March carries
	// the cycle 2026-02, and that is what clears February instead of leaving it
	// overdue forever. Empty unless Recurring is set.
	Cycle string
	// TransferGroup ties the two legs of a transfer together — an outcome on the
	// account the money left and an income on the one it reached. It is the id
	// of the outcome leg, so the origin of a transfer is a fact of the data
	// rather than a convention about which row was written first. 0 on
	// everything that is not a transfer, which is nearly everything.
	TransferGroup int64
	// Counterpart is the other leg, filled in by the store's join. It is what
	// lets either row on its own say where the money came from or went to,
	// without a second query and without walking the group.
	Counterpart Counterpart
	CreatedAt   string
	UpdatedAt   string
}

// Counterpart is the far end of a transfer. Value and Currency are the other
// leg's own: they are not this leg's when the currencies differ, or when a fee
// was taken on the way.
type Counterpart struct {
	Ref      Ref
	Value    int64
	Currency string
}

var ErrNotFound = errors.New("transaction not found")

func (t Transaction) IsCard() bool { return t.Card.ID != 0 }

// IsTransfer says whether this row is one leg of money moving between two
// accounts you own. The rules below hang off this rather than off the kind:
// each leg is an ordinary income or outcome, and it is the group that says the
// pair is not really either.
func (t Transaction) IsTransfer() bool { return t.TransferGroup != 0 }

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
	if n := t.Installment.Count; n < 0 || n > MaxInstallments {
		return fmt.Errorf("installments must be between 1 and %d, not %d", MaxInstallments, n)
	}
	// Only a credit card has bills to spread a purchase over.
	if t.IsInstallment() && !t.IsCard() {
		return errors.New("only a credit card purchase can be split into installments")
	}
	// A goal counts one currency and nothing else: adding centavos to satoshis
	// is not a sum, and there is no rate anywhere in kakei to make it one. An
	// empty GoalCurrency fails this too — that is what a caller who only knew
	// the id leaves behind, and letting it through is the whole hole.
	if t.Goal.ID != 0 && t.Currency != t.GoalCurrency {
		return fmt.Errorf("this goal counts %s and the transaction is in %s — link it to a goal in its own currency",
			t.GoalCurrency, t.Currency)
	}
	// A recurring bill's payment says which month it settles, always. Without it
	// a bill paid late leaves the month it was for owing forever, and a cycle on
	// a transaction that pays no bill is a month with nothing to be a month of.
	if (t.Recurring.ID == 0) != (t.Cycle == "") {
		return errors.New("a payment names the bill it pays and the month it is for, or neither")
	}
	if t.Cycle != "" {
		if _, err := time.Parse(CycleLayout, t.Cycle); err != nil {
			return fmt.Errorf("a cycle is a month, written YYYY-MM, not %q", t.Cycle)
		}
	}
	if t.PaysBillID != 0 {
		// The money has to come from somewhere, and a card settling its own bill
		// is a loop.
		if t.IsCard() {
			return errors.New("a bill is paid from an account, not from a credit card")
		}
		if t.Kind != KindOutcome {
			return errors.New("paying a bill is money going out")
		}
	}
	if t.IsTransfer() {
		return t.validateTransfer()
	}
	return nil
}

// validateTransfer is what one leg of a transfer may not claim. A transfer
// counts toward nothing: nothing was earned, nothing was consumed, and the
// money is still yours. Every rule here is that sentence applied to one field.
func (t Transaction) validateTransfer() error {
	// A category that never counts toward a budget is a lie — and it is also
	// what keeps the budgets module out of this entirely, since its spend
	// matches on category_id and a NULL matches nothing.
	if t.Category.ID != 0 {
		return errors.New("a transfer carries no category — it is not spending, so nothing should count it")
	}
	if t.IsCard() {
		return errors.New("a transfer moves money between accounts — to pay a card, use its bill")
	}
	if t.PaysBillID != 0 || t.Recurring.ID != 0 {
		return errors.New("settling a bill is not a transfer")
	}
	if t.IsInstallment() {
		return errors.New("a transfer is not split into installments — there is nothing to spread")
	}
	// Money arriving somewhere is what counts toward a goal. The same movement
	// must not also climb a goal for paying something down, which is what
	// letting the outgoing leg carry one would do.
	if t.Goal.ID != 0 && t.Kind != KindIncome {
		return errors.New("a transfer feeds a goal on the leg the money arrives on, not the one it leaves")
	}
	return nil
}

// IsInstallment says whether this row is one of a split purchase. A count of 1
// is a purchase that happens to have been left at the default, not a series.
func (t Transaction) IsInstallment() bool { return t.Installment.Count > 1 }

// SplitInstallments divides total into n parts that add back up to it exactly.
// The remainder rides on the first: it is the one already agreed at the till,
// and the ones after it are the round number the card statement will show.
//
// Integer arithmetic throughout — no float ever touches an amount.
func SplitInstallments(total int64, n int) []int64 {
	if n < 1 {
		n = 1
	}
	each := total / int64(n)
	out := make([]int64, n)
	for i := range out {
		out[i] = each
	}
	out[0] += total - each*int64(n)
	return out
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

// ParseInstallments reads the form's installment count. Blank is one charge,
// which is what an untouched field means.
func ParseInstallments(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 1, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > MaxInstallments {
		return 0, fmt.Errorf("installments must be a number between 1 and %d", MaxInstallments)
	}
	return n, nil
}

// ParseDate accepts YYYY-MM-DD and nothing else, and hands back the canonical
// form. Rejecting 2026-02-30 is time.Parse's own doing.
func ParseDate(s string) (string, error) {
	d, err := time.Parse(DateLayout, strings.TrimSpace(s))
	if err != nil {
		return "", fmt.Errorf("date must be YYYY-MM-DD, like %s", Today())
	}
	return d.Format(DateLayout), nil
}

// ParseCycle accepts YYYY-MM and hands back the canonical form. A full date is
// refused rather than trimmed: someone typing one means a day, and a cycle is a
// month. The unpadded layout is what reads "2026-8" as well as "2026-08".
func ParseCycle(s string) (string, error) {
	d, err := time.Parse("2006-1", strings.TrimSpace(s))
	if err != nil {
		return "", fmt.Errorf("a cycle is a month, written YYYY-MM, like %s",
			time.Now().Format(CycleLayout))
	}
	return d.Format(CycleLayout), nil
}

// CycleOf is the month a date falls in.
func CycleOf(date string) string {
	d, err := time.Parse(DateLayout, strings.TrimSpace(date))
	if err != nil {
		return ""
	}
	return d.Format(CycleLayout)
}

// Today is what a new transaction's date starts as.
func Today() string { return time.Now().Format(DateLayout) }
