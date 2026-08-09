package db

import (
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
		for _, table := range []string{"accounts", "credit_cards", "categories", "transactions", "transaction_tags"} {
			if _, err := conn.Exec("SELECT 1 FROM " + table); err != nil {
				t.Fatalf("open %d: %s table missing: %v", i+1, table, err)
			}
		}
		conn.Close()
	}
}
