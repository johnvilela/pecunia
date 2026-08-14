package transactions

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"kakei/internal/bills"
	"kakei/internal/cards"
)

// Scope is how far an edit or a delete reaches through an installment series.
// The zero value is one row, which is what every transaction that is not part of
// a series gets.
type Scope int

const (
	ScopeOne     Scope = iota // this row only
	ScopeForward              // this row and the installments after it
	ScopeAll                  // every installment in the series
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Filter is every axis the list command can narrow by. A zero value is "all",
// and each field set narrows further.
type Filter struct {
	From, To    string // YYYY-MM-DD, both inclusive
	Tag         string
	Search      string // substring of the title or the description
	CategoryID  int64
	AccountID   int64
	CardID      int64
	GoalID      int64
	RecurringID int64
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
	COALESCE((SELECT group_concat(tag) FROM transaction_tags WHERE transaction_id = t.id), ''),
	COALESCE(t.installment_group, 0), t.installment_seq, t.installment_count, COALESCE(t.pays_bill_id, 0),
	COALESCE(g.id, 0), COALESCE(g.name, ''), COALESCE(g.currency, ''),
	COALESCE(r.id, 0), COALESCE(r.code, ''), COALESCE(r.name, ''), COALESCE(r.color, ''),
	COALESCE(t.cycle, '')`

const from = `
	FROM transactions t
	LEFT JOIN categories   c ON c.id = t.category_id
	LEFT JOIN accounts     a ON a.id = t.account_id
	LEFT JOIN credit_cards k ON k.id = t.card_id
	LEFT JOIN goals        g ON g.id = t.goal_id
	LEFT JOIN recurring_bills r ON r.id = t.recurring_id`

func scan(row interface{ Scan(...any) error }) (Transaction, error) {
	var t Transaction
	var accountCur, cardCur, tags string
	err := row.Scan(&t.ID, &t.Title, &t.Description, &t.Value, &t.Kind, &t.Date, &t.CreatedAt, &t.UpdatedAt,
		&t.Category.ID, &t.Category.Code, &t.Category.Name, &t.Category.Color,
		&t.Account.ID, &t.Account.Code, &t.Account.Name, &t.Account.Color, &accountCur,
		&t.Card.ID, &t.Card.Code, &t.Card.Name, &t.Card.Color, &cardCur,
		&tags,
		&t.Installment.Group, &t.Installment.Seq, &t.Installment.Count, &t.PaysBillID,
		&t.Goal.ID, &t.Goal.Name, &t.GoalCurrency,
		&t.Recurring.ID, &t.Recurring.Code, &t.Recurring.Name, &t.Recurring.Color, &t.Cycle)
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
	if f.GoalID != 0 {
		add(`t.goal_id = ?`, f.GoalID)
	}
	if f.RecurringID != 0 {
		add(`t.recurring_id = ?`, f.RecurringID)
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
//
// An installment purchase is Installment.Count rows rather than one, dated a
// month apart and sharing a group, with Value split between them. They are
// written inside the same transaction, so the limit refuses the series whole or
// not at all — a purchase that only half fits is not a purchase.
func (s *Store) Create(t *Transaction) error {
	t.Tags = NormalizeTags(t.Tags)
	if err := s.fillGoalCurrency(t); err != nil {
		return err
	}
	if err := t.Validate(); err != nil {
		return err
	}
	n := int(t.Installment.Count)
	if n < 1 {
		n = 1
	}
	first, err := time.Parse(DateLayout, t.Date)
	if err != nil {
		return err
	}

	return s.inTx(func(tx *sql.Tx) error {
		var group int64
		for i, value := range SplitInstallments(t.Value, n) {
			row := *t
			row.Value = value
			row.Date = cards.AddMonths(first, i).Format(DateLayout)
			row.Installment = Installment{Group: group, Seq: int64(i + 1), Count: int64(n)}
			if n == 1 {
				// A single charge is not a series, and saying so with a 1/1 would
				// only put "(1/1)" on every ordinary row.
				row.Installment = Installment{}
			}

			id, err := insertRow(tx, row)
			if err != nil {
				return err
			}
			if i == 0 {
				t.ID = id
				// The group is the first row's id, which does not exist until the
				// row does.
				if n > 1 {
					group = id
					if _, err := tx.Exec(
						`UPDATE transactions SET installment_group = ? WHERE id = ?`, id, id); err != nil {
						return err
					}
				}
			}
			if err := writeTags(tx, id, row.Tags); err != nil {
				return err
			}
			if err := applyBalance(tx, row, 1); err != nil {
				return err
			}
		}
		return refreshBills(tx, t.PaysBillID)
	})
}

// PayBill records a bill payment: an ordinary outcome on whichever account paid,
// which happens to name the bill. There is no matching row on the card — the
// card's balance moves because the payment names the bill, which is what keeps a
// payment from ever showing up as spending on the next one.
func (s *Store) PayBill(billID, accountID, value int64, date string) error {
	t := Transaction{
		Title:      "Bill payment",
		Value:      value,
		Kind:       KindOutcome,
		Date:       date,
		Account:    Ref{ID: accountID},
		PaysBillID: billID,
	}
	return s.Create(&t)
}

// Update reverses what the stored row did to its target before applying what the
// new one does, so an edit that changes the value, flips the kind or moves the
// transaction to another account or card leaves every balance right.
//
// scope says how far it reaches through an installment series. The rows beside
// the edited one take its title, description, category, goal, tags and kind, and
// keep their own date and amount: each installment falls on its own bill, and
// re-splitting a live series is a different operation.
func (s *Store) Update(t Transaction, scope Scope) error {
	t.Tags = NormalizeTags(t.Tags)
	if err := s.fillGoalCurrency(&t); err != nil {
		return err
	}
	if err := t.Validate(); err != nil {
		return err
	}
	return s.inTx(func(tx *sql.Tx) error {
		targets, err := scoped(tx, t.ID, scope)
		if err != nil {
			return err
		}
		var touched []int64
		for _, old := range targets {
			row := t
			if old.ID != t.ID {
				// A sibling keeps everything that is its own.
				row.ID, row.Value, row.Date = old.ID, old.Value, old.Date
				row.Installment, row.PaysBillID = old.Installment, old.PaysBillID
			}
			if err := applyBalance(tx, old, -1); err != nil {
				return err
			}
			if err := updateRow(tx, row); err != nil {
				return err
			}
			if err := applyBalance(tx, row, 1); err != nil {
				return err
			}
			touched = append(touched, old.PaysBillID, row.PaysBillID)
		}
		return refreshBills(tx, touched...)
	})
}

// Delete takes the row away and gives its target the money back. The tags go
// with it, by the cascade in the schema. scope says how far it reaches through
// an installment series.
func (s *Store) Delete(id int64, scope Scope) error {
	return s.inTx(func(tx *sql.Tx) error {
		targets, err := scoped(tx, id, scope)
		if err != nil {
			return err
		}
		var touched []int64
		for _, old := range targets {
			if _, err := tx.Exec(`DELETE FROM transactions WHERE id = ?`, old.ID); err != nil {
				return err
			}
			if err := applyBalance(tx, old, -1); err != nil {
				return err
			}
			touched = append(touched, old.PaysBillID)
		}
		return refreshBills(tx, touched...)
	})
}

// Series is the rows of one installment purchase, in the order they fall due.
func (s *Store) Series(groupID int64) ([]Transaction, error) {
	rows, err := s.db.Query(`SELECT `+columns+from+`
		WHERE t.installment_group = ?
		ORDER BY t.installment_seq`, groupID)
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

// scoped is the rows one write applies to. Anything that is not part of a
// series is itself and nothing else, whatever scope was asked for — so a caller
// that does not care about series never has to think about one.
func scoped(tx *sql.Tx, id int64, scope Scope) ([]Transaction, error) {
	one, err := scan(tx.QueryRow(`SELECT `+columns+from+`
		WHERE t.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if scope == ScopeOne || !one.IsInstallment() {
		return []Transaction{one}, nil
	}

	where := `t.installment_group = ?`
	args := []any{one.Installment.Group}
	if scope == ScopeForward {
		where += ` AND t.installment_seq >= ?`
		args = append(args, one.Installment.Seq)
	}
	rows, err := tx.Query(`SELECT `+columns+from+`
		WHERE `+where+`
		ORDER BY t.installment_seq`, args...)
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

// fillGoalCurrency reads back the two currencies a goal link has to agree on:
// the goal's own, and the one the transaction inherits from whatever it is
// filed against. Both are facts of other tables, so a caller that only knew the
// ids still gets a mismatch refused rather than filing satoshis against a goal
// counted in centavos.
//
// It costs nothing on the transactions that name no goal, which is nearly all
// of them.
func (s *Store) fillGoalCurrency(t *Transaction) error {
	if t.Goal.ID == 0 {
		t.GoalCurrency = ""
		return nil
	}
	if err := s.db.QueryRow(`SELECT currency FROM goals WHERE id = ?`, t.Goal.ID).
		Scan(&t.GoalCurrency); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("no goal %d to link this to", t.Goal.ID)
		}
		return err
	}
	query, id := `SELECT currency FROM accounts WHERE id = ?`, t.Account.ID
	if t.IsCard() {
		query, id = `SELECT currency FROM credit_cards WHERE id = ?`, t.Card.ID
	}
	return s.db.QueryRow(query, id).Scan(&t.Currency)
}

