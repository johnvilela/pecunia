package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pecunia/internal/accounts"
	"pecunia/internal/db"
	"pecunia/internal/notes"
)

// runNotesIn points PECUNIA_DB at a database of this case's own — the notes
// directory follows it — captures what the command writes and returns both.
//
// The editor is swapped for a fake by fakeEditor, so new and edit run here;
// the delete confirmation and the pickers still block on a TTY and are
// covered through the store.
func runNotesIn(t *testing.T, dbPath string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("PECUNIA_DB", dbPath)
	t.Setenv("PECUNIA_NOTES", "")

	var buf bytes.Buffer
	old := out
	out = &buf
	t.Cleanup(func() { out = old })

	err := runNotes(args)
	return buf.String(), err
}

// fakeEditor stands in for the owner's editor: it rewrites the file it is
// handed with what fn returns, or leaves it alone when fn returns "". It
// reports how many times it was opened.
func fakeEditor(t *testing.T, fn func(path string) string) *int {
	t.Helper()
	calls := 0
	old := openEditor
	openEditor = func(path string, line int) error {
		calls++
		if content := fn(path); content != "" {
			return os.WriteFile(path, []byte(content), 0o600)
		}
		return nil
	}
	t.Cleanup(func() { openEditor = old })
	return &calls
}

func notesDir(dbPath string) string { return filepath.Join(filepath.Dir(dbPath), "notes") }

// openNotes opens the store the command would, for looking at what it did.
func openNotes(t *testing.T, dbPath string) *notes.Store {
	t.Helper()
	t.Setenv("PECUNIA_DB", dbPath)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return notes.NewStore(conn, notesDir(dbPath))
}

func seedAccount(t *testing.T, dbPath, code string) {
	t.Helper()
	t.Setenv("PECUNIA_DB", dbPath)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	a := accounts.Account{Code: code, Name: "Banco " + code, Color: "orange", Currency: "BRL"}
	if err := accounts.NewStore(conn).Create(&a); err != nil {
		t.Fatal(err)
	}
}

// quick creates a note through the command itself, no editor.
func quick(t *testing.T, dbPath string, args ...string) {
	t.Helper()
	if _, err := runNotesIn(t, dbPath, append([]string{"new", "--no-edit"}, args...)...); err != nil {
		t.Fatal(err)
	}
}

func TestNotesHelp(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"top level -h", []string{"-h"}, "priority that moves"},
		{"top level --help", []string{"--help"}, "priority that moves"},
		{"new -h", []string{"new", "-h"}, "Create a note"},
		{"n -h", []string{"n", "-h"}, "Create a note"},
		{"edit -h", []string{"edit", "-h"}, "Edit a note"},
		{"e --help", []string{"e", "--help"}, "Edit a note"},
		{"delete --help", []string{"delete", "--help"}, "Delete a note"},
		{"d -h", []string{"d", "-h"}, "Delete a note"},
		{"sync -h", []string{"sync", "-h"}, "Re-index"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nope", "unused.db")
			got, err := runNotesIn(t, path, tc.args...)
			if err != nil {
				t.Fatalf("help returned %v", err)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("help = %q; want it to contain %q", got, tc.want)
			}
		})
	}
}

