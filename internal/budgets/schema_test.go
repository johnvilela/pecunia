package budgets

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"kakei/internal/db"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("KAKEI_DB", filepath.Join(t.TempDir(), "kakei.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// category puts one category in the database for a budget to cap.
func category(t *testing.T, conn *sql.DB, code string) int64 {
	t.Helper()
	res, err := conn.Exec(
		`INSERT INTO categories (code, name, color) VALUES (?, ?, 'green')`, code, code)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// insertBudget writes a row straight past the store, which is what lets these
// cases aim at the constraints rather than at Validate.
func insertBudget(conn *sql.DB, code string, amount int64, currency string, categoryID int64) error {
	_, err := conn.Exec(
		`INSERT INTO budgets (code, name, color, amount, currency, category_id)
		 VALUES (?, 'Food', 'green', ?, ?, ?)`, code, amount, currency, categoryID)
	return err
}

func TestSchemaConstraints(t *testing.T) {
	t.Run("an amount of zero is refused by the table itself", func(t *testing.T) {
		conn := newTestDB(t)
		cat := category(t, conn, "FOODC")
		if err := insertBudget(conn, "FOOD1", 0, "BRL", cat); err == nil {
			t.Fatal("a budget of zero was written; want the CHECK to refuse it")
		}
	})

	t.Run("a negative amount is refused by the table itself", func(t *testing.T) {
		conn := newTestDB(t)
		cat := category(t, conn, "FOODC")
		if err := insertBudget(conn, "FOOD1", -1, "BRL", cat); err == nil {
			t.Fatal("a negative budget was written; want the CHECK to refuse it")
		}
	})

	t.Run("a code that is not five characters is refused", func(t *testing.T) {
		conn := newTestDB(t)
		cat := category(t, conn, "FOODC")
		if err := insertBudget(conn, "FOO", 80000, "BRL", cat); err == nil {
			t.Fatal("a three-character code was written; want the CHECK to refuse it")
		}
	})

	t.Run("two budgets over one category in one currency are refused", func(t *testing.T) {
		conn := newTestDB(t)
		cat := category(t, conn, "FOODC")
		if err := insertBudget(conn, "FOOD1", 80000, "BRL", cat); err != nil {
			t.Fatal(err)
		}
		err := insertBudget(conn, "FOOD2", 50000, "BRL", cat)
		if err == nil {
			t.Fatal("a second budget over the same category was written; want the UNIQUE to refuse it")
		}
		if !strings.Contains(err.Error(), "UNIQUE") {
			t.Fatalf("err = %v; want the UNIQUE constraint", err)
		}
	})

	t.Run("one category may be capped in two currencies", func(t *testing.T) {
		conn := newTestDB(t)
		cat := category(t, conn, "FOODC")
		if err := insertBudget(conn, "FOOD1", 80000, "BRL", cat); err != nil {
			t.Fatal(err)
		}
		// Two disjoint sets of transactions, so two real budgets — nothing is
		// counted twice.
		if err := insertBudget(conn, "FOOD2", 500000, "BTC", cat); err != nil {
			t.Fatalf("a second currency over the same category was refused: %v", err)
		}
	})

	t.Run("a budget with no category is refused", func(t *testing.T) {
		conn := newTestDB(t)
		if _, err := conn.Exec(
			`INSERT INTO budgets (code, name, color, amount, currency) VALUES ('FOOD1', 'Food', 'green', 80000, 'BRL')`,
		); err == nil {
			t.Fatal("a budget with no category was written; want NOT NULL to refuse it")
		}
	})

	t.Run("losing the category takes the budget with it", func(t *testing.T) {
		conn := newTestDB(t)
		cat := category(t, conn, "FOODC")
		if err := insertBudget(conn, "FOOD1", 80000, "BRL", cat); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`DELETE FROM categories WHERE id = ?`, cat); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := conn.QueryRow(`SELECT count(*) FROM budgets`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%d budget(s) left after the category went; want the cascade to take it", n)
		}
	})

	t.Run("losing the budget takes its amount log with it", func(t *testing.T) {
		conn := newTestDB(t)
		cat := category(t, conn, "FOODC")
		if err := insertBudget(conn, "FOOD1", 80000, "BRL", cat); err != nil {
			t.Fatal(err)
		}
		var id int64
		if err := conn.QueryRow(`SELECT id FROM budgets`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(
			`INSERT INTO budget_amount_log (budget_id, previous, amount) VALUES (?, 80000, 95000)`, id,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`DELETE FROM budgets WHERE id = ?`, id); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := conn.QueryRow(`SELECT count(*) FROM budget_amount_log`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%d log entries left after the budget went; want the cascade to take them", n)
		}
	})

	t.Run("a logged amount of zero is refused", func(t *testing.T) {
		conn := newTestDB(t)
		cat := category(t, conn, "FOODC")
		if err := insertBudget(conn, "FOOD1", 80000, "BRL", cat); err != nil {
			t.Fatal(err)
		}
		var id int64
		if err := conn.QueryRow(`SELECT id FROM budgets`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(
			`INSERT INTO budget_amount_log (budget_id, previous, amount) VALUES (?, 80000, 0)`, id,
		); err == nil {
			t.Fatal("a log entry of zero was written; want the CHECK to refuse it")
		}
	})
}
