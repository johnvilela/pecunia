package goals

import (
	"database/sql"
	"path/filepath"
	"testing"

	"pecunia/internal/db"
)

// newTestDB gives the caller its own SQLite file in its own temp dir, so no two
// cases ever share state. Call it inside the subtest, not the parent.
//
// A real file rather than :memory: — the CHECK constraints and the foreign key
// are most of what the schema is worth, and only the migration path builds them.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("PECUNIA_DB", filepath.Join(t.TempDir(), "pecunia.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// insertGoal writes one row straight through the schema, so what comes back is
// the constraint's own verdict and not a Go guard standing in front of it.
func insertGoal(conn *sql.DB, name string, target int64, currency, kind string) error {
	_, err := conn.Exec(
		`INSERT INTO goals (name, target, currency, kind) VALUES (?, ?, ?, ?)`,
		name, target, currency, kind)
	return err
}

// seedGoal and seedTransaction write through raw SQL rather than through the
// stores. This package cannot import pecunia/internal/transactions — that package
// imports this one, because a transaction names the goal it feeds — and the
// same rule holds for its tests, which live in this package too.
func seedGoal(t *testing.T, conn *sql.DB, name string, target int64, currency, kind string) int64 {
	t.Helper()
	res, err := conn.Exec(
		`INSERT INTO goals (name, target, currency, kind) VALUES (?, ?, ?, ?)`,
		name, target, currency, kind)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// seedTransaction files one R$120.00 outcome against an account, naming a goal
// when it is given one.
func seedTransaction(t *testing.T, conn *sql.DB, goalID *int64) int64 {
	t.Helper()
	var account int64
	if err := conn.QueryRow(
		`INSERT INTO accounts (code, name, color, balance, currency)
		 VALUES ('INTER', 'Banco Inter', 'orange', 100000, 'BRL') RETURNING id`).
		Scan(&account); err != nil {
		t.Fatal(err)
	}
	res, err := conn.Exec(
		`INSERT INTO transactions (title, account_id, value, kind, date, goal_id)
		 VALUES ('Groceries', ?, 12000, 'outcome', '2026-08-08', ?)`,
		account, goalID)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSchema(t *testing.T) {
	t.Run("a saving goal is accepted", func(t *testing.T) {
		conn := newTestDB(t)
		if err := insertGoal(conn, "New laptop", 500000, "BRL", "saving"); err != nil {
			t.Fatalf("insert a saving goal = %v", err)
		}
	})

	t.Run("a paying goal is accepted", func(t *testing.T) {
		conn := newTestDB(t)
		if err := insertGoal(conn, "Student loan", 900000, "BRL", "paying"); err != nil {
			t.Fatalf("insert a paying goal = %v", err)
		}
	})

	t.Run("any other kind is refused", func(t *testing.T) {
		conn := newTestDB(t)
		if err := insertGoal(conn, "Someday", 100, "BRL", "wishing"); err == nil {
			t.Fatal("insert with kind 'wishing' succeeded; want the CHECK to refuse it")
		}
	})

	t.Run("a target of zero or less is refused", func(t *testing.T) {
		for _, target := range []int64{0, -1} {
			conn := newTestDB(t)
			if err := insertGoal(conn, "Nothing", target, "BRL", "saving"); err == nil {
				t.Errorf("insert with target %d succeeded; want the CHECK to refuse it", target)
			}
		}
	})

	t.Run("description defaults to empty and the timestamps fill themselves", func(t *testing.T) {
		conn := newTestDB(t)
		if err := insertGoal(conn, "New laptop", 500000, "BRL", "saving"); err != nil {
			t.Fatal(err)
		}
		var description, created, updated string
		if err := conn.QueryRow(
			`SELECT description, created_at, updated_at FROM goals`).
			Scan(&description, &created, &updated); err != nil {
			t.Fatal(err)
		}
		if description != "" {
			t.Errorf("description = %q; want empty", description)
		}
		if created == "" || updated == "" {
			t.Errorf("timestamps = %q / %q; want both filled in", created, updated)
		}
	})

	t.Run("a transaction starts with no goal", func(t *testing.T) {
		conn := newTestDB(t)
		id := seedTransaction(t, conn, nil)
		var goal sql.NullInt64
		if err := conn.QueryRow(`SELECT goal_id FROM transactions WHERE id = ?`, id).
			Scan(&goal); err != nil {
			t.Fatal(err)
		}
		if goal.Valid {
			t.Errorf("goal_id = %d; want NULL", goal.Int64)
		}
	})

	t.Run("deleting a goal leaves the transaction without one", func(t *testing.T) {
		conn := newTestDB(t)
		goalID := seedGoal(t, conn, "New laptop", 500000, "BRL", "saving")
		txID := seedTransaction(t, conn, &goalID)

		if _, err := conn.Exec(`DELETE FROM goals WHERE id = ?`, goalID); err != nil {
			t.Fatalf("delete a goal with a transaction pointing at it = %v", err)
		}

		// The money really moved, so the row stays; only the link goes.
		var value int64
		var goal sql.NullInt64
		if err := conn.QueryRow(`SELECT value, goal_id FROM transactions WHERE id = ?`, txID).
			Scan(&value, &goal); err != nil {
			t.Fatalf("the transaction went with the goal: %v", err)
		}
		if goal.Valid {
			t.Errorf("goal_id = %d; want NULL", goal.Int64)
		}
		if value != 12000 {
			t.Errorf("value = %d; want 12000 — the transaction should be untouched", value)
		}
	})
}

func TestTargetLogSchema(t *testing.T) {
	logChange := func(conn *sql.DB, goalID, previous, target int64, note string) error {
		_, err := conn.Exec(
			`INSERT INTO goal_target_log (goal_id, previous, target, note) VALUES (?, ?, ?, ?)`,
			goalID, previous, target, note)
		return err
	}

	t.Run("an entry keeps what the target was, what it became and why", func(t *testing.T) {
		conn := newTestDB(t)
		id := seedGoal(t, conn, "Quitar o Itaú", 500000, "BRL", "paying")
		if err := logChange(conn, id, 500000, 350000, "consegui um desconto"); err != nil {
			t.Fatal(err)
		}

		var previous, target int64
		var note, created string
		if err := conn.QueryRow(
			`SELECT previous, target, note, created_at FROM goal_target_log`).
			Scan(&previous, &target, &note, &created); err != nil {
			t.Fatal(err)
		}
		if previous != 500000 || target != 350000 {
			t.Errorf("previous/target = %d/%d; want 500000/350000", previous, target)
		}
		if note != "consegui um desconto" {
			t.Errorf("note = %q; want it kept", note)
		}
		// The date is the whole point of a log: an entry with no stamp says
		// nothing about when the target moved.
		if created == "" {
			t.Error("created_at is empty; want the migration to stamp it")
		}
	})

	t.Run("the note is optional", func(t *testing.T) {
		conn := newTestDB(t)
		id := seedGoal(t, conn, "New laptop", 500000, "BRL", "saving")
		if _, err := conn.Exec(
			`INSERT INTO goal_target_log (goal_id, previous, target) VALUES (?, ?, ?)`,
			id, 500000, 300000); err != nil {
			t.Fatal(err)
		}
		var note string
		if err := conn.QueryRow(`SELECT note FROM goal_target_log`).Scan(&note); err != nil {
			t.Fatal(err)
		}
		if note != "" {
			t.Errorf("note = %q; want it to default to empty", note)
		}
	})

	t.Run("a target of zero or less is refused, the same as the goal's own", func(t *testing.T) {
		conn := newTestDB(t)
		id := seedGoal(t, conn, "New laptop", 500000, "BRL", "saving")
		if err := logChange(conn, id, 500000, 0, ""); err == nil {
			t.Error("logged a target of zero; want the CHECK to refuse it")
		}
	})

	t.Run("the log goes with the goal", func(t *testing.T) {
		conn := newTestDB(t)
		id := seedGoal(t, conn, "New laptop", 500000, "BRL", "saving")
		if err := logChange(conn, id, 500000, 300000, "mudei de ideia"); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`PRAGMA foreign_keys = ON`); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`DELETE FROM goals WHERE id = ?`, id); err != nil {
			t.Fatal(err)
		}

		// Unlike a transaction, an entry here is worth nothing without the goal
		// it describes — there is no money in it to lose.
		var n int
		if err := conn.QueryRow(`SELECT count(*) FROM goal_target_log`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%d log entries survived their goal; want the cascade to take them", n)
		}
	})
}
