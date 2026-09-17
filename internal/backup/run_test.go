package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// scene is a database with one category, a notes directory with one file,
// and a local provider to send them to.
type scene struct {
	dbPath, notesDir string
	cfg              Config
}

func setScene(t *testing.T) scene {
	t.Helper()
	path, conn := openDB(t)
	if _, err := conn.Exec("INSERT INTO categories (code, name, color) VALUES ('FOODX', 'Food', 'red')"); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(filepath.Dir(path), "notes")
	os.MkdirAll(notes, 0o700)
	os.WriteFile(filepath.Join(notes, "1-a.md"), []byte("note a"), 0o600)
	return scene{path, notes, Config{Provider: "local", Local: LocalConfig{Dir: filepath.Join(t.TempDir(), "backups")}}}
}

var at = time.Date(2026, 9, 16, 14, 5, 0, 0, time.UTC)

func TestRun(t *testing.T) {
	t.Run("puts a dated archive on the provider", func(t *testing.T) {
		s := setScene(t)
		res, err := Run(s.cfg, s.dbPath, s.notesDir, at)
		if err != nil {
			t.Fatal(err)
		}
		if res.Name != "pecunia-20260916T140500Z.tar.gz" {
			t.Fatalf("name %q", res.Name)
		}
		st, err := os.Stat(filepath.Join(s.cfg.Local.Dir, res.Name))
		if err != nil {
			t.Fatal(err)
		}
		if res.Size != st.Size() || res.Size == 0 {
			t.Fatalf("size %d, file %d", res.Size, st.Size())
		}
		// And it is a real archive.
		f, _ := os.Open(filepath.Join(s.cfg.Local.Dir, res.Name))
		defer f.Close()
		into := t.TempDir()
		if err := Extract(f, into); err != nil {
			t.Fatal(err)
		}
		if n := countCategories(t, filepath.Join(into, "pecunia.db")); n != 1 {
			t.Fatalf("categories %d", n)
		}
	})

	t.Run("encrypts when there is a passphrase", func(t *testing.T) {
		s := setScene(t)
		s.cfg.Passphrase = "pw"
		res, err := Run(s.cfg, s.dbPath, s.notesDir, at)
		if err != nil {
			t.Fatal(err)
		}
		if res.Name != "pecunia-20260916T140500Z.tar.gz.age" {
			t.Fatalf("name %q", res.Name)
		}
		raw, _ := os.ReadFile(filepath.Join(s.cfg.Local.Dir, res.Name))
		if !strings.HasPrefix(string(raw), "age-encryption.org/v1") {
			t.Fatalf("not an age file: %q", raw[:30])
		}
	})

	t.Run("prunes past keep, oldest first", func(t *testing.T) {
		s := setScene(t)
		s.cfg.Keep = 2
		for _, d := range []int{-3, -2, -1} {
			if _, err := Run(s.cfg, s.dbPath, s.notesDir, at.AddDate(0, 0, d)); err != nil {
				t.Fatal(err)
			}
		}
		res, err := Run(s.cfg, s.dbPath, s.notesDir, at)
		if err != nil {
			t.Fatal(err)
		}
		// Each run prunes: the third one already dropped the 13th.
		if strings.Join(res.Pruned, ",") != "pecunia-20260914T140500Z.tar.gz" {
			t.Fatalf("pruned %v", res.Pruned)
		}
		objs, _ := OpenProviderMust(s.cfg).List()
		if len(objs) != 2 || objs[0].Name != "pecunia-20260915T140500Z.tar.gz" {
			t.Fatalf("left %+v", objs)
		}
	})

	t.Run("keep 0 prunes nothing", func(t *testing.T) {
		s := setScene(t)
		for _, d := range []int{-2, -1, 0} {
			Run(s.cfg, s.dbPath, s.notesDir, at.AddDate(0, 0, d))
		}
		objs, _ := OpenProviderMust(s.cfg).List()
		if len(objs) != 3 {
			t.Fatalf("left %d", len(objs))
		}
	})

	t.Run("prune leaves files that are not ours", func(t *testing.T) {
		s := setScene(t)
		s.cfg.Keep = 1
		os.MkdirAll(s.cfg.Local.Dir, 0o700)
		os.WriteFile(filepath.Join(s.cfg.Local.Dir, "keep-me.tar.gz"), []byte("x"), 0o600)
		Run(s.cfg, s.dbPath, s.notesDir, at.AddDate(0, 0, -1))
		Run(s.cfg, s.dbPath, s.notesDir, at)
		if _, err := os.Stat(filepath.Join(s.cfg.Local.Dir, "keep-me.tar.gz")); err != nil {
			t.Fatal("keep-me.tar.gz was pruned")
		}
	})

	t.Run("an invalid config is refused before anything is written", func(t *testing.T) {
		s := setScene(t)
		s.cfg.Local.Dir = ""
		_, err := Run(s.cfg, s.dbPath, s.notesDir, at)
		if err == nil || !strings.Contains(err.Error(), "local.dir") {
			t.Fatalf("err %v", err)
		}
	})
}

