package transactions

import (
	"database/sql"
	"errors"
	"fmt"

	"kakei/internal/logs"
)

// Transfer is money moving between two accounts you own, as one thing. It is
// stored as two rows — an outcome on From and an income on To, sharing the
// group — but nothing outside this file has to know that: it is recorded,
// edited and deleted whole.
//
// FromValue and ToValue are each in their own side's minor units and need not
// match. Different currencies is the obvious case, and the rate is used once
// and never stored; the same currency with the two differing is a fee, which is
// the honest way to record one without a field pretending it is something else.
type Transfer struct {
	// Group is the id of the outgoing leg, and so the id of the transfer. Zero
	// until it is written.
	Group                    int64
	Title, Description, Date string
	Tags                     []string
	From, To                 Ref // accounts, never cards
	FromValue, ToValue       int64
	// Goal is what the arriving money feeds, and is carried on that leg alone.
	Goal Ref
}

// Validate is the store boundary guard for a transfer as a whole. The per-leg
// rules are Transaction.Validate's; these are the ones that only make sense
// with both ends in view.
func (t Transfer) Validate() error {
	if err := ValidateTitle(t.Title); err != nil {
		return err
	}
	if t.From.ID == 0 || t.To.ID == 0 {
		return errors.New("a transfer goes from one account to another — name both")
	}
	if t.From.ID == t.To.ID {
		return errors.New("a transfer into the same account it left is not a movement")
	}
	if t.FromValue <= 0 || t.ToValue <= 0 {
		return errors.New("both sides of a transfer must be more than zero")
	}
	if _, err := ParseDate(t.Date); err != nil {
		return err
	}
	return nil
}

// legs is the transfer as the two transactions it is stored as. The outgoing
// one comes first, because it is written first and its id becomes the group.
func (t Transfer) legs() []Transaction {
	out := Transaction{
		Title: t.Title, Description: t.Description, Date: t.Date, Tags: t.Tags,
		Value: t.FromValue, Kind: KindOutcome, Account: t.From,
		TransferGroup: t.Group,
	}
	in := Transaction{
		Title: t.Title, Description: t.Description, Date: t.Date, Tags: t.Tags,
		Value: t.ToValue, Kind: KindIncome, Account: t.To,
		Goal: t.Goal, TransferGroup: t.Group,
	}
	return []Transaction{out, in}
}

// Transfer records one, writing both legs and moving both balances — or
// neither. It is its own path rather than a use of Create because the two share
// a column and nothing else: an installment series shares everything but its
// date and its value, and the legs of a transfer share almost nothing but the
// title, the description, the date and the tags.
func (s *Store) Transfer(t *Transfer) error {
	t.Tags = NormalizeTags(t.Tags)
	if err := t.Validate(); err != nil {
		return err
	}

	// Both legs are checked before either is written, so a transfer into a
	// frozen account never half-happens.
	legs := t.legs()
	for i := range legs {
		if err := s.fillGoalCurrency(&legs[i]); err != nil {
			return err
		}
		// The group is not known until the first row exists, so the transfer
		// rules are asserted here rather than waiting for a value Validate
		// cannot see yet.
		legs[i].TransferGroup = 1
		if err := legs[i].Validate(); err != nil {
			return err
		}
		legs[i].TransferGroup = 0
		if err := s.refuseFrozen(legs[i]); err != nil {
			return err
		}
	}

	return s.inTx(func(tx *sql.Tx) error {
		var group int64
		for i, leg := range legs {
			leg.TransferGroup = group
			id, err := insertRow(tx, leg)
			if err != nil {
				return err
			}
			if i == 0 {
				// The group is the outgoing leg's own id, which does not exist
				// until the row does — the same shape installment_group has.
				group, t.Group = id, id
				if _, err := tx.Exec(
					`UPDATE transactions SET transfer_group = ? WHERE id = ?`, id, id); err != nil {
					return err
				}
			}
			if err := writeTags(tx, id, leg.Tags); err != nil {
				return err
			}
			if err := applyBalance(tx, leg, 1); err != nil {
				return err
			}
		}
		// One action, however the legs fell — the legs themselves stay
		// unlogged, or every transfer would read as two things happening.
		return logs.Record(tx, logs.Actor, "created", "transfer", t.Group)
	})
}

