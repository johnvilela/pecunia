package transactions

import (
	"database/sql"
	"errors"
	"slices"
	"strings"

	"kakei/internal/cards"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Filter is every axis the list command can narrow by. A zero value is "all",
// and each field set narrows further.
type Filter struct {
	From, To   string // YYYY-MM-DD, both inclusive
	Tag        string
	Search     string // substring of the title or the description
	CategoryID int64
	AccountID  int64
	CardID     int64
}

// columns joins in the three tables a transaction points at, so a row comes back
// carrying every code, name and colour it takes to render it. The alternative
// was three more round trips per listed row, or a ui package that imports the
// other three modules.
//
// COALESCE everywhere because all three joins are outer: an uncategorised
// transaction has no category row, and every transaction is missing one of the
// account and the card.
const columns = `
	t.id, t.title, t.description, t.value, t.kind, t.date, t.created_at, t.updated_at,
	COALESCE(c.id, 0), COALESCE(c.code, ''), COALESCE(c.name, ''), COALESCE(c.color, ''),
	COALESCE(a.id, 0), COALESCE(a.code, ''), COALESCE(a.name, ''), COALESCE(a.color, ''), COALESCE(a.currency, ''),
	COALESCE(k.id, 0), COALESCE(k.code, ''), COALESCE(k.name, ''), COALESCE(k.color, ''), COALESCE(k.currency, ''),
	COALESCE((SELECT group_concat(tag) FROM transaction_tags WHERE transaction_id = t.id), '')`

const from = `
	FROM transactions t
	LEFT JOIN categories   c ON c.id = t.category_id
	LEFT JOIN accounts     a ON a.id = t.account_id
	LEFT JOIN credit_cards k ON k.id = t.card_id`

func scan(row interface{ Scan(...any) error }) (Transaction, error) {
	var t Transaction
	var accountCur, cardCur, tags string
	err := row.Scan(&t.ID, &t.Title, &t.Description, &t.Value, &t.Kind, &t.Date, &t.CreatedAt, &t.UpdatedAt,
		&t.Category.ID, &t.Category.Code, &t.Category.Name, &t.Category.Color,
		&t.Account.ID, &t.Account.Code, &t.Account.Name, &t.Account.Color, &accountCur,
		&t.Card.ID, &t.Card.Code, &t.Card.Name, &t.Card.Color, &cardCur,
		&tags)
	if err != nil {
		return t, err
	}
	t.Currency = accountCur
	if t.IsCard() {
		t.Currency = cardCur
	}
	if tags != "" {
		// group_concat gives no order of its own, and a tag can never contain
		// the comma it is joined with — NormalizeTags strips those.
		t.Tags = strings.Split(tags, ",")
		slices.Sort(t.Tags)
	}
	return t, nil
}

func (s *Store) List(f Filter) ([]Transaction, error) {
	var where []string
	var args []any
	add := func(clause string, v ...any) {
		where = append(where, clause)
		args = append(args, v...)
	}
	if f.From != "" {
		add(`t.date >= ?`, f.From)
	}
	if f.To != "" {
		add(`t.date <= ?`, f.To)
	}
	if f.Tag != "" {
		add(`EXISTS (SELECT 1 FROM transaction_tags WHERE transaction_id = t.id AND tag = ?)`,
			strings.ToLower(strings.TrimSpace(f.Tag)))
	}
	if f.Search != "" {
		// LIKE is case-insensitive for ASCII in SQLite, which is what makes
		// "coffee" find "Coffee" without a lower() on the column.
		like := "%" + f.Search + "%"
		add(`(t.title LIKE ? OR t.description LIKE ?)`, like, like)
	}
	if f.CategoryID != 0 {
		add(`t.category_id = ?`, f.CategoryID)
	}
	if f.AccountID != 0 {
		add(`t.account_id = ?`, f.AccountID)
	}
	if f.CardID != 0 {
		add(`t.card_id = ?`, f.CardID)
	}

	query := `SELECT ` + columns + from
	if len(where) > 0 {
		query += "\n\tWHERE " + strings.Join(where, " AND ")
	}
	// Newest first, and the id breaks the tie so two transactions on the same
	// day always come back in the same order.
	query += "\n\tORDER BY t.date DESC, t.id DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Transaction
	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) Get(id int64) (Transaction, error) {
	t, err := scan(s.db.QueryRow(`SELECT `+columns+from+`
		WHERE t.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// AllTags is every tag any transaction is using, which is what the form offers
// back so a tag gets reused instead of retyped.
func (s *Store) AllTags() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT tag FROM transaction_tags ORDER BY tag`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}

// Create writes the transaction and moves its target's balance, both or
// neither: a card that would be pushed past a limit it may not pass refuses the
// whole thing.
func (s *Store) Create(t *Transaction) error {
	t.Tags = NormalizeTags(t.Tags)
	if err := t.Validate(); err != nil {
		return err
	}
	return s.inTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`INSERT INTO transactions (title, description, category_id, account_id, card_id, value, kind, date)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			t.Title, t.Description, nullID(t.Category.ID), nullID(t.Account.ID), nullID(t.Card.ID),
			t.Value, t.Kind, t.Date)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if err := writeTags(tx, id, t.Tags); err != nil {
			return err
		}
		if err := applyBalance(tx, *t, 1); err != nil {
			return err
		}
		t.ID = id
		return nil
	})
}

// Update reverses what the stored row did to its target before applying what the
// new one does, so an edit that changes the value, flips the kind or moves the
// transaction to another account or card leaves every balance right.
func (s *Store) Update(t Transaction) error {
	t.Tags = NormalizeTags(t.Tags)
	if err := t.Validate(); err != nil {
		return err
	}
	return s.inTx(func(tx *sql.Tx) error {
		old, err := scan(tx.QueryRow(`SELECT `+columns+from+`
			WHERE t.id = ?`, t.ID))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := applyBalance(tx, old, -1); err != nil {
			return err
		}
		if _, err := tx.Exec(
			`UPDATE transactions SET title = ?, description = ?, category_id = ?, account_id = ?,
			 card_id = ?, value = ?, kind = ?, date = ?, updated_at = datetime('now') WHERE id = ?`,
			t.Title, t.Description, nullID(t.Category.ID), nullID(t.Account.ID), nullID(t.Card.ID),
			t.Value, t.Kind, t.Date, t.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM transaction_tags WHERE transaction_id = ?`, t.ID); err != nil {
			return err
		}
		if err := writeTags(tx, t.ID, t.Tags); err != nil {
			return err
		}
		return applyBalance(tx, t, 1)
	})
}