func TestNotesList(t *testing.T) {
	seed := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "pecunia.db")
		seedAccount(t, path, "INTER")
		quick(t, path, "Get a better health care", "-p", "low", "-t", "in 5 days", "--tags", "health,insurance", "--account", "inter")
		quick(t, path, "Renegociar o cartão", "-p", "high", "--status", "doing", "--tags", "bank")
		quick(t, path, "Cancel the old plan", "-p", "critical", "--status", "done")
		quick(t, path, "Paid late", "-p", "medium", "-t", "01/01/2020")
		return path
	}

	t.Run("an empty database says how to start", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		got, err := runNotesIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "no notes yet") || !strings.Contains(got, "pecunia n n") {
			t.Fatalf("list = %q", got)
		}
	})

	t.Run("the table, highest score first", func(t *testing.T) {
		path := seed(t)
		got, err := runNotesIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"PRIORITY", "SCORE", "TITLE", "TARGET", "TAGS",
			"Get a better health care", "Renegociar o cartão", "Paid late", "#health", "overdue", "in 5d"} {
			if !strings.Contains(got, want) {
				t.Errorf("list lacks %q:\n%s", want, got)
			}
		}
		if strings.Contains(got, "Cancel the old plan") {
			t.Errorf("a done note is listed by default:\n%s", got)
		}
		// Overdue medium (70) above high (60) above low-due-soon (45).
		if strings.Index(got, "Paid late") > strings.Index(got, "Renegociar") ||
			strings.Index(got, "Renegociar") > strings.Index(got, "Get a better") {
			t.Errorf("order is wrong:\n%s", got)
		}
	})

	cases := []struct {
		name  string
		args  []string
		want  []string
		block []string
	}{
		{"--all", []string{"--all"}, []string{"Cancel the old plan", "Paid late"}, nil},
		{"--status done", []string{"--status", "done"}, []string{"Cancel the old plan"}, []string{"Paid late"}},
		{"--tag", []string{"--tag", "bank"}, []string{"Renegociar"}, []string{"health care"}},
		{"--search", []string{"--search", "CARTÃO"}, []string{"Renegociar"}, []string{"health care"}},
		{"--overdue", []string{"--overdue"}, []string{"Paid late"}, []string{"health care"}},
		{"--due phrase", []string{"--due", "next week"}, []string{"health care", "Paid late"}, []string{"Renegociar"}},
		{"--priority effective", []string{"--priority", "medium"}, []string{"health care"}, []string{"Renegociar", "Paid late"}},
		{"--min-score", []string{"--min-score", "60"}, []string{"Renegociar", "Paid late"}, []string{"health care"}},
		{"--account by code", []string{"--account", "inter"}, []string{"health care"}, []string{"Renegociar"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := seed(t)
			got, err := runNotesIn(t, path, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("%v lacks %q:\n%s", tc.args, want, got)
				}
			}
			for _, block := range tc.block {
				if strings.Contains(got, block) {
					t.Errorf("%v shows %q:\n%s", tc.args, block, got)
				}
			}
		})
	}

	t.Run("a filter that matches nothing says so", func(t *testing.T) {
		path := seed(t)
		got, err := runNotesIn(t, path, "--tag", "zebra")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "no notes match") {
			t.Fatalf("list = %q", got)
		}
	})

	errs := []struct {
		name string
		args []string
		want string
	}{
		{"an unknown flag", []string{"--bogus"}, "pecunia n -h"},
		{"a positional beside flags", []string{"--all", "3"}, "unexpected argument"},
		{"a status outside the set", []string{"--status", "paused"}, "not a status"},
		{"a priority outside the set", []string{"--priority", "urgent"}, "not a priority"},
		{"a due phrase that is not one", []string{"--due", "soon"}, "--due"},
		{"an account that is not there", []string{"--account", "NOPE1"}, `no account matching "NOPE1"`},
	}
	for _, tc := range errs {
		t.Run(tc.name, func(t *testing.T) {
			path := seed(t)
			_, err := runNotesIn(t, path, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v; want %q", err, tc.want)
			}
		})
	}
}