// GetTransfer reads one back as the single thing it is, which is what the edit
// form takes. The group names the outgoing leg, so the direction never has to
// be guessed at.
func (s *Store) GetTransfer(group int64) (Transfer, error) {
	rows, err := s.legsOf(group)
	if err != nil {
		return Transfer{}, err
	}

	t := Transfer{Group: group}
	for _, leg := range rows {
		if leg.Kind == KindOutcome {
			t.Title, t.Description, t.Date, t.Tags = leg.Title, leg.Description, leg.Date, leg.Tags
			t.From, t.FromValue = leg.Account, leg.Value
			continue
		}
		t.To, t.ToValue, t.Goal = leg.Account, leg.Value, leg.Goal
	}
	return t, nil
}

// UpdateTransfer rewrites both legs together. Each leg's old balance is put
// back before its new one is applied, so changing an amount, or either end,
// leaves every balance right — the same order Update uses, for the same reason.
func (s *Store) UpdateTransfer(t Transfer) error {
	t.Tags = NormalizeTags(t.Tags)
	if err := t.Validate(); err != nil {
		return err
	}
	if t.Group == 0 {
		return ErrNotFound
	}
	// Read back as the one thing it is, so the trail diffs a transfer against a
	// transfer rather than leg against leg.
	was, err := s.GetTransfer(t.Group)
	if err != nil {
		return err
	}

	old, err := s.legsOf(t.Group)
	if err != nil {
		return err
	}

	// The stored rows keep their ids; everything else comes from the edit.
	next := t.legs()
	for i := range next {
		next[i].TransferGroup = t.Group
		for _, o := range old {
			if o.Kind == next[i].Kind {
				next[i].ID = o.ID
			}
		}
		if next[i].ID == 0 {
			return ErrNotFound
		}
		if err := s.fillGoalCurrency(&next[i]); err != nil {
			return err
		}
		if err := next[i].Validate(); err != nil {
			return err
		}
		if err := s.refuseFrozen(next[i]); err != nil {
			return err
		}
	}

	return s.inTx(func(tx *sql.Tx) error {
		for _, o := range old {
			if err := applyBalance(tx, o, -1); err != nil {
				return err
			}
		}
		for _, leg := range next {
			if err := updateRow(tx, leg); err != nil {
				return err
			}
			if err := applyBalance(tx, leg, 1); err != nil {
				return err
			}
		}
		return logs.RecordEdit(tx, logs.Actor, "transfer", t.Group, logs.Diff(
			logs.F("title", was.Title, t.Title),
			logs.F("description", was.Description, t.Description),
			logs.F("date", was.Date, t.Date),
			logs.F("from", was.From.ID, t.From.ID),
			logs.F("to", was.To.ID, t.To.ID),
			logs.F("from_value", was.FromValue, t.FromValue),
			logs.F("to_value", was.ToValue, t.ToValue),
			logs.F("goal", was.Goal.ID, t.Goal.ID),
			logs.F("tags", was.Tags, t.Tags),
		))
	})
}

// legsOf is both rows of one transfer, the outgoing one first. Anything that is
// not exactly two rows is a group the store did not write.
func (s *Store) legsOf(group int64) ([]Transaction, error) {
	rows, err := s.db.Query(`SELECT `+columns+from+`
		WHERE t.transfer_group = ?
		ORDER BY t.kind DESC`, group) // 'outcome' sorts after 'income'
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Transaction
	for rows.Next() {
		leg, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, leg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) != 2 {
		return nil, fmt.Errorf("transfer %d has %d leg(s); a transfer is two", group, len(out))
	}
	return out, nil
}

// legsOfTx is legsOf inside an open transaction, which is what a delete needs:
// it has to see the rows as the same statement will change them.
func legsOfTx(tx *sql.Tx, group int64) ([]Transaction, error) {
	rows, err := tx.Query(`SELECT `+columns+from+`
		WHERE t.transfer_group = ?
		ORDER BY t.kind DESC`, group)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Transaction
	for rows.Next() {
		leg, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, leg)
	}
	return out, rows.Err()
}
