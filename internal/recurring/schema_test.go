package recurring

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"kakei/internal/db"
)

// newTestDB gives the caller its own SQLite file in its own temp dir, so no two
// cases ever share state. Call it inside the subtest, not the parent.
//
// A real file rather than :memory: — the CHECK constraints and the foreign keys
// are most of what the schema is worth, and only the migration path builds them.
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

// seedAccount is the account every bill in these tests is paid from.
func seedAccount(t *testing.T, conn *sql.DB) int64 {
	t.Helper()
	var id int64
	if err := conn.QueryRow(
		`INSERT INTO accounts (code, name, color, balance, currency)
		 VALUES ('INTER', 'Banco Inter', 'orange', 1000000, 'BRL') RETURNING id`).
		Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedCard(t *testing.T, conn *sql.DB) int64 {
	t.Helper()
	var id int64
	if err := conn.QueryRow(
		`INSERT INTO credit_cards (code, name, color, credit_limit, balance, currency, closing_day, due_day)
		 VALUES ('NUCRD', 'Nubank', 'violet', 500000, 0, 'BRL', 20, 27) RETURNING id`).
		Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// insertBill writes one row straight through the schema, so what comes back is
// the constraint's own verdict and not a Go guard standing in front of it.
func insertBill(conn *sql.DB, code string, openDay, dueDay int, account, card any) error {
	_, err := conn.Exec(
		`INSERT INTO recurring_bills (code, name, expected, account_id, card_id, open_day, due_day)
		 VALUES (?, 'Energy', 21490, ?, ?, ?, ?)`,
		code, account, card, openDay, dueDay)
	return err
}

func TestSchema(t *testing.T) {
	t.Run("accepts a bill paid from an account", func(t *testing.T) {
		conn := newTestDB(t)
		if err := insertBill(conn, "ENERG", 5, 15, seedAccount(t, conn), nil); err != nil {
			t.Fatalf("insert: %v", err)
		}
	})

	t.Run("accepts a bill paid on a credit card", func(t *testing.T) {
		conn := newTestDB(t)
		if err := insertBill(conn, "NFLIX", 1, 10, nil, seedCard(t, conn)); err != nil {
			t.Fatalf("insert: %v", err)
		}
	})

	t.Run("refuses a bill with both an account and a card", func(t *testing.T) {
		conn := newTestDB(t)
		if err := insertBill(conn, "ENERG", 5, 15, seedAccount(t, conn), seedCard(t, conn)); err == nil {
			t.Fatal("expected the one-source CHECK to refuse two sources")
		}
	})

	t.Run("refuses a bill with neither", func(t *testing.T) {
		conn := newTestDB(t)
		if err := insertBill(conn, "ENERG", 5, 15, nil, nil); err == nil {
			t.Fatal("expected the one-source CHECK to refuse no source at all")
		}
	})

	t.Run("refuses a day outside the month", func(t *testing.T) {
		conn := newTestDB(t)
		account := seedAccount(t, conn)
		if err := insertBill(conn, "ENERG", 0, 15, account, nil); err == nil {
			t.Fatal("expected open_day 0 to be refused")
		}
		if err := insertBill(conn, "WATER", 5, 32, account, nil); err == nil {
			t.Fatal("expected due_day 32 to be refused")
		}
	})

	t.Run("refuses a negative expected amount", func(t *testing.T) {
		conn := newTestDB(t)
		if _, err := conn.Exec(
			`INSERT INTO recurring_bills (code, name, expected, account_id, open_day, due_day)
			 VALUES ('ENERG', 'Energy', -1, ?, 5, 15)`, seedAccount(t, conn)); err == nil {
			t.Fatal("expected a negative expected amount to be refused")
		}
	})

	t.Run("allows a zero expected amount", func(t *testing.T) {
		conn := newTestDB(t)
		if _, err := conn.Exec(
			`INSERT INTO recurring_bills (code, name, expected, account_id, open_day, due_day)
			 VALUES ('ENERG', 'Energy', 0, ?, 5, 15)`, seedAccount(t, conn)); err != nil {
			t.Fatalf("a bill whose amount is unknown must be allowed: %v", err)
		}
	})

	t.Run("refuses a duplicate code", func(t *testing.T) {
		conn := newTestDB(t)
		account := seedAccount(t, conn)
		if err := insertBill(conn, "ENERG", 5, 15, account, nil); err != nil {
			t.Fatal(err)
		}
		err := insertBill(conn, "ENERG", 1, 10, account, nil)
		if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
			t.Fatalf("expected a UNIQUE violation, got %v", err)
		}
	})

	t.Run("a bill starts active", func(t *testing.T) {
		conn := newTestDB(t)
		if err := insertBill(conn, "ENERG", 5, 15, seedAccount(t, conn), nil); err != nil {
			t.Fatal(err)
		}
		var active bool
		if err := conn.QueryRow(`SELECT active FROM recurring_bills WHERE code = 'ENERG'`).
			Scan(&active); err != nil {
			t.Fatal(err)
		}
		if !active {
			t.Fatal("a new bill must be active")
		}
	})

	t.Run("deleting a bill unlinks its payments instead of deleting them", func(t *testing.T) {
		conn := newTestDB(t)
		account := seedAccount(t, conn)
		if err := insertBill(conn, "ENERG", 5, 15, account, nil); err != nil {
			t.Fatal(err)
		}
		var bill int64
		if err := conn.QueryRow(`SELECT id FROM recurring_bills WHERE code = 'ENERG'`).
			Scan(&bill); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(
			`INSERT INTO transactions (title, account_id, value, kind, date, recurring_id, cycle)
			 VALUES ('Energy', ?, 21490, 'outcome', '2026-08-08', ?, '2026-08')`,
			account, bill); err != nil {
			t.Fatalf("a transaction must be able to name the bill it pays: %v", err)
		}
		if _, err := conn.Exec(`DELETE FROM recurring_bills WHERE id = ?`, bill); err != nil {
			t.Fatalf("nothing may block deleting a bill: %v", err)
		}

		var n int
		if err := conn.QueryRow(`SELECT count(*) FROM transactions WHERE recurring_id IS NULL`).
			Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("the payment must survive the bill, unlinked; found %d unlinked", n)
		}
	})

	t.Run("refuses a cycle that is not a month", func(t *testing.T) {
		conn := newTestDB(t)
		account := seedAccount(t, conn)
		if _, err := conn.Exec(
			`INSERT INTO transactions (title, account_id, value, kind, date, cycle)
			 VALUES ('Energy', ?, 21490, 'outcome', '2026-08-08', '2026-08-08')`,
			account); err == nil {
			t.Fatal("expected the cycle CHECK to refuse a full date")
		}
	})
}
