package cards

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"pecunia/internal/core"
	"pecunia/internal/logs"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const columns = `id, code, name, description, color, credit_limit, balance, currency,
	closing_day, due_day, over_limit_allowed, created_at, updated_at`

func scan(row interface{ Scan(...any) error }) (Card, error) {
	var c Card
	err := row.Scan(&c.ID, &c.Code, &c.Name, &c.Description, &c.Color, &c.Limit,
		&c.Balance, &c.Currency, &c.ClosingDay, &c.DueDay, &c.OverLimitAllowed,
		&c.CreatedAt, &c.UpdatedAt)
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
	if err := c.ValidateBalance(); err != nil {
		return err
	}
	c.Code = core.NormalizeCode(c.Code)
	res, err := s.db.Exec(
		`INSERT INTO credit_cards (code, name, description, color, credit_limit, balance,
		 currency, closing_day, due_day, over_limit_allowed)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Code, c.Name, c.Description, c.Color, c.Limit, c.Balance, c.Currency,
		c.ClosingDay, c.DueDay, c.OverLimitAllowed)
	if err != nil {
		return core.CodeErr(err, c.Code)
	}
	if c.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	return logs.Record(s.db, logs.Actor, "created", "card", c.ID)
}

// Update refuses to move a card to another currency while charges are recorded
// against it. Nothing is converted by such a change: the limit, the balance and
// every charge already filed stay the integers they are, and re-reading them at
// another scale changes what all three mean without a row having moved — the
// same call accounts, goals and budgets make.
func (s *Store) Update(c Card) error {
	if err := core.ValidateName(c.Name); err != nil {
		return err
	}
	old, err := s.Get(c.ID)
	if err != nil {
		return err
	}
	// The stored balance, not the caller's: an edit never writes one, so the
	// limit is judged against what the card actually holds.
	check := c
	check.Balance = old.Balance
	if err := check.ValidateBalance(); err != nil {
		return err
	}
	if old.Currency != c.Currency {
		n, err := s.Linked(c.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf(
				"%d transaction(s) are already recorded against this card in %s — move or delete them before changing its currency",
				n, old.Currency)
		}
	}
	c.Code = core.NormalizeCode(c.Code)
	// No balance: after creation only the ledger moves it.
	res, err := s.db.Exec(
		`UPDATE credit_cards SET code = ?, name = ?, description = ?, color = ?,
		 credit_limit = ?, currency = ?, closing_day = ?, due_day = ?,
		 over_limit_allowed = ?, updated_at = datetime('now') WHERE id = ?`,
		c.Code, c.Name, c.Description, c.Color, c.Limit, c.Currency,
		c.ClosingDay, c.DueDay, c.OverLimitAllowed, c.ID)
	if err != nil {
		return core.CodeErr(err, c.Code)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return logs.RecordEdit(s.db, logs.Actor, "card", c.ID, logs.Diff(
		logs.F("code", old.Code, c.Code),
		logs.F("name", old.Name, c.Name),
		logs.F("description", old.Description, c.Description),
		logs.F("color", old.Color, c.Color),
		logs.F("limit", old.Limit, c.Limit),
		logs.F("currency", old.Currency, c.Currency),
		logs.F("closing_day", old.ClosingDay, c.ClosingDay),
		logs.F("due_day", old.DueDay, c.DueDay),
		logs.F("over_limit_allowed", old.OverLimitAllowed, c.OverLimitAllowed),
	))
}

func (s *Store) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM credit_cards WHERE id = ?`, id)
	if err != nil {
		// A transaction always names exactly one account or card, so the row
		// cannot be pulled out from under one.
		return core.FKErr(err, "this card still has transactions — delete or move them first")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return logs.Record(s.db, logs.Actor, "deleted", "card", id)
}

func (s *Store) CodeTaken(code string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM credit_cards WHERE code = ?`, core.NormalizeCode(code)).Scan(&n)
	return n > 0, err
}

// SuggestCode returns a free code to pre-fill the form with.
func (s *Store) SuggestCode() (string, error) { return core.SuggestCode(s.CodeTaken) }

// Linked is how many transactions are filed against this card. It is what
// stands between recorded charges and a currency change, and what the form asks
// before offering the currency at all.
func (s *Store) Linked(id int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM transactions WHERE card_id = ?`, id).Scan(&n)
	return n, err
}
