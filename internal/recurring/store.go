package recurring

import (
	"database/sql"
	"errors"
	"strings"

	"kakei/internal/core"
	"kakei/internal/transactions"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// columns joins in the three tables a bill points at, so a row comes back
// carrying every code, name and colour it takes to render it — and the currency
// of whatever pays it, which a bill never stores of its own.
//
// COALESCE everywhere because all three joins are outer: a bill may have no
// category, and every bill is missing one of the account and the card.
const columns = `
	b.id, b.code, b.name, b.description, b.color, b.expected, b.open_day, b.due_day, b.active,
	b.created_at, b.updated_at,
	COALESCE(c.id, 0), COALESCE(c.code, ''), COALESCE(c.name, ''), COALESCE(c.color, ''),
	COALESCE(a.id, 0), COALESCE(a.code, ''), COALESCE(a.name, ''), COALESCE(a.color, ''), COALESCE(a.currency, ''),
	COALESCE(k.id, 0), COALESCE(k.code, ''), COALESCE(k.name, ''), COALESCE(k.color, ''), COALESCE(k.currency, ''),
	COALESCE((SELECT group_concat(tag) FROM recurring_bill_tags WHERE bill_id = b.id), '')`

const from = `
	FROM recurring_bills b
	LEFT JOIN categories   c ON c.id = b.category_id
	LEFT JOIN accounts     a ON a.id = b.account_id
	LEFT JOIN credit_cards k ON k.id = b.card_id`

func scanRow(row interface{ Scan(...any) error }) (Bill, error) {
	var b Bill
	var accountCur, cardCur, tags string
	err := row.Scan(&b.ID, &b.Code, &b.Name, &b.Description, &b.Color, &b.Expected,
		&b.OpenDay, &b.DueDay, &b.Active, &b.CreatedAt, &b.UpdatedAt,
		&b.Category.ID, &b.Category.Code, &b.Category.Name, &b.Category.Color,
		&b.Account.ID, &b.Account.Code, &b.Account.Name, &b.Account.Color, &accountCur,
		&b.Card.ID, &b.Card.Code, &b.Card.Name, &b.Card.Color, &cardCur,
		&tags)
	if err != nil {
		return b, err
	}
	// A bill stores no currency of its own: it is in whatever pays it, which is
	// also what its expected amount is read at.
	b.Currency = accountCur
	if b.IsCard() {
		b.Currency = cardCur
	}
	if tags != "" {
		// group_concat gives no order of its own, and a tag can never contain the
		// comma it is joined with — NormalizeTags strips those.
		b.Tags = transactions.NormalizeTags(strings.Split(tags, ","))
	}
	return b, nil
}

// List is every bill with its payments already counted: two queries whatever
// the number of bills, rather than one per bill for the cycles.
//
// archived brings back the ones that have been put away — a cancelled
// subscription stops counting as due, it does not stop existing.
func (s *Store) List(archived bool) ([]Bill, error) {
	query := `SELECT ` + columns + from
	if !archived {
		query += "\n\tWHERE b.active = 1"
	}
	query += "\n\tORDER BY b.name"

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var all []Bill
	for rows.Next() {
		b, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	paid, err := s.tallies(0)
	if err != nil {
		return nil, err
	}
	for i := range all {
		all[i].Payments = paid[all[i].ID]
	}
	return all, nil
}

func (s *Store) Get(id int64) (Bill, error) {
	return s.one(`WHERE b.id = ?`, id)
}

// ByCode is how the command line names a bill: kakei bill ENERG.
func (s *Store) ByCode(code string) (Bill, error) {
	return s.one(`WHERE b.code = ?`, core.NormalizeCode(code))
}

// one reads a single bill, with its cycles, however it was asked for.
func (s *Store) one(where string, arg any) (Bill, error) {
	b, err := scanRow(s.db.QueryRow(`SELECT `+columns+from+"\n\t"+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	if err != nil {
		return b, err
	}
	paid, err := s.tallies(b.ID)
	if err != nil {
		return b, err
	}
	b.Payments = paid[b.ID]
	return b, nil
}

// tallies is what has been paid, by bill and by cycle. billID 0 is every bill,
// which is what the list needs in one round trip.
func (s *Store) tallies(billID int64) (map[int64]map[string]Tally, error) {
	query := `SELECT recurring_id, cycle, SUM(value), COUNT(*)
		FROM transactions WHERE recurring_id IS NOT NULL AND cycle IS NOT NULL`
	var args []any
	if billID != 0 {
		query += ` AND recurring_id = ?`
		args = append(args, billID)
	}
	query += ` GROUP BY recurring_id, cycle`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]map[string]Tally{}
	for rows.Next() {
		var id int64
		var cycle string
		var t Tally
		if err := rows.Scan(&id, &cycle, &t.Value, &t.Count); err != nil {
			return nil, err
		}
		if out[id] == nil {
			out[id] = map[string]Tally{}
		}
		out[id][cycle] = t
	}
	return out, rows.Err()
}

// Payments is one bill's transactions, newest first — what the detail view
// lists and what its averages are worked out over. It goes through the
// transactions store rather than a query of its own: the joins that turn a row
// into something renderable already live there.
func (s *Store) Payments(billID int64) ([]transactions.Transaction, error) {
	return transactions.NewStore(s.db).List(transactions.Filter{RecurringID: billID})
}

func (s *Store) Create(b *Bill) error {
	b.Code = core.NormalizeCode(b.Code)
	b.Tags = transactions.NormalizeTags(b.Tags)
	if err := b.Validate(); err != nil {
		return err
	}

	return s.inTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`INSERT INTO recurring_bills
			   (code, name, description, color, expected, category_id, account_id, card_id,
			    open_day, due_day, active)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
			b.Code, strings.TrimSpace(b.Name), b.Description, b.Color, b.Expected,
			nullID(b.Category.ID), nullID(b.Account.ID), nullID(b.Card.ID), b.OpenDay, b.DueDay)
		if err != nil {
			return core.CodeErr(err, b.Code)
		}
		if b.ID, err = res.LastInsertId(); err != nil {
			return err
		}
		b.Active = true
		return writeTags(tx, b.ID, b.Tags)
	})
}

func (s *Store) Update(b Bill) error {
	b.Code = core.NormalizeCode(b.Code)
	b.Tags = transactions.NormalizeTags(b.Tags)
	if err := b.Validate(); err != nil {
		return err
	}

	return s.inTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`UPDATE recurring_bills SET code = ?, name = ?, description = ?, color = ?, expected = ?,
			   category_id = ?, account_id = ?, card_id = ?, open_day = ?, due_day = ?,
			   updated_at = datetime('now')
			 WHERE id = ?`,
			b.Code, strings.TrimSpace(b.Name), b.Description, b.Color, b.Expected,
			nullID(b.Category.ID), nullID(b.Account.ID), nullID(b.Card.ID),
			b.OpenDay, b.DueDay, b.ID)
		if err != nil {
			return core.CodeErr(err, b.Code)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		if _, err := tx.Exec(`DELETE FROM recurring_bill_tags WHERE bill_id = ?`, b.ID); err != nil {
			return err
		}
		return writeTags(tx, b.ID, b.Tags)
	})
}

// SetActive archives a bill or brings it back. An archived bill stops counting
// as due and keeps every payment it ever took.
func (s *Store) SetActive(id int64, active bool) error {
	res, err := s.db.Exec(
		`UPDATE recurring_bills SET active = ?, updated_at = datetime('now') WHERE id = ?`,
		active, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete is never refused. A bill is a label on the payments made against it,
// so losing it unlinks them — by the ON DELETE SET NULL in the schema — rather
// than taking money that really moved with it.
//
// The cycle goes with the link: a month is only a month *of* something, and a
// payment left carrying one with no bill to name would be a row nothing could
// read back.
func (s *Store) Delete(id int64) error {
	return s.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`UPDATE transactions SET cycle = NULL WHERE recurring_id = ?`, id); err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM recurring_bills WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// Linked is how many payments name this bill, which is what the delete
// confirmation counts.
func (s *Store) Linked(id int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM transactions WHERE recurring_id = ?`, id).Scan(&n)
	return n, err
}

// CodeTaken is what core.SuggestCode asks before offering a code.
func (s *Store) CodeTaken(code string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM recurring_bills WHERE code = ?`,
		core.NormalizeCode(code)).Scan(&n)
	return n > 0, err
}

func (s *Store) inTx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func writeTags(tx *sql.Tx, id int64, tags []string) error {
	for _, tag := range tags {
		if _, err := tx.Exec(
			`INSERT INTO recurring_bill_tags (bill_id, tag) VALUES (?, ?)`, id, tag); err != nil {
			return err
		}
	}
	return nil
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