func insertRow(tx *sql.Tx, t Transaction) (int64, error) {
	res, err := tx.Exec(
		`INSERT INTO transactions (title, description, category_id, account_id, card_id, value, kind, date,
		   installment_group, installment_seq, installment_count, pays_bill_id, goal_id,
		   recurring_id, cycle)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Title, t.Description, nullID(t.Category.ID), nullID(t.Account.ID), nullID(t.Card.ID),
		t.Value, t.Kind, t.Date,
		nullID(t.Installment.Group), t.Installment.Seq, t.Installment.Count, nullID(t.PaysBillID),
		nullID(t.Goal.ID), nullID(t.Recurring.ID), nullText(t.Cycle))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func updateRow(tx *sql.Tx, t Transaction) error {
	if _, err := tx.Exec(
		`UPDATE transactions SET title = ?, description = ?, category_id = ?, account_id = ?,
		 card_id = ?, value = ?, kind = ?, date = ?, pays_bill_id = ?, goal_id = ?,
		 recurring_id = ?, cycle = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		t.Title, t.Description, nullID(t.Category.ID), nullID(t.Account.ID), nullID(t.Card.ID),
		t.Value, t.Kind, t.Date, nullID(t.PaysBillID), nullID(t.Goal.ID),
		nullID(t.Recurring.ID), nullText(t.Cycle), t.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM transaction_tags WHERE transaction_id = ?`, t.ID); err != nil {
		return err
	}
	return writeTags(tx, t.ID, t.Tags)
}

// refreshBills rewrites the status of every bill the write touched. It runs last
// because the status is read back out of the transactions table, so it has to
// see the rows as they finally are — not halfway through an edit that has
// reversed the old row but not yet written the new one.
func refreshBills(tx *sql.Tx, ids ...int64) error {
	seen := map[int64]bool{}
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		if err := bills.Refresh(tx, id); err != nil {
			return err
		}
	}
	return nil
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

// nullText is nullID for a column whose empty value is a string. The cycle has
// a CHECK on its shape, and "" is not a month.
func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
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
	if t.IsCard() {
		if _, err := tx.Exec(
			`UPDATE credit_cards SET balance = balance + ?, updated_at = datetime('now') WHERE id = ?`,
			sign*t.CardDelta(), t.Card.ID); err != nil {
			return err
		}
		return checkLimit(tx, t.Card.ID)
	}
	if _, err := tx.Exec(
		`UPDATE accounts SET balance = balance + ?, updated_at = datetime('now') WHERE id = ?`,
		sign*t.Signed(), t.Account.ID); err != nil {
		return err
	}
	if t.PaysBillID == 0 {
		return nil
	}
	return applyPayment(tx, t, sign)
}

// applyPayment lowers what the paid bill's card owes. This is the one place a
// transaction moves a balance it does not name: a payment lives on the account
// it came out of, and the card it settles is reached through the bill.
func applyPayment(tx *sql.Tx, t Transaction, sign int64) error {
	var cardID int64
	err := tx.QueryRow(`SELECT card_id FROM card_bills WHERE id = ?`, t.PaysBillID).Scan(&cardID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("no bill %d to pay", t.PaysBillID)
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE credit_cards SET balance = balance - ?, updated_at = datetime('now') WHERE id = ?`,
		sign*t.Value, cardID); err != nil {
		return err
	}
	// Paying can only lower the debt, but taking a payment back raises it — and
	// that can push a card past a limit it may not pass.
	return checkLimit(tx, cardID)
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
