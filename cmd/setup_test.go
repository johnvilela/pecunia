package main

import (
	"os"
	"path/filepath"
	"testing"

	"pecunia/internal/categories"
	"pecunia/internal/db"
)

func TestRunSetup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "pecunia.db")
	t.Setenv("PECUNIA_DB", path)

	if err := runSetup(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database not created: %v", err)
	}

	// Second run without --force must leave the file alone.
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := runSetup(false); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("existing database was modified without --force")
	}

	// --force moves the old file aside and leaves a fresh one behind.
	if err := runSetup(true); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(path + ".*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected 1 backup, got %v (%v)", backups, err)
	}
	if b, _ := os.ReadFile(backups[0]); string(b) != string(before) {
		t.Fatal("backup does not hold the original contents")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database not recreated after --force: %v", err)
	}
}

func TestRunSetupSeedsCategories(t *testing.T) {
	countCategories := func(t *testing.T) int {
		t.Helper()
		conn, err := db.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		all, err := categories.NewStore(conn).List()
		if err != nil {
			t.Fatal(err)
		}
		return len(all)
	}

	t.Run("a new database starts with the starter set", func(t *testing.T) {
		t.Setenv("PECUNIA_DB", filepath.Join(t.TempDir(), "pecunia.db"))
		if err := runSetup(false); err != nil {
			t.Fatal(err)
		}
		if got := countCategories(t); got != len(categories.Starter) {
			t.Fatalf("%d categories after setup; want %d", got, len(categories.Starter))
		}
	})

	t.Run("setup on an existing database tops up what is missing", func(t *testing.T) {
		t.Setenv("PECUNIA_DB", filepath.Join(t.TempDir(), "pecunia.db"))
		if err := runSetup(false); err != nil {
			t.Fatal(err)
		}

		conn, err := db.Open()
		if err != nil {
			t.Fatal(err)
		}
		s := categories.NewStore(conn)
		c, err := s.ByCode(categories.Starter[0].Code)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Delete(c.ID); err != nil {
			t.Fatal(err)
		}
		conn.Close()

		// A database that predates the module must not need --force, which would
		// mean backing up real data just to get categories.
		if err := runSetup(false); err != nil {
			t.Fatal(err)
		}
		if got := countCategories(t); got != len(categories.Starter) {
			t.Fatalf("%d categories after a second setup; want %d", got, len(categories.Starter))
		}
	})
}
