package db

import (
	"database/sql"
	"io/fs"
	"os"
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
	t.Run("a dev build ignores PECUNIA_DB", func(t *testing.T) {
		// The whole point: a dev binary can never be pointed at the real
		// database, however the environment is set.
		setDevDB(t, "/tmp/dev.db")
		t.Setenv("PECUNIA_DB", "/home/someone/.config/pecunia/pecunia.db")
		if got, err := Path(); err != nil || got != "/tmp/dev.db" {
			t.Fatalf("dev build with PECUNIA_DB set: got %q, %v; want the dev database", got, err)
		}
	})

	t.Run("a dev build with no PECUNIA_DB uses the dev database", func(t *testing.T) {
		setDevDB(t, "/tmp/dev.db")
		t.Setenv("PECUNIA_DB", "")
		if got, err := Path(); err != nil || got != "/tmp/dev.db" {
			t.Fatalf("with only DevDB set: got %q, %v", got, err)
		}
	})

	t.Run("a release build honours PECUNIA_DB", func(t *testing.T) {
		setDevDB(t, "")
		t.Setenv("PECUNIA_DB", "/tmp/explicit.db")
		if got, err := Path(); err != nil || got != "/tmp/explicit.db" {
			t.Fatalf("with PECUNIA_DB set: got %q, %v", got, err)
		}
	})

	t.Run("user config dir is the fallback", func(t *testing.T) {
		setDevDB(t, "")
		t.Setenv("PECUNIA_DB", "")
		got, err := Path()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(got, filepath.Join("pecunia", "pecunia.db")) {
			t.Fatalf("default path %q does not end in pecunia/pecunia.db", got)
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
	t.Setenv("PECUNIA_DB", filepath.Join(t.TempDir(), "pecunia.db"))

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
		t.Setenv("PECUNIA_DB", filepath.Join(t.TempDir(), "pecunia.db"))
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

// A second process reading while this one writes is the whole reason for these:
// on the rollback journal, and with no busy timeout, it is an instant
// "database is locked" rather than a wait.
func TestOpenConcurrencySettings(t *testing.T) {
	open := func(t *testing.T) *sql.DB {
		t.Helper()
		setDevDB(t, "")
		t.Setenv("PECUNIA_DB", filepath.Join(t.TempDir(), "pecunia.db"))
		conn, err := Open()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn
	}

	t.Run("the journal is write-ahead, so a reader never blocks the writer", func(t *testing.T) {
		conn := open(t)
		var mode string
		if err := conn.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
			t.Fatal(err)
		}
		if !strings.EqualFold(mode, "wal") {
			t.Fatalf("journal_mode = %q; want wal", mode)
		}
	})

	t.Run("a busy database is waited on rather than refused", func(t *testing.T) {
		conn := open(t)
		var ms int
		if err := conn.QueryRow(`PRAGMA busy_timeout`).Scan(&ms); err != nil {
			t.Fatal(err)
		}
		if ms < 1000 {
			t.Fatalf("busy_timeout = %dms; want a wait long enough to be worth having", ms)
		}
	})

	t.Run("one connection, so two writes in a process queue instead of colliding", func(t *testing.T) {
		conn := open(t)
		if n := conn.Stats().MaxOpenConnections; n != 1 {
			t.Fatalf("MaxOpenConnections = %d; want 1", n)
		}
	})

	// WAL is a property of the file, not of the handle: a second opener has to
	// find it already set rather than set it again.
	t.Run("reopening the same file keeps every setting", func(t *testing.T) {
		setDevDB(t, "")
		path := filepath.Join(t.TempDir(), "pecunia.db")
		t.Setenv("PECUNIA_DB", path)

		first, err := Open()
		if err != nil {
			t.Fatal(err)
		}
		first.Close()

		second, err := Open()
		if err != nil {
			t.Fatalf("reopening: %v", err)
		}
		defer second.Close()

		var mode string
		if err := second.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
			t.Fatal(err)
		}
		if !strings.EqualFold(mode, "wal") {
			t.Fatalf("journal_mode on reopen = %q; want wal", mode)
		}
	})
}

// Every transaction you have ever recorded is in this one file. On a shared
// machine the default umask leaves it world-readable, and the -wal beside it
// holds the most recent writes.
func TestOpenPermissions(t *testing.T) {
	t.Run("the database file is readable only by its owner", func(t *testing.T) {
		setDevDB(t, "")
		path := filepath.Join(t.TempDir(), "pecunia", "pecunia.db")
		t.Setenv("PECUNIA_DB", path)

		conn, err := Open()
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("database file is %04o; want 0600", perm)
		}
	})

	t.Run("a directory pecunia creates is its owner's alone", func(t *testing.T) {
		setDevDB(t, "")
		dir := filepath.Join(t.TempDir(), "pecunia")
		t.Setenv("PECUNIA_DB", filepath.Join(dir, "pecunia.db"))

		conn, err := Open()
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Fatalf("created directory is %04o; want 0700", perm)
		}
	})

	// The directory may be $HOME, or /tmp, or anything else PECUNIA_DB points
	// into. Tightening one pecunia did not create is not pecunia's call to make.
	t.Run("a directory that already existed is left exactly as it was", func(t *testing.T) {
		setDevDB(t, "")
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PECUNIA_DB", filepath.Join(dir, "pecunia.db"))

		conn, err := Open()
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o755 {
			t.Fatalf("an existing directory was changed to %04o; want it left at 0755", perm)
		}
	})

	t.Run("the write-ahead log beside it is shut too", func(t *testing.T) {
		setDevDB(t, "")
		path := filepath.Join(t.TempDir(), "pecunia", "pecunia.db")
		t.Setenv("PECUNIA_DB", path)

		conn, err := Open()
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		// Something has to be written for the -wal to exist at all.
		if _, err := conn.Exec(
			`CREATE TABLE perm_check (id INTEGER PRIMARY KEY)`); err != nil {
			t.Fatal(err)
		}

		for _, suffix := range []string{"-wal", "-shm"} {
			info, err := os.Stat(path + suffix)
			if os.IsNotExist(err) {
				continue // nothing written to it yet is nothing to leak
			}
			if err != nil {
				t.Fatal(err)
			}
			if perm := info.Mode().Perm(); perm != 0o600 {
				t.Errorf("%s is %04o; want 0600", suffix, perm)
			}
		}
	})
}