func TestRestore(t *testing.T) {
	// backedUp runs one backup, then changes the live data so a restore is
	// visible: a second category, a second note.
	backedUp := func(t *testing.T, cfg Config) scene {
		t.Helper()
		s := setScene(t)
		s.cfg.Passphrase = cfg.Passphrase
		if _, err := Run(s.cfg, s.dbPath, s.notesDir, at); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PECUNIA_DB", s.dbPath)
		conn := mustOpen(t)
		if _, err := conn.Exec("INSERT INTO categories (code, name, color) VALUES ('RENTX', 'Rent', 'blue')"); err != nil {
			t.Fatal(err)
		}
		conn.Close()
		os.WriteFile(filepath.Join(s.notesDir, "2-b.md"), []byte("note b"), 0o600)
		return s
	}

	t.Run("the latest archive replaces the database and the notes", func(t *testing.T) {
		s := backedUp(t, Config{})
		res, err := Restore(s.cfg, "", s.dbPath, s.notesDir)
		if err != nil {
			t.Fatal(err)
		}
		if res.Name != "pecunia-20260916T140500Z.tar.gz" {
			t.Fatalf("name %q", res.Name)
		}
		if n := countCategories(t, s.dbPath); n != 1 {
			t.Fatalf("categories after restore %d, want 1", n)
		}
		if _, err := os.Stat(filepath.Join(s.notesDir, "2-b.md")); !os.IsNotExist(err) {
			t.Fatal("2-b.md survived the restore")
		}
		if _, err := os.Stat(filepath.Join(s.notesDir, "1-a.md")); err != nil {
			t.Fatal("1-a.md missing after restore")
		}
	})

	t.Run("what was there is kept beside, as .bak", func(t *testing.T) {
		s := backedUp(t, Config{})
		res, err := Restore(s.cfg, "", s.dbPath, s.notesDir)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(res.OldDB, ".bak") || !strings.HasPrefix(res.OldDB, s.dbPath) {
			t.Fatalf("old db %q", res.OldDB)
		}
		if n := countCategories(t, res.OldDB); n != 2 {
			t.Fatalf("old db has %d categories, want 2", n)
		}
		if _, err := os.Stat(filepath.Join(res.OldNotes, "2-b.md")); err != nil {
			t.Fatalf("old notes: %v", err)
		}
		if _, err := os.Stat(s.dbPath + "-wal"); !os.IsNotExist(err) {
			t.Fatal("a stale -wal is still beside the restored database")
		}
	})

	t.Run("a named archive", func(t *testing.T) {
		s := backedUp(t, Config{})
		// A newer one, with two categories; restoring the older by name wins.
		if _, err := Run(s.cfg, s.dbPath, s.notesDir, at.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := Restore(s.cfg, "pecunia-20260916T140500Z.tar.gz", s.dbPath, s.notesDir); err != nil {
			t.Fatal(err)
		}
		if n := countCategories(t, s.dbPath); n != 1 {
			t.Fatalf("categories %d", n)
		}
	})

	t.Run("an encrypted archive needs the passphrase", func(t *testing.T) {
		s := backedUp(t, Config{Passphrase: "pw"})
		s.cfg.Passphrase = ""
		_, err := Restore(s.cfg, "", s.dbPath, s.notesDir)
		if err == nil || !strings.Contains(err.Error(), "passphrase") {
			t.Fatalf("err %v", err)
		}
		s.cfg.Passphrase = "wrong"
		_, err = Restore(s.cfg, "", s.dbPath, s.notesDir)
		if err == nil || !strings.Contains(err.Error(), "wrong passphrase") {
			t.Fatalf("err %v", err)
		}
		if n := countCategories(t, s.dbPath); n != 2 {
			t.Fatal("the live database was touched by a failed restore")
		}
		s.cfg.Passphrase = "pw"
		if _, err := Restore(s.cfg, "", s.dbPath, s.notesDir); err != nil {
			t.Fatal(err)
		}
		if n := countCategories(t, s.dbPath); n != 1 {
			t.Fatalf("categories %d", n)
		}
	})

	t.Run("nothing to restore says so", func(t *testing.T) {
		s := setScene(t)
		_, err := Restore(s.cfg, "", s.dbPath, s.notesDir)
		if err == nil || !strings.Contains(err.Error(), "no archives") {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("a corrupt archive leaves the live data alone", func(t *testing.T) {
		s := backedUp(t, Config{})
		os.WriteFile(filepath.Join(s.cfg.Local.Dir, "pecunia-20260917T000000Z.tar.gz"), []byte("garbage"), 0o600)
		_, err := Restore(s.cfg, "", s.dbPath, s.notesDir)
		if err == nil {
			t.Fatal("garbage restored")
		}
		if n := countCategories(t, s.dbPath); n != 2 {
			t.Fatalf("categories %d, want the live 2", n)
		}
	})
}
