package notes

import (
	"database/sql"
	"path/filepath"
	"testing"

	"pecunia/internal/db"
)

// newTestDB gives the caller its own SQLite file in its own temp dir, so no two
// cases ever share state. Call it inside the subtest, not the parent.
//
// A real file rather than :memory: — the CHECK constraints and the cascades are
// most of what the schema is worth, and only the migration path builds them.
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

// insertNote writes one row straight through the schema, so what comes back is
// the constraint's own verdict and not a Go guard standing in front of it.
func insertNote(conn *sql.DB, title, priority, status, target, path string) (int64, error) {
	res, err := conn.Exec(
		`INSERT INTO notes (title, priority, status, target, path) VALUES (?, ?, ?, ?, ?)`,
		title, priority, status, target, path)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func TestSchema(t *testing.T) {
	t.Run("every priority in the set is accepted", func(t *testing.T) {
		for _, p := range []string{"low", "medium", "high", "critical"} {
			conn := newTestDB(t)
			if _, err := insertNote(conn, "Health care", p, "open", "", p+".md"); err != nil {
				t.Errorf("insert with priority %q = %v", p, err)
			}
		}
	})

	t.Run("a priority outside the set is refused", func(t *testing.T) {
		conn := newTestDB(t)
		if _, err := insertNote(conn, "Health care", "urgent", "open", "", "1.md"); err == nil {
			t.Fatal("insert with priority 'urgent' succeeded; want the CHECK to refuse it")
		}
	})

	t.Run("every status in the set is accepted", func(t *testing.T) {
		for _, s := range []string{"open", "doing", "done", "dropped"} {
			conn := newTestDB(t)
			if _, err := insertNote(conn, "Health care", "low", s, "", s+".md"); err != nil {
				t.Errorf("insert with status %q = %v", s, err)
			}
		}
	})

	t.Run("a status outside the set is refused", func(t *testing.T) {
		conn := newTestDB(t)
		if _, err := insertNote(conn, "Health care", "low", "paused", "", "1.md"); err == nil {
			t.Fatal("insert with status 'paused' succeeded; want the CHECK to refuse it")
		}
	})

	t.Run("a target is a day or nothing", func(t *testing.T) {
		conn := newTestDB(t)
		if _, err := insertNote(conn, "A", "low", "open", "2026-12-16", "a.md"); err != nil {
			t.Errorf("insert with a day = %v", err)
		}
		if _, err := insertNote(conn, "B", "low", "open", "", "b.md"); err != nil {
			t.Errorf("insert with no target = %v", err)
		}
		if _, err := insertNote(conn, "C", "low", "open", "in 3 months", "c.md"); err == nil {
			t.Error("insert with target 'in 3 months' succeeded; want the CHECK to refuse it")
		}
	})

	t.Run("two notes cannot share a file", func(t *testing.T) {
		conn := newTestDB(t)
		if _, err := insertNote(conn, "A", "low", "open", "", "same.md"); err != nil {
			t.Fatal(err)
		}
		if _, err := insertNote(conn, "B", "low", "open", "", "same.md"); err == nil {
			t.Fatal("two rows took the same path; want UNIQUE to refuse it")
		}
	})

	t.Run("counters and stamps fill themselves", func(t *testing.T) {
		conn := newTestDB(t)
		if _, err := conn.Exec(`INSERT INTO notes (title, path) VALUES ('A', 'a.md')`); err != nil {
			t.Fatal(err)
		}
		var priority, status, target, phrase, hash, lastRead, created, updated string
		var mtime, reads, edits int64
		if err := conn.QueryRow(
			`SELECT priority, status, target, target_phrase, mtime, body_hash, read_count,
			        edit_count, last_read_at, created_at, updated_at FROM notes`).
			Scan(&priority, &status, &target, &phrase, &mtime, &hash, &reads, &edits,
				&lastRead, &created, &updated); err != nil {
			t.Fatal(err)
		}
		if priority != "medium" || status != "open" {
			t.Errorf("priority/status = %q/%q; want medium/open", priority, status)
		}
		if target != "" || phrase != "" || hash != "" || lastRead != "" {
			t.Errorf("target/phrase/hash/last_read = %q/%q/%q/%q; want all empty", target, phrase, hash, lastRead)
		}
		if mtime != 0 || reads != 0 || edits != 0 {
			t.Errorf("mtime/reads/edits = %d/%d/%d; want zeros", mtime, reads, edits)
		}
		if created == "" || updated == "" {
			t.Errorf("timestamps = %q / %q; want both filled in", created, updated)
		}
	})

	t.Run("a tag cannot be listed twice on one note", func(t *testing.T) {
		conn := newTestDB(t)
		id, err := insertNote(conn, "A", "low", "open", "", "a.md")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`INSERT INTO note_tags (note_id, tag) VALUES (?, 'health')`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`INSERT INTO note_tags (note_id, tag) VALUES (?, 'health')`, id); err == nil {
			t.Fatal("the same tag went on twice; want the primary key to refuse it")
		}
	})

	t.Run("tags and links go with the note", func(t *testing.T) {
		conn := newTestDB(t)
		id, err := insertNote(conn, "A", "low", "open", "", "a.md")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`INSERT INTO note_tags (note_id, tag) VALUES (?, 'health')`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`INSERT INTO note_links (note_id, kind, ref_id) VALUES (?, 'account', 7)`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`DELETE FROM notes WHERE id = ?`, id); err != nil {
			t.Fatal(err)
		}
		var tags, links int
		if err := conn.QueryRow(`SELECT count(*) FROM note_tags`).Scan(&tags); err != nil {
			t.Fatal(err)
		}
		if err := conn.QueryRow(`SELECT count(*) FROM note_links`).Scan(&links); err != nil {
			t.Fatal(err)
		}
		if tags != 0 || links != 0 {
			t.Errorf("%d tag(s) and %d link(s) survived their note; want the cascade to take them", tags, links)
		}
	})

	t.Run("a link kind outside the set is refused", func(t *testing.T) {
		conn := newTestDB(t)
		id, err := insertNote(conn, "A", "low", "open", "", "a.md")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`INSERT INTO note_links (note_id, kind, ref_id) VALUES (?, 'budget', 1)`, id); err == nil {
			t.Fatal("a link of kind 'budget' went in; want the CHECK to refuse it")
		}
	})
}