// TestOpenRestoresForeignKeys pins that migrate's foreign_keys toggle is undone:
// the handle Open hands back must enforce references again.
func TestOpenRestoresForeignKeys(t *testing.T) {
	setDevDB(t, "")
	t.Setenv("PECUNIA_DB", filepath.Join(t.TempDir(), "pecunia.db"))
	conn, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var on int
	if err := conn.QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil {
		t.Fatal(err)
	}
	if on != 1 {
		t.Fatalf("foreign_keys = %d after Open; want it back on", on)
	}
}

// TestAdjustmentRebuildPreservesData builds a database as it stood before 013 —
// by running the embedded migrations below it by hand — fills it, and lets Open
// apply only the rebuild. What was written must come through whole: same ids
// (installment_group, transfer_group and transaction_tags all point at them),
// tags still attached, nothing dangling.
func TestAdjustmentRebuildPreservesData(t *testing.T) {
	setDevDB(t, "")
	path := filepath.Join(t.TempDir(), "pecunia.db")

	pre, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pre.Exec(`CREATE TABLE schema_migrations (
		version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (datetime('now')))`); err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() >= "013" {
			continue
		}
		stmts, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pre.Exec(string(stmts)); err != nil {
			t.Fatalf("replaying %s: %v", e.Name(), err)
		}
		if _, err := pre.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, e.Name()); err != nil {
			t.Fatal(err)
		}
	}
	for _, stmt := range []string{
		`INSERT INTO accounts (id, code, name, color, balance, currency) VALUES (1, 'INTER', 'Inter', 'orange', 100000, 'BRL')`,
		`INSERT INTO accounts (id, code, name, color, balance, currency) VALUES (2, 'CASH1', 'Cash', 'green', 15000, 'BRL')`,
		`INSERT INTO transactions (id, title, account_id, value, kind, date) VALUES (7, 'Groceries', 1, 12000, 'outcome', '2026-08-08')`,
		`INSERT INTO transaction_tags (transaction_id, tag) VALUES (7, 'mercado')`,
		`INSERT INTO transactions (id, title, account_id, value, kind, date, transfer_group) VALUES (8, 'Move', 1, 5000, 'outcome', '2026-08-09', 8)`,
		`INSERT INTO transactions (id, title, account_id, value, kind, date, transfer_group) VALUES (9, 'Move', 2, 5000, 'income', '2026-08-09', 8)`,
	} {
		if _, err := pre.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := pre.Close(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PECUNIA_DB", path)
	conn, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	count := func(query string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(query).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM transactions`); n != 3 {
		t.Fatalf("%d transactions after the rebuild; want 3", n)
	}
	if n := count(`SELECT count(*) FROM transaction_tags t
		JOIN transactions tr ON tr.id = t.transaction_id WHERE tr.id = 7`); n != 1 {
		t.Fatalf("tag join finds %d rows; want the tag still attached to id 7", n)
	}
	if n := count(`SELECT count(*) FROM transactions WHERE transfer_group = 8`); n != 2 {
		t.Fatalf("%d transfer legs under group 8; want both", n)
	}
	rows, err := conn.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign_key_check found dangling references after the rebuild")
	}
	// And the rebuilt table takes what the rebuild was for.
	if _, err := conn.Exec(
		`INSERT INTO transactions (title, account_id, value, kind, date) VALUES ('Balance adjustment', 1, -2500, 'adjustment', '2026-08-27')`,
	); err != nil {
		t.Fatalf("a signed adjustment on the rebuilt table = %v", err)
	}
}
