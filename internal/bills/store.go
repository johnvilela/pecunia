package bills

import (
	"database/sql"
	"errors"
	"time"

	"pecunia/internal/cards"
	"pecunia/internal/logs"
)

// DB is the slice of *sql.DB and *sql.Tx this package needs. Refresh takes one
// so the transactions store can run it inside its own transaction: a payment and
// the status it produces have to land together or not at all.
type DB interface {
	Exec(string, ...any) (sql.Result, error)
	QueryRow(string, ...any) *sql.Row
}

type Store struct {
	db *sql.DB
	// now is the clock. It is a field because every read here decides what is
	// still open by comparing against today, and a test that cannot pin that is
	// a different test each month.
	now func() time.Time
}

func NewStore(db *sql.DB) *Store { return NewStoreAt(db, time.Now) }

// NewStoreAt is NewStore with the clock named. Every read here decides what has
// closed by comparing against today, so a caller that has already pinned its
// own today — a summary asks every module the same date — has to be able to
// hand it over.
func NewStoreAt(db *sql.DB, now func() time.Time) *Store { return &Store{db: db, now: now} }

const columns = `id, closes_on, due_on, total, status`

func scan(row interface{ Scan(...any) error }, c cards.Card) (Bill, error) {
	b := Bill{Card: c}
	err := row.Scan(&b.ID, &b.ClosesOn, &b.DueOn, &b.Total, &b.Status)
	return b, err
}

