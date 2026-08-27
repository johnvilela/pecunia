package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"kakei/internal/db"
)

func runLogsIn(t *testing.T, dbPath string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("KAKEI_DB", dbPath)

	var buf bytes.Buffer
	old := out
	out = &buf
	t.Cleanup(func() { out = old })

	err := runLogs(args)
	return buf.String(), err
}

// seedLog writes a trail row straight into the database at path.
func seedLog(t *testing.T, path, source, action, entity string, id int64) {
	t.Helper()
	t.Setenv("KAKEI_DB", path)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Exec(
		`INSERT INTO logs (source, action, entity, entity_id) VALUES (?, ?, ?, ?)`,
		source, action, entity, id); err != nil {
		t.Fatal(err)
	}
}

func TestLogsCommand(t *testing.T) {
	t.Run("the trail renders", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedLog(t, path, "user", "created", "account", 1)
		seedLog(t, path, "system", "created", "card_bill", 2)

		got, err := runLogsIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"account", "card_bill", "system", "created"} {
			if !strings.Contains(got, want) {
				t.Errorf("output is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("filters narrow the trail", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedLog(t, path, "user", "created", "account", 1)
		seedLog(t, path, "user", "deleted", "goal", 2)

		got, err := runLogsIn(t, path, "--entity", "goal")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "goal") || strings.Contains(got, "account") {
			t.Errorf("--entity goal did not narrow to goals:\n%s", got)
		}
	})

	t.Run("an id without its entity is refused", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		if _, err := runLogsIn(t, path, "--id", "3"); err == nil {
			t.Fatal("--id alone was accepted; an id is only an id of something")
		}
	})

	t.Run("an unknown entity is refused by name", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		_, err := runLogsIn(t, path, "--entity", "wallet")
		if err == nil {
			t.Fatal("an unknown entity was accepted")
		}
		if !strings.Contains(err.Error(), "account") {
			t.Fatalf("err = %v; want it to name what an entity can be", err)
		}
	})

	t.Run("an unknown action is refused", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		if _, err := runLogsIn(t, path, "--action", "renamed"); err == nil {
			t.Fatal("an unknown action was accepted")
		}
	})

	t.Run("an unknown source is refused", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		if _, err := runLogsIn(t, path, "--source", "cron"); err == nil {
			t.Fatal("an unknown source was accepted")
		}
	})

	t.Run("a bad date is refused", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		if _, err := runLogsIn(t, path, "--from", "august"); err == nil {
			t.Fatal("a date that is not YYYY-MM-DD was accepted")
		}
	})

	t.Run("an empty trail says so", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		got, err := runLogsIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "nothing") {
			t.Errorf("output does not say the trail is empty:\n%s", got)
		}
	})

	t.Run("help prints without a database", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		got, err := runLogsIn(t, path, "-h")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "kakei logs") {
			t.Errorf("help output looks wrong:\n%s", got)
		}
	})
}
