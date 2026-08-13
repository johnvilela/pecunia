package db

import (
	"database/sql"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// setDevDB swaps the ldflags-injected dev default for one case only.
func setDevDB(t *testing.T, path string) {
	t.Helper()
	old := DevDB
	DevDB = path
	t.Cleanup(func() { DevDB = old })
}

func TestPath(t *testing.T) {
	t.Run("a dev build ignores KAKEI_DB", func(t *testing.T) {
		// The whole point: a dev binary can never be pointed at the real
		// database, however the environment is set.
		setDevDB(t, "/tmp/dev.db")
		t.Setenv("KAKEI_DB", "/home/someone/.config/kakei/kakei.db")
		if got, err := Path(); err != nil || got != "/tmp/dev.db" {
			t.Fatalf("dev build with KAKEI_DB set: got %q, %v; want the dev database", got, err)
		}
	})

	t.Run("a dev build with no KAKEI_DB uses the dev database", func(t *testing.T) {
		setDevDB(t, "/tmp/dev.db")
		t.Setenv("KAKEI_DB", "")
		if got, err := Path(); err != nil || got != "/tmp/dev.db" {
			t.Fatalf("with only DevDB set: got %q, %v", got, err)
		}
	})

	t.Run("a release build honours KAKEI_DB", func(t *testing.T) {
		setDevDB(t, "")
		t.Setenv("KAKEI_DB", "/tmp/explicit.db")
		if got, err := Path(); err != nil || got != "/tmp/explicit.db" {
			t.Fatalf("with KAKEI_DB set: got %q, %v", got, err)
		}
	})

	t.Run("user config dir is the fallback", func(t *testing.T) {
		setDevDB(t, "")
		t.Setenv("KAKEI_DB", "")
		got, err := Path()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(got, filepath.Join("kakei", "kakei.db")) {
			t.Fatalf("default path %q does not end in kakei/kakei.db", got)
		}
	})

	t.Run("a release build has no dev default", func(t *testing.T) {
		// DevDB is only ever set by scripts/dev.sh; nothing else may set it.
		if DevDB != "" {
			t.Fatalf("DevDB = %q in a plain build; want empty", DevDB)
		}
	})
}

func TestOpenMigratesOnceOnly(t *testing.T) {
	t.Setenv("KAKEI_DB", filepath.Join(t.TempDir(), "kakei.db"))

	for i := range 2 {
		conn, err := Open()
		if err != nil {
			t.Fatalf("open %d: %v", i+1, err)
		}
		var n int
		if err := conn.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&n); err != nil {
			t.Fatal(err)
		}
		// Derived from the embedded files, so adding a migration never means
		// editing this number.
		entries, err := fs.ReadDir(migrations, "migrations")
		if err != nil {
			t.Fatal(err)
		}
		if n != len(entries) {
			t.Fatalf("open %d: expected %d applied migrations, got %d", i+1, len(entries), n)
		}
		for _, table := range []string{"accounts", "credit_cards", "categories", "transactions", "transaction_tags", "card_bills", "goals"} {
			if _, err := conn.Exec("SELECT 1 FROM " + table); err != nil {
				t.Fatalf("open %d: %s table missing: %v", i+1, table, err)
			}
		}
		conn.Close()
	}
}

func TestCardBillsSchema(t *testing.T) {
	open := func(t *testing.T) *sql.DB {
		t.Helper()
		t.Setenv("KAKEI_DB", filepath.Join(t.TempDir(), "kakei.db"))
		conn, err := Open()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		if _, err := conn.Exec(
			`INSERT INTO credit_cards (id, code, name, color, currency, closing_day, due_day)
			 VALUES (1, 'NUCRD', 'Nubank', 'violet', 'BRL', 10, 20)`); err != nil {
			t.Fatal(err)
		}
		return conn
	}
	bill := func(conn *sql.DB, closesOn, status string) error {
		_, err := conn.Exec(
			`INSERT INTO card_bills (card_id, closes_on, due_on, status) VALUES (1, ?, '2026-08-20', ?)`,
			closesOn, status)
		return err
	}

	t.Run("one bill per card per closing date", func(t *testing.T) {
		conn := open(t)
		if err := bill(conn, "2026-08-10", "open"); err != nil {
			t.Fatal(err)
		}
		// What makes generating bills on every read idempotent.
		if err := bill(conn, "2026-08-10", "open"); err == nil {
			t.Fatal("the same closing date was accepted twice")
		}
	})

	t.Run("status is one of the four", func(t *testing.T) {
		conn := open(t)
		for i, s := range []string{"open", "closed", "partial", "paid"} {
			if err := bill(conn, "2026-08-1"+string(rune('0'+i)), s); err != nil {
				t.Errorf("status %q was refused: %v", s, err)
			}
		}
		if err := bill(conn, "2026-09-10", "settled"); err == nil {
			t.Error("an unknown status was accepted")
		}
	})

	t.Run("deleting the card takes its bills with it", func(t *testing.T) {
		conn := open(t)
		if err := bill(conn, "2026-08-10", "open"); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`DELETE FROM credit_cards WHERE id = 1`); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := conn.QueryRow(`SELECT count(*) FROM card_bills`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%d bill(s) outlived the card", n)
		}
	})

	t.Run("transactions carry the bill link and the installment columns", func(t *testing.T) {
		conn := open(t)
		if err := bill(conn, "2026-08-10", "closed"); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(
			`INSERT INTO accounts (id, code, name, color, currency)
			 VALUES (1, 'INTER', 'Inter', 'orange', 'BRL')`); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(
			`INSERT INTO transactions (title, account_id, value, kind, date, pays_bill_id)
			 VALUES ('Bill NUCRD', 1, 89050, 'outcome', '2026-08-20', 1)`); err != nil {
			t.Fatalf("pays_bill_id: %v", err)
		}
		if _, err := conn.Exec(
			`INSERT INTO transactions (title, card_id, value, kind, date,
			   installment_group, installment_seq, installment_count)
			 VALUES ('Phone', 1, 20000, 'outcome', '2026-08-14', 7, 3, 5)`); err != nil {
			t.Fatalf("installment columns: %v", err)
		}

		// Losing a bill unlinks its payments; it does not delete them.
		if _, err := conn.Exec(`DELETE FROM card_bills WHERE id = 1`); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := conn.QueryRow(
			`SELECT count(*) FROM transactions WHERE pays_bill_id IS NOT NULL`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%d payment(s) still point at a deleted bill", n)
		}
		if err := conn.QueryRow(`SELECT count(*) FROM transactions`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("%d transaction(s) left; deleting a bill should not delete them", n)
		}
	})
}
