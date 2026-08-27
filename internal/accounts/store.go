package accounts

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"kakei/internal/core"
	"kakei/internal/logs"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const columns = `id, code, name, description, color, balance, currency, is_frozen, created_at, updated_at`

func scan(row interface{ Scan(...any) error }) (Account, error) {
	var a Account
	err := row.Scan(&a.ID, &a.Code, &a.Name, &a.Description, &a.Color,
		&a.Balance, &a.Currency, &a.IsFrozen, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

func (s *Store) List() ([]Account, error) {
	// Frozen accounts sort last everywhere they are shown; the list command
	// hides them altogether unless asked for.
	rows, err := s.db.Query(`SELECT ` + columns + ` FROM accounts ORDER BY is_frozen, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Account
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Get(id int64) (Account, error) {
	a, err := scan(s.db.QueryRow(`SELECT `+columns+` FROM accounts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (s *Store) ByCode(code string) (Account, error) {
	a, err := scan(s.db.QueryRow(`SELECT `+columns+` FROM accounts WHERE code = ?`, core.NormalizeCode(code)))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// Resolve looks a reference up as an id when it is all digits, otherwise as a
// code — that is what lets every command take {CODE|ID}.
func (s *Store) Resolve(ref string) (Account, error) {
	ref = strings.TrimSpace(ref)
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		return s.Get(id)
	}
	return s.ByCode(ref)
}

func (s *Store) Create(a *Account) error {
	if err := core.ValidateName(a.Name); err != nil {
		return err
	}
	a.Code = core.NormalizeCode(a.Code)
	res, err := s.db.Exec(
		`INSERT INTO accounts (code, name, description, color, balance, currency) VALUES (?, ?, ?, ?, ?, ?)`,
		a.Code, a.Name, a.Description, a.Color, a.Balance, a.Currency)
	if err != nil {
		return core.CodeErr(err, a.Code)
	}
	if a.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	// ponytail: the trail is a second Exec on the same handle, not a tx around
	// both — one connection and a CLI lifetime keep the gap negligible; wrap
	// the write and its log in a tx if that ever stops being true.
	return logs.Record(s.db, logs.Actor, "created", "account", a.ID)
}

// Update refuses to move an account to another currency while transactions are
// filed against it. Nothing is converted by such a change: the amounts already
// recorded stay the integers they are, and re-reading a balance of centavos as
// satoshis changes what every one of them means without a row having moved —
// the same call goals and budgets make, and for the same reason.
//
// A recurring bill's expected amount is not guarded the same way. It is
// documented as a starting point rather than a fact, and an account with a bill
// paid from it has the transactions to prove it anyway.
func (s *Store) Update(a Account) error {
	if err := core.ValidateName(a.Name); err != nil {
		return err
	}
	old, err := s.Get(a.ID)
	if err != nil {
		return err
	}
	if old.Currency != a.Currency {
		n, err := s.Linked(a.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf(
				"%d transaction(s) are already recorded against this account in %s — move or delete them before changing its currency",
				n, old.Currency)
		}
	}
	a.Code = core.NormalizeCode(a.Code)
	// No balance: after creation only the ledger moves it. A balance change in
	// the edit form is filed as an adjustment transaction by the command layer,
	// which is also what puts it on the trail.
	res, err := s.db.Exec(
		`UPDATE accounts SET code = ?, name = ?, description = ?, color = ?,
		 currency = ?, is_frozen = ?, updated_at = datetime('now') WHERE id = ?`,
		a.Code, a.Name, a.Description, a.Color, a.Currency, a.IsFrozen, a.ID)
	if err != nil {
		return core.CodeErr(err, a.Code)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return logs.RecordEdit(s.db, logs.Actor, "account", a.ID, logs.Diff(
		logs.F("code", old.Code, a.Code),
		logs.F("name", old.Name, a.Name),
		logs.F("description", old.Description, a.Description),
		logs.F("color", old.Color, a.Color),
		logs.F("currency", old.Currency, a.Currency),
		logs.F("frozen", old.IsFrozen, a.IsFrozen),
	))
}

func (s *Store) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM accounts WHERE id = ?`, id)
	if err != nil {
		// A transaction always names exactly one account or card, so the row
		// cannot be pulled out from under one.
		return core.FKErr(err, "this account still has transactions — delete or move them first")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return logs.Record(s.db, logs.Actor, "deleted", "account", id)
}

// ToggleFreeze flips is_frozen and reports the new state.
func (s *Store) ToggleFreeze(id int64) (bool, error) {
	a, err := s.Get(id)
	if err != nil {
		return false, err
	}
	if _, err := s.db.Exec(
		`UPDATE accounts SET is_frozen = ?, updated_at = datetime('now') WHERE id = ?`,
		!a.IsFrozen, id); err != nil {
		return false, err
	}
	return !a.IsFrozen, logs.RecordEdit(s.db, logs.Actor, "account", id,
		logs.Diff(logs.F("frozen", a.IsFrozen, !a.IsFrozen)))
}

func (s *Store) CodeTaken(code string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM accounts WHERE code = ?`, core.NormalizeCode(code)).Scan(&n)
	return n > 0, err
}

// SuggestCode returns a free code to pre-fill the form with.
func (s *Store) SuggestCode() (string, error) { return core.SuggestCode(s.CodeTaken) }

// Linked is how many transactions are filed against this account. It is what
// stands between recorded money and a currency change, and what the form asks
// before offering the currency at all.
func (s *Store) Linked(id int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM transactions WHERE account_id = ?`, id).Scan(&n)
	return n, err
}