func TestNotesShow(t *testing.T) {
	t.Run("renders the note and counts the read", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		quick(t, path, "Health care", "-p", "low", "-t", "in 3 months", "--tags", "health")
		s := openNotes(t, path)
		n, err := s.Get(1)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Update(n, "# Why\n\nBecause the plan is bad."); err != nil {
			t.Fatal(err)
		}

		got, err := runNotesIn(t, path, "1")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Health care", "LOW", "score", "in 3 months", "#health", "Because the plan is bad.", "1-health-care.md"} {
			if !strings.Contains(got, want) {
				t.Errorf("show lacks %q:\n%s", want, got)
			}
		}
		if n, _ = s.Get(1); n.ReadCount != 1 {
			t.Errorf("read count = %d; want 1", n.ReadCount)
		}
	})

	t.Run("--path prints the file and counts nothing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		quick(t, path, "Health care")
		got, err := runNotesIn(t, path, "--path", "1")
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.Join(notesDir(path), "1-health-care.md")+"\n" {
			t.Fatalf("--path = %q", got)
		}
		if n, _ := openNotes(t, path).Get(1); n.ReadCount != 0 {
			t.Errorf("read count = %d; want 0", n.ReadCount)
		}
	})

	t.Run("an id that is not there", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		quick(t, path, "Health care")
		_, err := runNotesIn(t, path, "9")
		if err == nil || !strings.Contains(err.Error(), `no note matching "9"`) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("a word is not an id", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		quick(t, path, "Health care")
		_, err := runNotesIn(t, path, "health")
		if err == nil || !strings.Contains(err.Error(), "referenced by id") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestNotesNew(t *testing.T) {
	t.Run("--no-edit creates from the flags", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		seedAccount(t, path, "INTER")
		got, err := runNotesIn(t, path, "new", "-p", "low", "Get a better", "health care",
			"-t", "in 3 months", "--tags", "Health, insurance", "--account", "inter", "--no-edit")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "created note 1: Get a better health care") {
			t.Fatalf("output = %q", got)
		}
		n, err := openNotes(t, path).Get(1)
		if err != nil {
			t.Fatal(err)
		}
		if n.Priority != "low" || n.TargetPhrase != "in 3 months" || n.Target == "" ||
			strings.Join(n.Tags, ",") != "health,insurance" || strings.Join(n.Accounts, ",") != "INTER" {
			t.Fatalf("Get() = %+v", n)
		}
		data, err := os.ReadFile(filepath.Join(notesDir(path), n.Path))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "target: "+n.Target+" # in 3 months\n") {
			t.Errorf("file =\n%s", data)
		}
	})

	t.Run("--no-edit needs a title", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		_, err := runNotesIn(t, path, "new", "--no-edit")
		if err == nil || !strings.Contains(err.Error(), "title is required") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("the editor writes the note", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		calls := fakeEditor(t, func(p string) string {
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(filepath.Base(p), "draft-") || !strings.Contains(string(data), "title: Health care\n") {
				t.Errorf("the editor got %s:\n%s", p, data)
			}
			return strings.Replace(string(data), "priority: medium", "priority: high", 1) + "the body\n"
		})
		got, err := runNotesIn(t, path, "n", "Health care")
		if err != nil {
			t.Fatal(err)
		}
		if *calls != 1 || !strings.Contains(got, "created note 1: Health care") {
			t.Fatalf("editor opened %d times, output %q", *calls, got)
		}
		n, err := openNotes(t, path).Get(1)
		if err != nil || n.Priority != "high" {
			t.Fatalf("Get() = %+v, %v", n, err)
		}
		entries, _ := os.ReadDir(notesDir(path))
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		if strings.Join(names, ",") != "1-health-care.md" {
			t.Errorf("dir has %v; want only the note", names)
		}
		data, _ := os.ReadFile(filepath.Join(notesDir(path), "1-health-care.md"))
		if !strings.HasSuffix(string(data), "---\n\nthe body\n") || strings.Contains(string(data), "# low | medium") {
			t.Errorf("file is not canonical:\n%s", data)
		}
	})

	t.Run("an editor that wrote nothing creates nothing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		fakeEditor(t, func(string) string { return "" })
		got, err := runNotesIn(t, path, "new", "Health care")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "nothing written") {
			t.Fatalf("output = %q", got)
		}
		if all, _ := openNotes(t, path).List(notes.Filter{All: true}); len(all) != 0 {
			t.Errorf("%d notes created", len(all))
		}
		if entries, _ := os.ReadDir(notesDir(path)); len(entries) != 0 {
			t.Errorf("the draft was left behind")
		}
	})

	errs := []struct {
		name string
		args []string
		want string
	}{
		{"a target that is not one", []string{"new", "x", "-t", "soon"}, "--target"},
		{"an account that is not there", []string{"new", "x", "--account", "NOPE1"}, `no account matching "NOPE1"`},
		{"a priority outside the set", []string{"new", "x", "-p", "urgent"}, "not a priority"},
		{"a status outside the set", []string{"new", "x", "--status", "paused"}, "not a status"},
		{"a goal that is not a number", []string{"new", "x", "--goal", "laptop"}, "not a goal id"},
	}
	for _, tc := range errs {
		t.Run(tc.name+" fails before the editor opens", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pecunia.db")
			calls := fakeEditor(t, func(string) string { return "" })
			_, err := runNotesIn(t, path, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v; want %q", err, tc.want)
			}
			if *calls != 0 {
				t.Fatalf("the editor opened %d times", *calls)
			}
		})
	}
}

