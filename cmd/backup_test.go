package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pecunia/internal/backup"
	"pecunia/internal/db"
)

// runBackupIn points PECUNIA_DB at this case's database (backup.toml and
// the notes follow it), stubs systemctl, captures the output and runs.
func runBackupIn(t *testing.T, dbPath string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("PECUNIA_DB", dbPath)
	t.Setenv("PECUNIA_NOTES", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(filepath.Dir(dbPath), "xdg"))
	var buf bytes.Buffer
	old := out
	out = &buf
	t.Cleanup(func() { out = old })
	err := runBackup(args)
	return buf.String(), err
}

func stubSystemctl(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	old, oldHave := backup.Systemctl, backup.HaveSystemd
	backup.Systemctl = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return nil
	}
	backup.HaveSystemd = func() bool { return true }
	t.Cleanup(func() { backup.Systemctl, backup.HaveSystemd = old, oldHave })
	return &calls
}

// seededDB is a database with one category and a notes dir with one file.
func seededDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pecunia.db")
	t.Setenv("PECUNIA_DB", path)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec("INSERT INTO categories (code, name, color) VALUES ('FOODX', 'Food', 'red')"); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	notes := filepath.Join(filepath.Dir(path), "notes")
	os.MkdirAll(notes, 0o700)
	os.WriteFile(filepath.Join(notes, "1-a.md"), []byte("a"), 0o600)
	return path
}

func TestBackupSetup(t *testing.T) {
	t.Run("flags write backup.toml without a form", func(t *testing.T) {
		path := seededDB(t)
		stubSystemctl(t)
		dir := filepath.Join(t.TempDir(), "usb")
		got, err := runBackupIn(t, path, "setup", "--provider", "local", "--dir", dir, "--keep", "5")
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := backup.Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Provider != "local" || cfg.Local.Dir != dir || cfg.Keep != 5 || cfg.Every != "" {
			t.Fatalf("saved %+v", cfg)
		}
		if !strings.Contains(got, "backup.toml") {
			t.Fatalf("output %q", got)
		}
	})

	t.Run("s3 flags", func(t *testing.T) {
		path := seededDB(t)
		stubSystemctl(t)
		_, err := runBackupIn(t, path, "setup", "--provider", "s3", "--bucket", "b", "--region", "eu-west-1",
			"--access-key", "ak", "--secret-key", "sk", "--prefix", "p", "--endpoint", "https://e", "--passphrase", "pw")
		if err != nil {
			t.Fatal(err)
		}
		cfg, _ := backup.Load()
		want := backup.S3Config{Bucket: "b", Prefix: "p", Region: "eu-west-1", Endpoint: "https://e", AccessKey: "ak", SecretKey: "sk"}
		if cfg.S3 != want || cfg.Passphrase != "pw" {
			t.Fatalf("saved %+v", cfg)
		}
	})

	t.Run("an every flag installs the timer", func(t *testing.T) {
		path := seededDB(t)
		calls := stubSystemctl(t)
		got, err := runBackupIn(t, path, "setup", "--provider", "local", "--dir", "/x", "--every", "2/day")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(*calls, " | ") != "daemon-reload | enable --now pecunia-backup.timer" {
			t.Fatalf("systemctl %v", *calls)
		}
		if !strings.Contains(got, "2/day") {
			t.Fatalf("output %q", got)
		}
	})

	t.Run("a bad flag set is refused before saving", func(t *testing.T) {
		path := seededDB(t)
		stubSystemctl(t)
		_, err := runBackupIn(t, path, "setup", "--provider", "s3", "--bucket", "b")
		if err == nil || !strings.Contains(err.Error(), "s3.access_key") {
			t.Fatalf("err %v", err)
		}
		if _, err := backup.Load(); err == nil {
			t.Fatal("backup.toml was written")
		}
	})
}

