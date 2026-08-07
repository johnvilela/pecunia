package accounts

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"kakei/internal/core"
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
	a.Code = core.NormalizeCode(a.Code)
	res, err := s.db.Exec(
		`INSERT INTO accounts (code, name, description, color, balance, currency) VALUES (?, ?, ?, ?, ?, ?)`,
		a.Code, a.Name, a.Description, a.Color, a.Balance, a.Currency)
	if err != nil {
		return core.CodeErr(err, a.Code)
	}
	a.ID, err = res.LastInsertId()
	return err
}

func (s *Store) Update(a Account) error {
	a.Code = core.NormalizeCode(a.Code)
	res, err := s.db.Exec(
		`UPDATE accounts SET code = ?, name = ?, description = ?, color = ?, balance = ?,
		 currency = ?, is_frozen = ?, updated_at = datetime('now') WHERE id = ?`,
		a.Code, a.Name, a.Description, a.Color, a.Balance, a.Currency, a.IsFrozen, a.ID)
	if err != nil {
		return core.CodeErr(err, a.Code)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM accounts WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
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
	return !a.IsFrozen, nil
}

func (s *Store) CodeTaken(code string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM accounts WHERE code = ?`, core.NormalizeCode(code)).Scan(&n)
	return n > 0, err
}

// SuggestCode returns a free code to pre-fill the form with.
func (s *Store) SuggestCode() (string, error) {
	for range 20 {
		code := core.RandomCode()
		taken, err := s.CodeTaken(code)
		if err != nil {
			return "", err
		}
		if !taken {
			return code, nil
		}
	}
	return "", errors.New("could not find a free code")
}
