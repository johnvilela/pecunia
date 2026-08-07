package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunSetup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "kakei.db")
	t.Setenv("KAKEI_DB", path)

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
