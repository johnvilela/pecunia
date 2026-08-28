package logs

import (
	"database/sql"
	"path/filepath"
	"testing"

	"pecunia/internal/db"
)

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

// insertLog writes a row straight past the package, aiming these cases at the
// constraints rather than at Record.
func insertLog(conn *sql.DB, source, action string) error {
	_, err := conn.Exec(
		`INSERT INTO logs (source, action, entity, entity_id) VALUES (?, ?, 'account', 1)`,
		source, action)
	return err
}

func TestSchemaConstraints(t *testing.T) {
	t.Run("an action outside created, edited, deleted is refused", func(t *testing.T) {
		conn := newTestDB(t)
		if err := insertLog(conn, "user", "renamed"); err == nil {
			t.Fatal("an unknown action was written; want the CHECK to refuse it")
		}
	})

	t.Run("a source outside user, system, ai is refused", func(t *testing.T) {
		conn := newTestDB(t)
		if err := insertLog(conn, "cron", "created"); err == nil {
			t.Fatal("an unknown source was written; want the CHECK to refuse it")
		}
	})

	t.Run("an entity id that matches nothing still inserts", func(t *testing.T) {
		conn := newTestDB(t)
		if _, err := conn.Exec(
			`INSERT INTO logs (source, action, entity, entity_id) VALUES ('user', 'deleted', 'account', 999)`,
		); err != nil {
			t.Fatalf("a log for a gone entity was refused: %v; want no foreign key", err)
		}
	})

	t.Run("changes defaults to empty", func(t *testing.T) {
		conn := newTestDB(t)
		if err := insertLog(conn, "user", "created"); err != nil {
			t.Fatal(err)
		}
		var changes string
		if err := conn.QueryRow(`SELECT changes FROM logs`).Scan(&changes); err != nil {
			t.Fatal(err)
		}
		if changes != "" {
			t.Fatalf("changes = %q; want empty by default", changes)
		}
	})
}