// Delete takes the row away and gives its target the money back. The tags go
// with it, by the cascade in the schema.
func (s *Store) Delete(id int64) error {
	return s.inTx(func(tx *sql.Tx) error {
		old, err := scan(tx.QueryRow(`SELECT `+columns+from+`
			WHERE t.id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM transactions WHERE id = ?`, id); err != nil {
			return err
		}
		return applyBalance(tx, old, -1)
	})
}

// inTx runs fn in one SQL transaction. Every write here touches two tables, so
// nothing may half-happen: a rejected transaction leaves neither a row nor a
// moved balance behind.
func (s *Store) inTx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// nullID turns 0 into a SQL NULL. A row id is never 0, so 0 is how the model
// spells "not set" without every caller juggling a pointer.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func writeTags(tx *sql.Tx, id int64, tags []string) error {
	for _, tag := range tags {
		if _, err := tx.Exec(
			`INSERT INTO transaction_tags (transaction_id, tag) VALUES (?, ?)`, id, tag); err != nil {
			return err
		}
	}
	return nil
}

// applyBalance moves the transaction's target by its value. sign is 1 when the
// transaction is being added and -1 when it is being taken back, which is what
// lets Update reverse the old row before applying the new one.
func applyBalance(tx *sql.Tx, t Transaction, sign int64) error {
	if !t.IsCard() {
		_, err := tx.Exec(
			`UPDATE accounts SET balance = balance + ?, updated_at = datetime('now') WHERE id = ?`,
			sign*t.Signed(), t.Account.ID)
		return err
	}
	if _, err := tx.Exec(
		`UPDATE credit_cards SET balance = balance + ?, updated_at = datetime('now') WHERE id = ?`,
		sign*t.CardDelta(), t.Card.ID); err != nil {
		return err
	}
	return checkLimit(tx, t.Card.ID)
}

// checkLimit reads the card back and asks the cards module whether it would
// stand for the balance it now has. Reusing Card.ValidateBalance is what keeps
// one rule about limits in the codebase rather than two that drift.
func checkLimit(tx *sql.Tx, id int64) error {
	var c cards.Card
	if err := tx.QueryRow(
		`SELECT balance, credit_limit, currency, over_limit_allowed FROM credit_cards WHERE id = ?`, id).
		Scan(&c.Balance, &c.Limit, &c.Currency, &c.OverLimitAllowed); err != nil {
		return err
	}
	return c.ValidateBalance()
}
