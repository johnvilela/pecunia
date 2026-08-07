package cards

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"kakei/internal/core"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const columns = `id, code, name, description, color, credit_limit, balance, currency,
	closing_day, due_day, created_at, updated_at`

func scan(row interface{ Scan(...any) error }) (Card, error) {
	var c Card
	err := row.Scan(&c.ID, &c.Code, &c.Name, &c.Description, &c.Color, &c.Limit,
		&c.Balance, &c.Currency, &c.ClosingDay, &c.DueDay, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func (s *Store) List() ([]Card, error) {
	rows, err := s.db.Query(`SELECT ` + columns + ` FROM credit_cards ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Card
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Get(id int64) (Card, error) {
	c, err := scan(s.db.QueryRow(`SELECT `+columns+` FROM credit_cards WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (s *Store) ByCode(code string) (Card, error) {
	c, err := scan(s.db.QueryRow(`SELECT `+columns+` FROM credit_cards WHERE code = ?`, core.NormalizeCode(code)))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// Resolve looks a reference up as an id when it is all digits, otherwise as a
// code — that is what lets every command take {CODE|ID}.
func (s *Store) Resolve(ref string) (Card, error) {
	ref = strings.TrimSpace(ref)
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		return s.Get(id)
	}
	return s.ByCode(ref)
}

func (s *Store) Create(c *Card) error {
	if err := core.ValidateName(c.Name); err != nil {
		return err
	}
	c.Code = core.NormalizeCode(c.Code)
	res, err := s.db.Exec(
		`INSERT INTO credit_cards (code, name, description, color, credit_limit, balance,
		 currency, closing_day, due_day) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Code, c.Name, c.Description, c.Color, c.Limit, c.Balance, c.Currency,
		c.ClosingDay, c.DueDay)
	if err != nil {
		return core.CodeErr(err, c.Code)
	}
	c.ID, err = res.LastInsertId()
	return err
}

func (s *Store) Update(c Card) error {
	if err := core.ValidateName(c.Name); err != nil {
		return err
	}
	c.Code = core.NormalizeCode(c.Code)
	res, err := s.db.Exec(
		`UPDATE credit_cards SET code = ?, name = ?, description = ?, color = ?,
		 credit_limit = ?, balance = ?, currency = ?, closing_day = ?, due_day = ?,
		 updated_at = datetime('now') WHERE id = ?`,
		c.Code, c.Name, c.Description, c.Color, c.Limit, c.Balance, c.Currency,
		c.ClosingDay, c.DueDay, c.ID)
	if err != nil {
		return core.CodeErr(err, c.Code)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM credit_cards WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CodeTaken(code string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM credit_cards WHERE code = ?`, core.NormalizeCode(code)).Scan(&n)
	return n > 0, err
}

// SuggestCode returns a free code to pre-fill the form with.
//
// ponytail: the same loop as accounts.Store.SuggestCode. Two copies beat
// threading a "is this taken" callback through core for one extra caller.
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