func TestBackupRunListRestore(t *testing.T) {
	setup := func(t *testing.T) (string, string) {
		t.Helper()
		path := seededDB(t)
		stubSystemctl(t)
		dir := filepath.Join(t.TempDir(), "usb")
		if _, err := runBackupIn(t, path, "setup", "--provider", "local", "--dir", dir, "--keep", "2"); err != nil {
			t.Fatal(err)
		}
		return path, dir
	}

	t.Run("run sends an archive and says so", func(t *testing.T) {
		path, dir := setup(t)
		got, err := runBackupIn(t, path, "run")
		if err != nil {
			t.Fatal(err)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "pecunia-") {
			t.Fatalf("dir %v", entries)
		}
		if !strings.Contains(got, entries[0].Name()) || !strings.Contains(got, dir) {
			t.Fatalf("output %q", got)
		}
	})

	t.Run("run without a config points at setup", func(t *testing.T) {
		path := seededDB(t)
		_, err := runBackupIn(t, path, "run")
		if err == nil || !strings.Contains(err.Error(), "pecunia backup setup") {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("list shows what is there, newest last", func(t *testing.T) {
		path, dir := setup(t)
		got, err := runBackupIn(t, path, "list")
		if err != nil || !strings.Contains(got, "no archives") {
			t.Fatalf("empty list: %q, %v", got, err)
		}
		os.MkdirAll(dir, 0o700)
		os.WriteFile(filepath.Join(dir, "pecunia-20260915T030000Z.tar.gz"), []byte("older"), 0o600)
		os.WriteFile(filepath.Join(dir, "pecunia-20260916T030000Z.tar.gz.age"), []byte("newer"), 0o600)
		got, err = runBackupIn(t, path, "list")
		if err != nil {
			t.Fatal(err)
		}
		i, j := strings.Index(got, "20260915"), strings.Index(got, "20260916")
		if i < 0 || j < 0 || i > j {
			t.Fatalf("output %q", got)
		}
		if !strings.Contains(got, "encrypted") {
			t.Fatalf("no encryption mark in %q", got)
		}
	})

	t.Run("restore -y puts the archive in place", func(t *testing.T) {
		path, _ := setup(t)
		if _, err := runBackupIn(t, path, "run"); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PECUNIA_DB", path)
		conn, _ := db.Open()
		conn.Exec("INSERT INTO categories (code, name, color) VALUES ('RENTX', 'Rent', 'blue')")
		conn.Close()
		got, err := runBackupIn(t, path, "restore", "-y")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, ".bak") {
			t.Fatalf("output %q", got)
		}
		conn, _ = db.Open()
		defer conn.Close()
		var n int
		conn.QueryRow("SELECT count(*) FROM categories").Scan(&n)
		if n != 1 {
			t.Fatalf("categories %d, want 1", n)
		}
	})

	t.Run("restore of a name that is not there", func(t *testing.T) {
		path, _ := setup(t)
		_, err := runBackupIn(t, path, "restore", "-y", "pecunia-20200101T000000Z.tar.gz")
		if err == nil || !strings.Contains(err.Error(), "pecunia-20200101T000000Z.tar.gz") {
			t.Fatalf("err %v", err)
		}
	})
}

func TestBackupSchedule(t *testing.T) {
	setup := func(t *testing.T) (string, *[]string) {
		t.Helper()
		path := seededDB(t)
		calls := stubSystemctl(t)
		if _, err := runBackupIn(t, path, "setup", "--provider", "local", "--dir", "/x"); err != nil {
			t.Fatal(err)
		}
		return path, calls
	}

	t.Run("sets every and installs the timer", func(t *testing.T) {
		path, calls := setup(t)
		got, err := runBackupIn(t, path, "schedule", "3/week")
		if err != nil {
			t.Fatal(err)
		}
		cfg, _ := backup.Load()
		if cfg.Every != "3/week" {
			t.Fatalf("every %q", cfg.Every)
		}
		if strings.Join(*calls, " | ") != "daemon-reload | enable --now pecunia-backup.timer" {
			t.Fatalf("systemctl %v", *calls)
		}
		if !strings.Contains(got, "Mon,Wed,Fri") {
			t.Fatalf("output %q", got)
		}
	})

	t.Run("off removes the timer and clears every", func(t *testing.T) {
		path, calls := setup(t)
		runBackupIn(t, path, "schedule", "1/day")
		*calls = nil
		if _, err := runBackupIn(t, path, "schedule", "off"); err != nil {
			t.Fatal(err)
		}
		cfg, _ := backup.Load()
		if cfg.Every != "" {
			t.Fatalf("every %q", cfg.Every)
		}
		if len(*calls) == 0 || !strings.HasPrefix((*calls)[0], "disable --now") {
			t.Fatalf("systemctl %v", *calls)
		}
	})

	t.Run("no argument reports the state", func(t *testing.T) {
		path, _ := setup(t)
		got, err := runBackupIn(t, path, "schedule")
		if err != nil || !strings.Contains(got, "no schedule") {
			t.Fatalf("%q, %v", got, err)
		}
		runBackupIn(t, path, "schedule", "2/day")
		got, _ = runBackupIn(t, path, "schedule")
		if !strings.Contains(got, "2/day") || !strings.Contains(got, "03,15:00:00") {
			t.Fatalf("%q", got)
		}
	})

	t.Run("a bad schedule is refused", func(t *testing.T) {
		path, _ := setup(t)
		_, err := runBackupIn(t, path, "schedule", "9/week")
		if err == nil || !strings.Contains(err.Error(), "at most 7") {
			t.Fatalf("err %v", err)
		}
	})
}

func TestBackupScheduleWithoutSystemd(t *testing.T) {
	path := seededDB(t)
	calls := stubSystemctl(t)
	backup.HaveSystemd = func() bool { return false }
	runBackupIn(t, path, "setup", "--provider", "local", "--dir", "/x")
	got, err := runBackupIn(t, path, "schedule", "2/day")
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatalf("systemctl was run: %v", *calls)
	}
	if !strings.Contains(got, "crontab") || !strings.Contains(got, "0 3,15 * * *") {
		t.Fatalf("no crontab line in %q", got)
	}
	cfg, _ := backup.Load()
	if cfg.Every != "2/day" {
		t.Fatalf("every %q", cfg.Every)
	}
}

func TestBackupStatusAndHelp(t *testing.T) {
	t.Run("help", func(t *testing.T) {
		got, err := runBackupIn(t, filepath.Join(t.TempDir(), "p.db"), "-h")
		if err != nil || !strings.Contains(got, "pecunia backup setup") {
			t.Fatalf("%q, %v", got, err)
		}
	})

	t.Run("status without a config", func(t *testing.T) {
		got, err := runBackupIn(t, filepath.Join(t.TempDir(), "p.db"))
		if err != nil || !strings.Contains(got, "pecunia backup setup") {
			t.Fatalf("%q, %v", got, err)
		}
	})

	t.Run("status with one", func(t *testing.T) {
		path := seededDB(t)
		stubSystemctl(t)
		runBackupIn(t, path, "setup", "--provider", "local", "--dir", "/x", "--keep", "4", "--passphrase", "pw")
		got, err := runBackupIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"local", "/x", "keep 4", "encrypted", "no schedule"} {
			if !strings.Contains(got, want) {
				t.Errorf("status lacks %q:\n%s", want, got)
			}
		}
		if strings.Contains(got, "pw") {
			t.Errorf("status leaks the passphrase:\n%s", got)
		}
	})

	t.Run("unknown subcommand", func(t *testing.T) {
		_, err := runBackupIn(t, filepath.Join(t.TempDir(), "p.db"), "frobnicate")
		if err == nil || !strings.Contains(err.Error(), "frobnicate") {
			t.Fatalf("err %v", err)
		}
	})
}
