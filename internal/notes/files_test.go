package notes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pecunia/internal/logs"
)

// rewrite replaces a file's content the way an editor outside pecunia would,
// and pushes its mtime a second forward so the change shows even on a
// filesystem that rounds mtimes.
func rewrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	later := st.ModTime().Add(time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}

func canonical(t *testing.T, s *Store, n Note) string {
	t.Helper()
	data, err := os.ReadFile(s.Path(n))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRefresh(t *testing.T) {
	t.Run("an untouched file is not re-read", func(t *testing.T) {
		s, conn := newTestStore(t)
		n := mustCreate(t, s, Note{Title: "Health care", Priority: PriorityLow, Status: StatusOpen}, "body")
		got, err := s.List(Filter{})
		if err != nil {
			t.Fatal(err)
		}
		if got[0].EditCount != 0 || got[0].Problem != "" || got[0].Mtime != n.Mtime {
			t.Fatalf("List() = %+v", got[0])
		}
		if count(t, conn, "logs") != 1 {
			t.Fatalf("%d log rows; want only the create", count(t, conn, "logs"))
		}
	})

	t.Run("a file edited outside pecunia is re-indexed and logged once", func(t *testing.T) {
		s, conn := newTestStore(t)
		account(t, conn, "INTER")
		n := mustCreate(t, s, Note{Title: "Health care", Priority: PriorityLow, Status: StatusOpen}, "body")
		rewrite(t, s.Path(n), "---\ntitle: Better health care\npriority: high\ntarget: in 2 weeks\ntags: [Health]\naccounts: [inter]\n---\nnew body\n")

		got, err := s.Get(n.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Title != "Better health care" || got.Priority != PriorityHigh || got.Target != inDays(14) ||
			got.TargetPhrase != "in 2 weeks" || strings.Join(got.Tags, ",") != "health" ||
			strings.Join(got.Accounts, ",") != "INTER" || got.EditCount != 1 || got.Problem != "" {
			t.Fatalf("Get() = %+v", got)
		}
		if got.Mtime == n.Mtime {
			t.Error("mtime was not updated; the file would be re-read on every list")
		}
		// The file is the owner's: a lazy pass reads it and never writes it.
		if data := canonical(t, s, got); !strings.Contains(data, "target: in 2 weeks\n") {
			t.Errorf("the file was rewritten:\n%s", data)
		}
		trail, err := logs.List(conn, logs.Filter{Entity: "note", Action: "edited"})
		if err != nil || len(trail) != 1 {
			t.Fatalf("trail = %+v, %v; want one edited row", trail, err)
		}
		for _, want := range []string{`"title"`, `"priority"`, `"target"`, `"tags"`, `"accounts"`, `"body"`} {
			if !strings.Contains(trail[0].Changes, want) {
				t.Errorf("changes = %s; want %s", trail[0].Changes, want)
			}
		}
		if _, err := s.List(Filter{}); err != nil {
			t.Fatal(err)
		}
		if trail, _ = logs.List(conn, logs.Filter{Entity: "note", Action: "edited"}); len(trail) != 1 {
			t.Fatalf("a second list logged again: %d rows", len(trail))
		}
	})

	t.Run("a touch that changed nothing is not an edit", func(t *testing.T) {
		s, conn := newTestStore(t)
		n := mustCreate(t, s, Note{Title: "Health care", Priority: PriorityLow, Status: StatusOpen}, "body")
		rewrite(t, s.Path(n), canonical(t, s, n)+"\n\n")
		got, err := s.Get(n.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.EditCount != 0 || got.Mtime == n.Mtime {
			t.Fatalf("edits = %d, mtime moved = %v; want 0 and true", got.EditCount, got.Mtime != n.Mtime)
		}
		if count(t, conn, "logs") != 1 {
			t.Fatalf("%d log rows; want only the create", count(t, conn, "logs"))
		}
	})

	t.Run("a broken file marks the row and keeps it", func(t *testing.T) {
		s, _ := newTestStore(t)
		n := mustCreate(t, s, Note{Title: "Health care", Priority: PriorityLow, Status: StatusOpen}, "body")
		rewrite(t, s.Path(n), "title: oops\n")
		got, err := s.List(Filter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Title != "Health care" || !strings.Contains(got[0].Problem, "no front matter") {
			t.Fatalf("List() = %+v", got)
		}
	})

	t.Run("an unknown link in the file marks the row", func(t *testing.T) {
		s, _ := newTestStore(t)
		n := mustCreate(t, s, Note{Title: "Health care", Priority: PriorityLow, Status: StatusOpen}, "body")
		rewrite(t, s.Path(n), "---\ntitle: Health care\naccounts: [NOPE1]\n---\n")
		got, err := s.Get(n.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got.Problem, `no account matching "NOPE1"`) {
			t.Fatalf("problem = %q", got.Problem)
		}
	})

	t.Run("a missing file marks the row", func(t *testing.T) {
		s, _ := newTestStore(t)
		n := mustCreate(t, s, Note{Title: "Health care", Priority: PriorityLow, Status: StatusOpen}, "body")
		if err := os.Remove(s.Path(n)); err != nil {
			t.Fatal(err)
		}
		got, err := s.List(Filter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Problem != "file missing — pecunia n sync" {
			t.Fatalf("List() = %+v", got)
		}
	})
}

func TestSync(t *testing.T) {
	t.Run("nothing to do", func(t *testing.T) {
		s, _ := newTestStore(t)
		r, err := s.Sync()
		if err != nil || !r.Empty() {
			t.Fatalf("Sync() = %+v, %v", r, err)
		}
	})

	t.Run("adopts a file dropped in the directory and names it", func(t *testing.T) {
		s, conn := newTestStore(t)
		account(t, conn, "INTER")
		if err := os.MkdirAll(s.dir, 0o700); err != nil {
			t.Fatal(err)
		}
		orphan := filepath.Join(s.dir, "ideia.md")
		if err := os.WriteFile(orphan, []byte("---\ntitle: Trocar de banco\ntarget: next month\naccounts: [inter]\n---\n\nver o Inter\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		r, err := s.Sync()
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Adopted) != 1 || r.Adopted[0] != "ideia.md → 1-trocar-de-banco.md" {
			t.Fatalf("adopted = %q", r.Adopted)
		}
		if _, err := os.Stat(orphan); !os.IsNotExist(err) {
			t.Error("the orphan is still there")
		}
		got, err := s.Get(1)
		if err != nil {
			t.Fatal(err)
		}
		if got.Title != "Trocar de banco" || got.TargetPhrase != "next month" || got.Target == "" ||
			strings.Join(got.Accounts, ",") != "INTER" {
			t.Fatalf("Get() = %+v", got)
		}
		if data := canonical(t, s, got); !strings.Contains(data, "target: "+got.Target+" # next month\n") ||
			!strings.HasSuffix(data, "\n\nver o Inter\n") {
			t.Errorf("file =\n%s", data)
		}
	})

	t.Run("a file that does not parse is reported and left alone", func(t *testing.T) {
		s, conn := newTestStore(t)
		if err := os.MkdirAll(s.dir, 0o700); err != nil {
			t.Fatal(err)
		}
		bad := filepath.Join(s.dir, "bad.md")
		if err := os.WriteFile(bad, []byte("just text\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		r, err := s.Sync()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(r.Problems["bad.md"], "no front matter") {
			t.Fatalf("problems = %v", r.Problems)
		}
		if _, err := os.Stat(bad); err != nil {
			t.Error("the file was removed")
		}
		if count(t, conn, "notes") != 0 {
			t.Error("a row was made for it")
		}
	})

	t.Run("dotfiles and other extensions are ignored", func(t *testing.T) {
		s, conn := newTestStore(t)
		if err := os.MkdirAll(s.dir, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{".1-x.md.swp", "notes.txt", ".hidden.md"} {
			if err := os.WriteFile(filepath.Join(s.dir, name), []byte("---\ntitle: x\n---\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Mkdir(filepath.Join(s.dir, "sub.md"), 0o700); err != nil {
			t.Fatal(err)
		}
		r, err := s.Sync()
		if err != nil || !r.Empty() || count(t, conn, "notes") != 0 {
			t.Fatalf("Sync() = %+v, %v with %d notes", r, err, count(t, conn, "notes"))
		}
	})

	t.Run("a missing file is reported, never dropped", func(t *testing.T) {
		s, conn := newTestStore(t)
		n := mustCreate(t, s, Note{Title: "Health care", Priority: PriorityLow, Status: StatusOpen}, "body")
		if err := os.Remove(s.Path(n)); err != nil {
			t.Fatal(err)
		}
		r, err := s.Sync()
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Missing) != 1 || r.Missing[0] != n.Path || count(t, conn, "notes") != 1 {
			t.Fatalf("Sync() = %+v with %d notes", r, count(t, conn, "notes"))
		}
	})

	t.Run("an external edit is indexed, logged and written back canonical", func(t *testing.T) {
		s, conn := newTestStore(t)
		n := mustCreate(t, s, Note{Title: "Health care", Priority: PriorityLow, Status: StatusOpen}, "body")
		rewrite(t, s.Path(n), "---\ntitle: Health care\nstatus: doing\ntarget: in 2 weeks\n---\nnew body\n")
		r, err := s.Sync()
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Reindexed) != 1 || r.Reindexed[0] != n.Path {
			t.Fatalf("Sync() = %+v", r)
		}
		got, err := s.Get(n.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != StatusDoing || got.Target != inDays(14) || got.EditCount != 1 || got.Problem != "" {
			t.Fatalf("Get() = %+v", got)
		}
		if data := canonical(t, s, got); !strings.Contains(data, "target: "+inDays(14)+" # in 2 weeks\n") {
			t.Errorf("file =\n%s", data)
		}
		trail, err := logs.List(conn, logs.Filter{Entity: "note", Action: "edited"})
		if err != nil || len(trail) != 1 {
			t.Fatalf("trail = %+v, %v", trail, err)
		}
		// Settled: a second pass has nothing to say.
		if r, _ = s.Sync(); !r.Empty() {
			t.Fatalf("second Sync() = %+v", r)
		}
	})

	t.Run("a broken file is a problem, not a missing one", func(t *testing.T) {
		s, _ := newTestStore(t)
		n := mustCreate(t, s, Note{Title: "Health care", Priority: PriorityLow, Status: StatusOpen}, "body")
		rewrite(t, s.Path(n), "---\ntitle: Health care\npriorty: high\n---\n")
		r, err := s.Sync()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(r.Problems[n.Path], `unknown key "priorty"`) {
			t.Fatalf("problems = %v", r.Problems)
		}
	})
}
