package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pecunia/internal/db"
)

// openDB makes this case's own database and returns its path with the
// connection still open, the way a running pecunia would hold it.
func openDB(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pecunia.db")
	t.Setenv("PECUNIA_DB", path)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return path, conn
}

func countCategories(t *testing.T, path string) int {
	t.Helper()
	t.Setenv("PECUNIA_DB", path)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var n int
	if err := conn.QueryRow("SELECT count(*) FROM categories").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestArchive(t *testing.T) {
	t.Run("round-trips the database and the notes", func(t *testing.T) {
		path, conn := openDB(t)
		if _, err := conn.Exec("INSERT INTO categories (code, name, color) VALUES ('FOODX', 'Food', 'red')"); err != nil {
			t.Fatal(err)
		}
		notes := filepath.Join(filepath.Dir(path), "notes")
		os.MkdirAll(filepath.Join(notes, "sub"), 0o700)
		os.WriteFile(filepath.Join(notes, "1-hello.md"), []byte("---\ntitle: hello\n---\nbody\n"), 0o600)
		os.WriteFile(filepath.Join(notes, "sub", "deep.md"), []byte("deep"), 0o600)

		var buf bytes.Buffer
		if err := Create(&buf, path, notes); err != nil {
			t.Fatal(err)
		}

		into := t.TempDir()
		if err := Extract(&buf, into); err != nil {
			t.Fatal(err)
		}
		if n := countCategories(t, filepath.Join(into, "pecunia.db")); n != 1 {
			t.Fatalf("restored database has %d categories, want 1", n)
		}
		got, err := os.ReadFile(filepath.Join(into, "notes", "1-hello.md"))
		if err != nil || !strings.Contains(string(got), "title: hello") {
			t.Fatalf("note: %q, %v", got, err)
		}
		if _, err := os.ReadFile(filepath.Join(into, "notes", "sub", "deep.md")); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(filepath.Join(into, "pecunia.db"))
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("database mode %v, want 0600", st.Mode().Perm())
		}
	})

	t.Run("includes a write still in the WAL", func(t *testing.T) {
		path, conn := openDB(t)
		// A row through the open connection sits in pecunia.db-wal until a
		// checkpoint; a copy of pecunia.db alone would not have it.
		if _, err := conn.Exec("INSERT INTO categories (code, name, color) VALUES ('FOODX', 'Food', 'red')"); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := Create(&buf, path, filepath.Join(t.TempDir(), "none")); err != nil {
			t.Fatal(err)
		}
		into := t.TempDir()
		if err := Extract(&buf, into); err != nil {
			t.Fatal(err)
		}
		if n := countCategories(t, filepath.Join(into, "pecunia.db")); n != 1 {
			t.Fatalf("restored database has %d categories, want 1", n)
		}
	})

	t.Run("no notes directory is fine", func(t *testing.T) {
		path, _ := openDB(t)
		var buf bytes.Buffer
		if err := Create(&buf, path, filepath.Join(t.TempDir(), "none")); err != nil {
			t.Fatal(err)
		}
		into := t.TempDir()
		if err := Extract(&buf, into); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(into, "notes")); !os.IsNotExist(err) {
			t.Fatalf("notes dir: %v, want not to exist", err)
		}
	})

	t.Run("no database is an error", func(t *testing.T) {
		var buf bytes.Buffer
		err := Create(&buf, filepath.Join(t.TempDir(), "missing.db"), "")
		if err == nil || !strings.Contains(err.Error(), "missing.db") {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("extract refuses an entry that escapes", func(t *testing.T) {
		buf := tarWith(t, "../evil", "x")
		err := Extract(buf, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "../evil") {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("extract refuses an entry it does not know", func(t *testing.T) {
		buf := tarWith(t, "other.txt", "x")
		err := Extract(buf, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "other.txt") {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("extract wants the database", func(t *testing.T) {
		buf := tarWith(t, "notes/a.md", "x")
		err := Extract(buf, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "no pecunia.db") {
			t.Fatalf("err %v", err)
		}
	})
}

func TestName(t *testing.T) {
	at := time.Date(2026, 9, 16, 14, 5, 0, 0, time.UTC)
	if got := Name(at, false); got != "pecunia-20260916T140500Z.tar.gz" {
		t.Errorf("plain name %q", got)
	}
	if got := Name(at, true); got != "pecunia-20260916T140500Z.tar.gz.age" {
		t.Errorf("encrypted name %q", got)
	}
	for _, name := range []string{"pecunia-20260916T140500Z.tar.gz", "pecunia-20260916T140500Z.tar.gz.age"} {
		got, ok := Stamp(name)
		if !ok || !got.Equal(at) {
			t.Errorf("Stamp(%q) = %v, %v", name, got, ok)
		}
	}
	for _, name := range []string{"notes.txt", "pecunia-yesterday.tar.gz", "pecunia-20260916T140500Z.zip"} {
		if _, ok := Stamp(name); ok {
			t.Errorf("Stamp(%q) accepted", name)
		}
	}
	if Encrypted("a.tar.gz.age") != true || Encrypted("a.tar.gz") != false {
		t.Error("Encrypted")
	}
}

func TestEncrypt(t *testing.T) {
	t.Run("round-trips with the passphrase", func(t *testing.T) {
		var buf bytes.Buffer
		w, err := Encrypt(&buf, "open sesame")
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte("secret ledger"))
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(buf.Bytes(), []byte("secret ledger")) {
			t.Fatal("plaintext in the output")
		}
		r, err := Decrypt(&buf, "open sesame")
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		out.ReadFrom(r)
		if out.String() != "secret ledger" {
			t.Fatalf("got %q", out.String())
		}
	})

	t.Run("the wrong passphrase is refused", func(t *testing.T) {
		var buf bytes.Buffer
		w, _ := Encrypt(&buf, "right")
		w.Write([]byte("x"))
		w.Close()
		_, err := Decrypt(&buf, "wrong")
		if err == nil || !strings.Contains(err.Error(), "passphrase") {
			t.Fatalf("err %v", err)
		}
	})
}

// tarWith is a one-entry tar.gz, for feeding Extract things Create never makes.
func tarWith(t *testing.T, name, content string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte(content))
	tw.Close()
	gz.Close()
	return &buf
}

func mustOpen(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

// OpenProviderMust is OpenProvider for a config the test knows is good.
func OpenProviderMust(cfg Config) Provider {
	p, err := OpenProvider(cfg)
	if err != nil {
		panic(err)
	}
	return p
}
