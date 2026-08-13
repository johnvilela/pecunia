package goals

import (
	"database/sql"
	"errors"
	"fmt"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// columns carries the progress back with the row it belongs to. The sum is a
// correlated subquery rather than a join and a GROUP BY, which is what lets
// every method here stay one plain SELECT — with a GROUP BY, a Get for an id
// that is not there comes back as one all-NULL row instead of no rows at all.
//
// The sum is signed income minus outcome and knows nothing about the goal's
// kind: kind carries the direction, value is always positive, and
// Goal.Progress decides what the total means. The 'income' literal is this
// package reading the transactions table directly — see the package comment.
const columns = `g.id, g.name, g.description, g.target, g.currency, g.kind,
	g.created_at, g.updated_at,
	COALESCE((SELECT SUM(CASE WHEN kind = 'income' THEN value ELSE -value END)
	          FROM transactions WHERE goal_id = g.id), 0)`

func scan(row interface{ Scan(...any) error }) (Goal, error) {
	var g Goal
	err := row.Scan(&g.ID, &g.Name, &g.Description, &g.Target, &g.Currency, &g.Kind,
		&g.CreatedAt, &g.UpdatedAt, &g.Net)
	return g, err
}

// List is one query for every goal and every progress, which is the whole
// reason the sum is a subquery in columns rather than a second round trip per
// listed goal.
func (s *Store) List() ([]Goal, error) {
	rows, err := s.db.Query(`SELECT ` + columns + ` FROM goals g ORDER BY g.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var all []Goal
	for rows.Next() {
		g, err := scan(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, g)
	}
	return all, rows.Err()
}

func (s *Store) Get(id int64) (Goal, error) {
	g, err := scan(s.db.QueryRow(`SELECT `+columns+` FROM goals g WHERE g.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

func (s *Store) Create(g *Goal) error {
	if err := g.Validate(); err != nil {
		return err
	}
	res, err := s.db.Exec(
		`INSERT INTO goals (name, description, target, currency, kind) VALUES (?, ?, ?, ?, ?)`,
		g.Name, g.Description, g.Target, g.Currency, g.Kind)
	if err != nil {
		return err
	}
	g.ID, err = res.LastInsertId()
	return err
}

// Update refuses to move a goal to another currency while transactions are
// linked to it. Their amounts were filed in the currency they were filed in,
// and re-reading a total of centavos as satoshis would change what the goal is
// at without a single row having moved.
//
// The kind is not guarded the same way: flipping it inverts the progress, which
// is the honest consequence of a goal that counts money in rather than out.
func (s *Store) Update(g Goal) error {
	if err := g.Validate(); err != nil {
		return err
	}
	old, err := s.Get(g.ID)
	if err != nil {
		return err
	}
	if old.Currency != g.Currency {
		n, err := s.Linked(g.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf(
				"%d transaction(s) are linked to this goal in %s — unlink them before changing its currency",
				n, old.Currency)
		}
	}
	res, err := s.db.Exec(
		`UPDATE goals SET name = ?, description = ?, target = ?, currency = ?, kind = ?,
			updated_at = datetime('now') WHERE id = ?`,
		g.Name, g.Description, g.Target, g.Currency, g.Kind, g.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete is never refused. A goal is a label on the transactions that fed it,
// so losing it unlinks them — by the ON DELETE SET NULL in the schema — rather
// than taking money that really moved with it.
func (s *Store) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM goals WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Linked is how many transactions name this goal. It is what the delete
// confirmation counts, and what stands between a linked goal and a currency
// change.
func (s *Store) Linked(id int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM transactions WHERE goal_id = ?`, id).Scan(&n)
	return n, err
}