func TestNotesEdit(t *testing.T) {
	t.Run("an unchanged file counts as a read", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		quick(t, path, "Health care")
		fakeEditor(t, func(string) string { return "" })
		got, err := runNotesIn(t, path, "e", "1")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "no changes to note 1") {
			t.Fatalf("output = %q", got)
		}
		n, _ := openNotes(t, path).Get(1)
		if n.ReadCount != 1 || n.EditCount != 0 {
			t.Errorf("reads %d edits %d; want 1 and 0", n.ReadCount, n.EditCount)
		}
	})

	t.Run("a changed file is saved canonical and counted", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		quick(t, path, "Health care")
		var opened string
		fakeEditor(t, func(p string) string {
			opened = p
			return "---\ntitle: Better health care\ntarget: in 2 weeks\n---\nnew body\n"
		})
		got, err := runNotesIn(t, path, "edit", "1")
		if err != nil {
			t.Fatal(err)
		}
		if opened != filepath.Join(notesDir(path), "1-health-care.md") {
			t.Errorf("the editor opened %s", opened)
		}
		if !strings.Contains(got, "updated note 1: Better health care") {
			t.Fatalf("output = %q", got)
		}
		n, _ := openNotes(t, path).Get(1)
		if n.Title != "Better health care" || n.EditCount != 1 || n.TargetPhrase != "in 2 weeks" || n.Path != "1-health-care.md" {
			t.Errorf("Get() = %+v", n)
		}
		data, _ := os.ReadFile(opened)
		if !strings.Contains(string(data), "target: "+n.Target+" # in 2 weeks\n") || !strings.Contains(string(data), "priority: medium\n") {
			t.Errorf("file =\n%s", data)
		}
	})

	t.Run("a note that is not there", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		quick(t, path, "Health care")
		_, err := runNotesIn(t, path, "e", "9")
		if err == nil || !strings.Contains(err.Error(), `no note matching "9"`) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("a note whose file is gone", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		quick(t, path, "Health care")
		if err := os.Remove(filepath.Join(notesDir(path), "1-health-care.md")); err != nil {
			t.Fatal(err)
		}
		calls := fakeEditor(t, func(string) string { return "" })
		_, err := runNotesIn(t, path, "e", "1")
		if err == nil || !strings.Contains(err.Error(), "1-health-care.md") || !strings.Contains(err.Error(), "pecunia n sync") {
			t.Fatalf("err = %v", err)
		}
		if *calls != 0 {
			t.Error("the editor opened on a missing file")
		}
	})
}

func TestNotesDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pecunia.db")
	quick(t, path, "Health care")
	_, err := runNotesIn(t, path, "d", "9")
	if err == nil || !strings.Contains(err.Error(), `no note matching "9"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestNotesSync(t *testing.T) {
	t.Run("nothing to do", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		got, err := runNotesIn(t, path, "sync")
		if err != nil || !strings.Contains(got, "nothing to do") {
			t.Fatalf("sync = %q, %v", got, err)
		}
	})

	t.Run("reports what it did", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pecunia.db")
		quick(t, path, "Health care")
		dir := notesDir(path)
		if err := os.WriteFile(filepath.Join(dir, "ideia.md"), []byte("---\ntitle: Trocar de banco\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bad.md"), []byte("nope\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(dir, "1-health-care.md")); err != nil {
			t.Fatal(err)
		}
		got, err := runNotesIn(t, path, "sync")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"adopted: ideia.md → 2-trocar-de-banco.md", "missing: 1-health-care.md", "pecunia n d 1", "problem: bad.md: no front matter"} {
			if !strings.Contains(got, want) {
				t.Errorf("sync lacks %q:\n%s", want, got)
			}
		}
	})
}

func TestNotesWithoutADatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", "pecunia.db")
	if err := os.WriteFile(filepath.Dir(path), []byte("in the way"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runNotesIn(t, path); err == nil {
		t.Fatal("listing without a database succeeded")
	}
}
