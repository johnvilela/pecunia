package budgets

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"kakei/internal/core"
	"kakei/internal/logs"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// spend is what the budget's category cost over one month, in the budget's own
// currency. It rides back with the row it belongs to as a correlated subquery
// rather than a join and a GROUP BY, which is what lets every read here stay
// one plain SELECT — with a GROUP BY, a Get for an id that is not there comes
// back as one all-NULL row instead of no rows at all.
//
// Outcome less income, so a refund gives the budget its money back. The two
// outer joins are the wrinkle goals does not have: a transaction carries no
// currency of its own, so the only way to know whether this budget counts it is
// to reach the account or the card it names.
//
// The dates are compared as text against the cycle's own bounds rather than
// with strftime on the column, which is what lets transactions_category_date be
// used instead of scanned.
const spend = `
	COALESCE((SELECT SUM(CASE WHEN t.kind = 'outcome' THEN t.value ELSE -t.value END)
	          FROM transactions t
	          LEFT JOIN accounts     a ON a.id = t.account_id
	          LEFT JOIN credit_cards k ON k.id = t.card_id
	          WHERE t.category_id = b.category_id
	            AND COALESCE(a.currency, k.currency) = b.currency
	            AND t.date >= ? AND t.date <= ?), 0)`

// columns carries the category the budget caps, so a row comes back with the
// code, name and colour it takes to render it. The join is inner: the schema
// makes a budget without a category impossible, so there is nothing to
// COALESCE away.
const columns = `
	b.id, b.code, b.name, b.description, b.color, b.amount, b.currency, b.active,
	b.created_at, b.updated_at,
	c.id, c.code, c.name, c.color,` + spend

const from = `
	FROM budgets b
	JOIN categories c ON c.id = b.category_id`

func scan(row interface{ Scan(...any) error }, cycle string) (Budget, error) {
	var b Budget
	err := row.Scan(&b.ID, &b.Code, &b.Name, &b.Description, &b.Color, &b.Amount,
		&b.Currency, &b.Active, &b.CreatedAt, &b.UpdatedAt,
		&b.Category.ID, &b.Category.Code, &b.Category.Name, &b.Category.Color,
		&b.Spent)
	b.Cycle = cycle
	return b, err
}

// List is every budget read for one month, spend included: one query whatever
// the number of budgets, which is the whole reason the sum is a subquery.
//
// archived brings back the ones that have been put away — a cap nobody is
// tracking any more stops counting, it does not stop existing.
func (s *Store) List(cycle string, archived bool) ([]Budget, error) {
	from_, to, err := CycleRange(cycle)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + columns + from
	if !archived {
		query += "\n\tWHERE b.active = 1"
	}
	query += "\n\tORDER BY b.name"

	rows, err := s.db.Query(query, from_, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var all []Budget
	for rows.Next() {
		b, err := scan(rows, cycle)
		if err != nil {
			return nil, err
		}
		all = append(all, b)
	}
	return all, rows.Err()
}

func (s *Store) Get(id int64, cycle string) (Budget, error) {
	return s.one(cycle, `b.id = ?`, id)
}

func (s *Store) ByCode(code, cycle string) (Budget, error) {
	return s.one(cycle, `b.code = ?`, core.NormalizeCode(code))
}

// Resolve looks a reference up as an id when it is all digits, otherwise as a
// code — that is what lets every command take {CODE|ID}.
func (s *Store) Resolve(ref, cycle string) (Budget, error) {
	ref = strings.TrimSpace(ref)
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		return s.Get(id, cycle)
	}
	return s.ByCode(ref, cycle)
}