// Ensure fills in every bill the card should have by now and brings the open
// ones up to date. Every read path starts here, so a bill can never be missing
// when something goes looking for it, and there is no close command to forget.
//
// It is the single point where a bill crosses from open to closed — and so the
// single point where its total stops moving.
func (s *Store) Ensure(c cards.Card) error {
	today := s.now()
	last := cards.NextDate(today, c.ClosingDay) // the cycle still taking charges

	// The current cycle's row standing means every earlier one stands too —
	// this loop is the only thing that creates bills, and it fills forward —
	// so the whole generation pass is one SELECT on every read after the
	// first of a cycle.
	var have int64
	err := s.db.QueryRow(
		`SELECT id FROM card_bills WHERE card_id = ? AND closes_on = ?`,
		c.ID, last.Format(dateLayout)).Scan(&have)
	if err == nil {
		return s.refreshOpen(c, today)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	from, err := s.start(c)
	if err != nil {
		return err
	}
	for closes := cards.NextDate(from, c.ClosingDay); !closes.After(last); closes = next(closes, c.ClosingDay) {
		res, err := s.db.Exec(
			`INSERT INTO card_bills (card_id, closes_on, due_on) VALUES (?, ?, ?)
			 ON CONFLICT (card_id, closes_on) DO NOTHING`,
			c.ID, closes.Format(dateLayout), DueDate(closes, c.DueDay).Format(dateLayout))
		if err != nil {
			return err
		}
		// Only a bill that actually appeared is an event — the conflict path is
		// this running again, which is every read after the first.
		if n, _ := res.RowsAffected(); n == 1 {
			id, err := res.LastInsertId()
			if err != nil {
				return err
			}
			if err := logs.Record(s.db, logs.System, "created", "card_bill", id); err != nil {
				return err
			}
		}
	}
	return s.refreshOpen(c, today)
}

// start is the first day the card could have a bill for: its earliest
// transaction, or the day the card itself was created when it has none.
func (s *Store) start(c cards.Card) (time.Time, error) {
	var first sql.NullString
	if err := s.db.QueryRow(
		`SELECT min(date) FROM transactions WHERE card_id = ?`, c.ID).Scan(&first); err != nil {
		return time.Time{}, err
	}
	if first.Valid {
		if d, err := time.Parse(dateLayout, first.String); err == nil {
			return d, nil
		}
	}
	// created_at is a datetime; only the date half is a bill's business.
	if len(c.CreatedAt) >= len(dateLayout) {
		if d, err := time.Parse(dateLayout, c.CreatedAt[:len(dateLayout)]); err == nil {
			return d, nil
		}
	}
	return s.now(), nil
}

// next is the closing date after this one. Stepping from the day after avoids
// NextDate handing back the date it was given.
func next(closes time.Time, day int) time.Time {
	return cards.NextDate(closes.AddDate(0, 0, 1), day)
}

// refreshOpen recomputes the total and the status of every bill still marked
// open. A bill whose closing date has passed gets its final total here and
// leaves the open status behind, which is what freezes it.
func (s *Store) refreshOpen(c cards.Card, today time.Time) error {
	rows, err := s.db.Query(
		`SELECT `+columns+` FROM card_bills WHERE card_id = ? AND status = ?`, c.ID, StatusOpen)
	if err != nil {
		return err
	}
	var open []Bill
	for rows.Next() {
		b, err := scan(rows, c)
		if err != nil {
			rows.Close()
			return err
		}
		open = append(open, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, b := range open {
		total, err := s.LiveTotal(b)
		if err != nil {
			return err
		}
		paid, err := paidOn(s.db, b.ID)
		if err != nil {
			return err
		}
		closed := b.ClosesOn < today.Format(dateLayout)
		status := StatusFor(total, paid, closed)
		// Nothing moved, nothing written — this runs on every read, and a read
		// that finds the record already right should leave no mark on it.
		if total == b.Total && status == b.Status {
			continue
		}
		if _, err := s.db.Exec(
			`UPDATE card_bills SET total = ?, status = ?, updated_at = datetime('now') WHERE id = ?`,
			total, status, b.ID); err != nil {
			return err
		}
	}
	return nil
}

// LiveTotal is what the bill's period sums to right now. It is what an open
// bill's total is set from — and, on a closed one, what the detail view compares
// the frozen total against so the drift is visible rather than silent.
func (s *Store) LiveTotal(b Bill) (int64, error) { return liveTotal(s.db, b) }

func liveTotal(db DB, b Bill) (int64, error) {
	from, to := b.Period()
	var total int64
	err := db.QueryRow(
		`SELECT COALESCE(SUM(CASE kind WHEN 'outcome' THEN value ELSE -value END), 0)
		 FROM transactions WHERE card_id = ? AND date >= ? AND date <= ?`,
		b.Card.ID, from, to).Scan(&total)
	return total, err
}

// Charged brings the open bill covering one card charge up to date, inside the
// caller's transaction — the charges' counterpart to Refresh, which follows
// payments. A bill that does not exist yet is not created: an installment can
// land five years out, and a future row would break everything that treats the
// newest open bill as the current one. The row appears when Ensure reaches its
// cycle, and its total is computed then. A closed bill is left alone — its
// total froze when it closed.
func Charged(db DB, cardID int64, date string) error {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return err
	}
	var closingDay int
	if err := db.QueryRow(
		`SELECT closing_day FROM credit_cards WHERE id = ?`, cardID).Scan(&closingDay); err != nil {
		return err
	}
	closes := cards.NextDate(d, closingDay).Format(dateLayout)

	var id, total int64
	var status string
	err = db.QueryRow(
		`SELECT id, total, status FROM card_bills WHERE card_id = ? AND closes_on = ?`,
		cardID, closes).Scan(&id, &total, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if status != StatusOpen {
		return nil
	}

	live, err := liveTotal(db, Bill{ClosesOn: closes, Card: cards.Card{ID: cardID, ClosingDay: closingDay}})
	if err != nil {
		return err
	}
	if live == total {
		return nil
	}
	_, err = db.Exec(
		`UPDATE card_bills SET total = ?, updated_at = datetime('now') WHERE id = ?`, live, id)
	return err
}

// paidOn is what has been paid against the bill. A payment is an account
// transaction naming the bill, so nothing here has to exclude anything: card
// transactions are charges and account transactions are payments.
func paidOn(db DB, billID int64) (int64, error) {
	var paid int64
	err := db.QueryRow(
		`SELECT COALESCE(SUM(value), 0) FROM transactions WHERE pays_bill_id = ?`, billID).Scan(&paid)
	return paid, err
}

// Refresh rewrites one bill's status from the payments now pointing at it. The
// transactions store calls it inside its own transaction whenever a payment is
// written, changed or taken away, so partial → paid → closed is never a separate
// step that can be skipped.
//
// The total is left alone: it belongs to the cycle, and paying does not change
// what was spent. Neither does the calendar come into it — whether the cycle has
// closed is already recorded in the status Ensure last wrote, so a payment can
// never accidentally re-open or close a bill.
func Refresh(db DB, billID int64) error {
	var total int64
	var status string
	if err := db.QueryRow(
		`SELECT total, status FROM card_bills WHERE id = ?`, billID).Scan(&total, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	paid, err := paidOn(db, billID)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE card_bills SET status = ?, updated_at = datetime('now') WHERE id = ?`,
		StatusFor(total, paid, status != StatusOpen), billID)
	return err
}

// List is every bill the card has, newest first, each carrying what has been
// paid against it.
func (s *Store) List(c cards.Card) ([]Bill, error) {
	if err := s.Ensure(c); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT `+columns+` FROM card_bills WHERE card_id = ? ORDER BY closes_on DESC`, c.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Bill
	for rows.Next() {
		b, err := scan(rows, c)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Paid, err = paidOn(s.db, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Get is one cycle by the date it closed.
func (s *Store) Get(c cards.Card, closesOn string) (Bill, error) {
	all, err := s.List(c)
	if err != nil {
		return Bill{}, err
	}
	for _, b := range all {
		if b.ClosesOn == closesOn {
			return b, nil
		}
	}
	return Bill{}, ErrNotFound
}

// Open is the bill still taking charges — the one closing next.
func (s *Store) Open(c cards.Card) (Bill, error) {
	all, err := s.List(c)
	if err != nil {
		return Bill{}, err
	}
	for _, b := range all {
		if b.Status == StatusOpen {
			return b, nil
		}
	}
	return Bill{}, ErrNotFound
}

// OldestUnpaid is what `pecunia cc pay` offers first: the bill that has been owed
// the longest. Nothing owing is ErrNotFound, not an empty bill.
func (s *Store) OldestUnpaid(c cards.Card) (Bill, error) {
	owing, err := s.Unpaid(c)
	if err != nil {
		return Bill{}, err
	}
	if len(owing) == 0 {
		return Bill{}, ErrNotFound
	}
	return owing[0], nil
}

// Unpaid is every bill that has closed and still owes something, oldest first —
// what `pecunia cc pay` offers to choose from. The open bill is left out: it is
// still taking charges, and settling a total that has not stopped moving is not
// paying a bill.
func (s *Store) Unpaid(c cards.Card) ([]Bill, error) {
	all, err := s.List(c)
	if err != nil {
		return nil, err
	}
	var out []Bill
	// List is newest first, so walking back gives oldest first.
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Owed() > 0 {
			out = append(out, all[i])
		}
	}
	return out, nil
}

// Charges is what the bill is made of, oldest first — a bill reads like a
// statement, not like the transaction list.
func (s *Store) Charges(b Bill) ([]Charge, error) {
	from, to := b.Period()
	rows, err := s.db.Query(
		`SELECT t.id, t.date, t.title, t.value, t.kind,
		        t.installment_seq, t.installment_count, COALESCE(c.code, '')
		 FROM transactions t
		 LEFT JOIN categories c ON c.id = t.category_id
		 WHERE t.card_id = ? AND t.date >= ? AND t.date <= ?
		 ORDER BY t.date, t.id`, b.Card.ID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Charge
	for rows.Next() {
		var ch Charge
		if err := rows.Scan(&ch.ID, &ch.Date, &ch.Title, &ch.Value, &ch.Kind,
			&ch.Seq, &ch.Count, &ch.Category); err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}