func (s *Store) one(cycle, where string, arg any) (Budget, error) {
	from_, to, err := CycleRange(cycle)
	if err != nil {
		return Budget{}, err
	}
	b, err := scan(s.db.QueryRow(`SELECT `+columns+from+`
		WHERE `+where, from_, to, arg), cycle)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

func (s *Store) Create(b *Budget) error {
	b.Code = core.NormalizeCode(b.Code)
	b.Name = strings.TrimSpace(b.Name)
	if err := b.Validate(); err != nil {
		return err
	}
	res, err := s.db.Exec(
		`INSERT INTO budgets (code, name, description, color, amount, currency, category_id, active)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 1)`,
		b.Code, b.Name, b.Description, b.Color, b.Amount, b.Currency, b.Category.ID)
	if err != nil {
		return s.uniqueErr(err, *b)
	}
	if b.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	b.Active = true
	return logs.Record(s.db, logs.Actor, "created", "budget", b.ID)
}

// Update refuses to move a budget to another currency while transactions it
// already counts are filed in the old one. Their amounts were filed in the
// currency they were filed in, and re-reading a total of centavos as satoshis
// would change what the budget is at without a single row having moved — the
// same call goals makes, and for the same reason.
//
// note is why the amount moved, and is kept only when it did: an edit that
// leaves the cap alone has nothing to explain. The write and its log entry go
// in one SQL transaction, so a budget can never end up at a cap its history
// does not account for.
func (s *Store) Update(b Budget, note string) error {
	b.Code = core.NormalizeCode(b.Code)
	b.Name = strings.TrimSpace(b.Name)
	if err := b.Validate(); err != nil {
		return err
	}
	// The cycle is a reading, not a row, so any valid one will do to fetch the
	// old values this compares against.
	old, err := s.Get(b.ID, "2000-01")
	if err != nil {
		return err
	}
	if old.Currency != b.Currency {
		n, err := s.Counted(old.Category.ID, old.Currency)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf(
				"%d transaction(s) under %s are already counted in %s — move the budget's category or its currency, not both",
				n, old.Category.Code, old.Currency)
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE budgets SET code = ?, name = ?, description = ?, color = ?, amount = ?,
		   currency = ?, category_id = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		b.Code, b.Name, b.Description, b.Color, b.Amount, b.Currency, b.Category.ID, b.ID)
	if err != nil {
		return s.uniqueErr(err, b)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if old.Amount != b.Amount {
		if _, err := tx.Exec(
			`INSERT INTO budget_amount_log (budget_id, previous, amount, note) VALUES (?, ?, ?, ?)`,
			b.ID, old.Amount, b.Amount, strings.TrimSpace(note)); err != nil {
			return err
		}
	}
	if err := logs.RecordEdit(tx, logs.Actor, "budget", b.ID, logs.Diff(
		logs.F("code", old.Code, b.Code),
		logs.F("name", old.Name, b.Name),
		logs.F("description", old.Description, b.Description),
		logs.F("color", old.Color, b.Color),
		logs.F("amount", old.Amount, b.Amount),
		logs.F("currency", old.Currency, b.Currency),
		logs.F("category_id", old.Category.ID, b.Category.ID),
	)); err != nil {
		return err
	}
	return tx.Commit()
}

// SetActive puts a budget away or brings it back. An archived cap stops being
// tracked and keeps its history readable, which is not the same as deleting it.
func (s *Store) SetActive(id int64, active bool) error {
	// Any valid cycle will do: only Active is read off the old row.
	old, err := s.Get(id, "2000-01")
	if err != nil {
		return err
	}
	res, err := s.db.Exec(
		`UPDATE budgets SET active = ?, updated_at = datetime('now') WHERE id = ?`, active, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	// Diff drops an equal pair, so archiving the archived records nothing.
	return logs.RecordEdit(s.db, logs.Actor, "budget", id,
		logs.Diff(logs.F("active", old.Active, active)))
}

// Delete is never refused, and takes nothing with it but its own history.
// Nothing ever named the budget: the transactions it counted were filed under a
// category, and the category outlives it.
func (s *Store) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM budgets WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return logs.Record(s.db, logs.Actor, "deleted", "budget", id)
}

// AmountLog is everything this budget's cap has been, newest first. An empty
// log is a budget that has said the same number since the day it was made.
func (s *Store) AmountLog(id int64) ([]AmountChange, error) {
	rows, err := s.db.Query(
		`SELECT id, previous, amount, note, created_at FROM budget_amount_log
		 WHERE budget_id = ? ORDER BY created_at DESC, id DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var all []AmountChange
	for rows.Next() {
		var c AmountChange
		if err := rows.Scan(&c.ID, &c.Previous, &c.Amount, &c.Note, &c.CreatedAt); err != nil {
			return nil, err
		}
		all = append(all, c)
	}
	return all, rows.Err()
}

// Counted is how many transactions a budget over this category and currency
// takes in, over all time. It is what stands between a counted budget and a
// currency change, so it has to see exactly what the spend subquery sees —
// including the income rows, which are as much the budget's business as the
// outcome ones.
func (s *Store) Counted(categoryID int64, currency string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*)
		FROM transactions t
		LEFT JOIN accounts     a ON a.id = t.account_id
		LEFT JOIN credit_cards k ON k.id = t.card_id
		WHERE t.category_id = ? AND COALESCE(a.currency, k.currency) = ?`,
		categoryID, currency).Scan(&n)
	return n, err
}

// History is what this budget's category has cost month by month, oldest first,
// over the months ending at the budget's own cycle. A cap missed every month is
// a cap, not a spending problem, and that is only visible next to the months
// before it.
//
// Quiet months come back as zero rather than as gaps: a month nothing was spent
// in is worth seeing, and a list with holes in it is not a history.
func (s *Store) History(b Budget, months int) ([]CycleSpend, error) {
	if months < 1 {
		months = 1
	}
	end, err := time.Parse(CycleLayout, b.Cycle)
	if err != nil {
		return nil, fmt.Errorf("a cycle is a month, written YYYY-MM, not %q", b.Cycle)
	}
	start := end.AddDate(0, -(months - 1), 0)

	from_, _, err := CycleRange(start.Format(CycleLayout))
	if err != nil {
		return nil, err
	}
	_, to, err := CycleRange(b.Cycle)
	if err != nil {
		return nil, err
	}

	// substr rather than strftime: the dates are text in this shape everywhere,
	// and the range above is what narrows the scan before the grouping runs.
	//
	// Grouped by the expression rather than by an alias: transactions has a
	// column literally called cycle — the month a recurring bill's payment is
	// for — so GROUP BY cycle binds to that instead, and every month collapses
	// into one NULL group.
	rows, err := s.db.Query(`SELECT substr(t.date, 1, 7) AS month,
		       SUM(CASE WHEN t.kind = 'outcome' THEN t.value ELSE -t.value END)
		FROM transactions t
		LEFT JOIN accounts     a ON a.id = t.account_id
		LEFT JOIN credit_cards k ON k.id = t.card_id
		WHERE t.category_id = ? AND COALESCE(a.currency, k.currency) = ?
		  AND t.date >= ? AND t.date <= ?
		GROUP BY substr(t.date, 1, 7)`, b.Category.ID, b.Currency, from_, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	found := map[string]int64{}
	for rows.Next() {
		var cycle string
		var spent int64
		if err := rows.Scan(&cycle, &spent); err != nil {
			return nil, err
		}
		found[cycle] = spent
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]CycleSpend, 0, months)
	for d := start; !d.After(end); d = d.AddDate(0, 1, 0) {
		cycle := d.Format(CycleLayout)
		out = append(out, CycleSpend{Cycle: cycle, Spent: found[cycle]})
	}
	return out, nil
}

// CountForCategory is how many budgets cap this category, archived ones
// included. Losing the category loses every one of them by the cascade in the
// schema, so this is what lets the confirmation say so before it happens rather
// than after.
func (s *Store) CountForCategory(categoryID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM budgets WHERE category_id = ?`, categoryID).Scan(&n)
	return n, err
}

func (s *Store) CodeTaken(code string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM budgets WHERE code = ?`,
		core.NormalizeCode(code)).Scan(&n)
	return n > 0, err
}

// SuggestCode returns a free code to pre-fill the form with.
func (s *Store) SuggestCode() (string, error) { return core.SuggestCode(s.CodeTaken) }

// uniqueErr turns whichever UNIQUE was hit into a sentence. There are two on
// this table and they mean very different things, so the column name in
// SQLite's message is what tells them apart — core.CodeErr alone would report a
// duplicated category as a duplicated code.
func (s *Store) uniqueErr(err error, b Budget) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE") &&
		strings.Contains(err.Error(), "category_id") {
		return fmt.Errorf("%s is already capped in %s — edit that budget instead of adding a second",
			b.Category.Code, b.Currency)
	}
	return core.CodeErr(err, b.Code)
}
