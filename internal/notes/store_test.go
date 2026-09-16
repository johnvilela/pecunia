package notes

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pecunia/internal/db"
	"pecunia/internal/logs"
)

func newTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	conn := newTestDB(t)
	return NewStore(conn, filepath.Join(t.TempDir(), "notes")), conn
}

func mustCreate(t *testing.T, s *Store, n Note, body string) Note {
	t.Helper()
	if err := s.Create(&n, body); err != nil {
		t.Fatalf("create %s: %v", n.Title, err)
	}
	return n
}

// account, card and goal put one row each in the database for a note to link
// to. Raw SQL keeps the fixtures short.
func account(t *testing.T, conn *sql.DB, code string) int64 {
	t.Helper()
	return insertRow(t, conn,
		`INSERT INTO accounts (code, name, color, balance, currency) VALUES (?, ?, 'orange', 0, 'BRL')`, code, code)
}

func card(t *testing.T, conn *sql.DB, code string) int64 {
	t.Helper()
	return insertRow(t, conn,
		`INSERT INTO credit_cards (code, name, color, currency, credit_limit, balance, closing_day, due_day)
		 VALUES (?, ?, 'violet', 'BRL', 500000, 0, 15, 22)`, code, code)
}

func goal(t *testing.T, conn *sql.DB, name string) int64 {
	t.Helper()
	return insertRow(t, conn,
		`INSERT INTO goals (name, target, currency, kind) VALUES (?, 100000, 'BRL', 'saving')`, name)
}

// file writes one transaction daysAgo days back, on an account or a card, naming
// a goal when goalID is not zero.
func file(t *testing.T, conn *sql.DB, accountID, cardID, goalID int64, daysAgo int) {
	t.Helper()
	date := time.Now().AddDate(0, 0, -daysAgo).Format("2006-01-02")
	var acc, crd, gl any
	if accountID != 0 {
		acc = accountID
	}
	if cardID != 0 {
		crd = cardID
	}
	if goalID != 0 {
		gl = goalID
	}
	insertRow(t, conn,
		`INSERT INTO transactions (title, account_id, card_id, value, kind, date, goal_id)
		 VALUES ('Something', ?, ?, 1000, 'outcome', ?, ?)`, acc, crd, date, gl)
}

func insertRow(t *testing.T, conn *sql.DB, query string, args ...any) int64 {
	t.Helper()
	res, err := conn.Exec(query, args...)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func count(t *testing.T, conn *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// withDevDB swaps the ldflags-injected dev default for one case only.
func withDevDB(t *testing.T, path string) {
	t.Helper()
	old := db.DevDB
	db.DevDB = path
	t.Cleanup(func() { db.DevDB = old })
}

func inDays(days int) string { return time.Now().AddDate(0, 0, days).Format("2006-01-02") }

func TestCreate(t *testing.T) {
	t.Run("writes the file and the row, canonical", func(t *testing.T) {
		s, conn := newTestStore(t)
		account(t, conn, "INTER")
		n := Note{Title: "Get a better health care", Priority: PriorityLow, Status: StatusOpen,
			Target: inDays(91), TargetPhrase: "in 3 months", Tags: []string{"insurance", "health"},
			Accounts: []string{"inter"}}
		n = mustCreate(t, s, n, "# Why\n\nBecause.\n\n")

		if n.ID != 1 || n.Path != "1-get-a-better-health-care.md" {
			t.Fatalf("created %+v", n)
		}
		data, err := os.ReadFile(filepath.Join(s.dir, n.Path))
		if err != nil {
			t.Fatal(err)
		}
		want := "---\ntitle: Get a better health care\npriority: low\nstatus: open\ntarget: " + inDays(91) +
			" # in 3 months\ntags: [health, insurance]\naccounts: [INTER]\ncards: []\ngoals: []\n---\n\n# Why\n\nBecause.\n"
		if string(data) != want {
			t.Fatalf("file =\n%s\nwant\n%s", data, want)
		}
		got, err := s.Get(n.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Title != n.Title || got.Priority != PriorityLow || got.Target != inDays(91) ||
			got.TargetPhrase != "in 3 months" || strings.Join(got.Tags, ",") != "health,insurance" ||
			strings.Join(got.Accounts, ",") != "INTER" || got.Mtime == 0 || got.BodyHash == "" {
			t.Fatalf("Get() = %+v", got)
		}
		if got.Score != 23 || got.Level != PriorityLow {
			t.Errorf("score = %d %s; want 23 low (base 20 + target 3)", got.Score, got.Level)
		}
		body, err := s.Body(got)
		if err != nil || body != "# Why\n\nBecause." {
			t.Errorf("Body() = %q, %v", body, err)
		}
		trail, err := logs.List(conn, logs.Filter{Entity: "note"})
		if err != nil {
			t.Fatal(err)
		}
		if len(trail) != 1 || trail[0].Action != "created" || trail[0].EntityID != n.ID {
			t.Errorf("trail = %+v; want one created row", trail)
		}
	})

	t.Run("every kind of link, by code or id", func(t *testing.T) {
		s, conn := newTestStore(t)
		account(t, conn, "INTER")
		card(t, conn, "NUCRD")
		gid := goal(t, conn, "Laptop")
		n := mustCreate(t, s, Note{Title: "Links", Priority: PriorityLow, Status: StatusOpen,
			Accounts: []string{"INTER"}, Cards: []string{"NUCRD"}, Goals: []int64{gid}}, "")
		got, err := s.Get(n.ID)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(got.Accounts, ",") != "INTER" || strings.Join(got.Cards, ",") != "NUCRD" ||
			len(got.Goals) != 1 || got.Goals[0] != gid {
			t.Fatalf("links = %v / %v / %v", got.Accounts, got.Cards, got.Goals)
		}
		if count(t, conn, "note_links") != 3 {
			t.Fatalf("%d links; want 3", count(t, conn, "note_links"))
		}
	})

	t.Run("an unknown reference is refused before anything is written", func(t *testing.T) {
		s, conn := newTestStore(t)
		cases := []struct {
			name string
			n    Note
			want string
		}{
			{"account", Note{Title: "x", Priority: PriorityLow, Status: StatusOpen, Accounts: []string{"NOPE1"}}, `no account matching "NOPE1"`},
			{"card", Note{Title: "x", Priority: PriorityLow, Status: StatusOpen, Cards: []string{"NOPE1"}}, `no credit card matching "NOPE1"`},
			{"goal", Note{Title: "x", Priority: PriorityLow, Status: StatusOpen, Goals: []int64{9}}, `no goal matching "9"`},
		}
		for _, tc := range cases {
			err := s.Create(&tc.n, "")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s: Create() = %v; want %q", tc.name, err, tc.want)
			}
		}
		if count(t, conn, "notes") != 0 {
			t.Fatalf("%d notes written; want none", count(t, conn, "notes"))
		}
		if _, err := os.Stat(s.dir); !os.IsNotExist(err) {
			t.Fatalf("the notes dir exists (%v); want nothing written", err)
		}
	})

	t.Run("an invalid note is refused", func(t *testing.T) {
		s, _ := newTestStore(t)
		n := Note{Title: "", Priority: PriorityLow, Status: StatusOpen}
		if err := s.Create(&n, ""); err == nil || !strings.Contains(err.Error(), "title is required") {
			t.Fatalf("Create() = %v", err)
		}
	})

	t.Run("a file that cannot be written leaves no row", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root can write anywhere")
		}
		s, conn := newTestStore(t)
		if err := os.MkdirAll(s.dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(s.dir, 0o700) })
		n := Note{Title: "x", Priority: PriorityLow, Status: StatusOpen}
		if err := s.Create(&n, ""); err == nil {
			t.Fatal("Create() succeeded into a read-only dir")
		}
		if count(t, conn, "notes") != 0 {
			t.Fatalf("%d notes written; want the row rolled back", count(t, conn, "notes"))
		}
	})
}

func TestGet(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.Get(42); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(42) = %v; want ErrNotFound", err)
	}
}

func TestList(t *testing.T) {
	// seed puts four notes in: a low one due soon about INTER, a high one being
	// worked on, and one each done and dropped.
	seed := func(t *testing.T) (*Store, *sql.DB) {
		t.Helper()
		s, conn := newTestStore(t)
		account(t, conn, "INTER")
		mustCreate(t, s, Note{Title: "Get a better health care", Priority: PriorityLow, Status: StatusOpen,
			Target: inDays(5), Tags: []string{"health"}, Accounts: []string{"INTER"}}, "compare the insurance plans")
		mustCreate(t, s, Note{Title: "Renegociar o cartão", Priority: PriorityHigh, Status: StatusDoing,
			Tags: []string{"bank"}}, "ligar para o banco")
		mustCreate(t, s, Note{Title: "Cancel the old plan", Priority: PriorityCritical, Status: StatusDone}, "done")
		mustCreate(t, s, Note{Title: "Move to another bank", Priority: PriorityMedium, Status: StatusDropped}, "")
		return s, conn
	}
	titles := func(ns []Note) string {
		var out []string
		for _, n := range ns {
			out = append(out, n.Title)
		}
		return strings.Join(out, " | ")
	}

	cases := []struct {
		name   string
		filter Filter
		want   string
	}{
		{"the default is the open ones, highest score first", Filter{}, "Renegociar o cartão | Get a better health care"},
		{"--all adds done and dropped", Filter{All: true}, "Renegociar o cartão | Get a better health care | Cancel the old plan | Move to another bank"},
		{"--status done", Filter{Status: StatusDone}, "Cancel the old plan"},
		{"--tag", Filter{Tag: " Health "}, "Get a better health care"},
		{"--search in the body", Filter{Search: "INSURANCE"}, "Get a better health care"},
		{"--search in the title", Filter{Search: "renegociar"}, "Renegociar o cartão"},
		{"--search finds nothing", Filter{Search: "zebra"}, ""},
		{"--due", Filter{DueBy: inDays(10)}, "Get a better health care"},
		{"--due too early", Filter{DueBy: inDays(2)}, ""},
		{"--overdue", Filter{Overdue: true}, ""},
		{"--priority is the effective level", Filter{Priority: PriorityHigh}, "Renegociar o cartão"},
		{"--priority medium is the low note that climbed", Filter{Priority: PriorityMedium}, "Get a better health care"},
		{"--min-score", Filter{MinScore: 50}, "Renegociar o cartão"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := seed(t)
			got, err := s.List(tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			if titles(got) != tc.want {
				t.Fatalf("List(%+v) = %q; want %q", tc.filter, titles(got), tc.want)
			}
		})
	}

	t.Run("--account", func(t *testing.T) {
		s, conn := seed(t)
		var id int64
		if err := conn.QueryRow(`SELECT id FROM accounts WHERE code = 'INTER'`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		got, err := s.List(Filter{Account: id})
		if err != nil {
			t.Fatal(err)
		}
		if titles(got) != "Get a better health care" {
			t.Fatalf("List() = %q", titles(got))
		}
		if got, _ = s.List(Filter{Account: id + 99}); len(got) != 0 {
			t.Fatalf("List() for an unlinked account = %q", titles(got))
		}
	})

	t.Run("scores and levels come back with the rows", func(t *testing.T) {
		s, _ := seed(t)
		got, err := s.List(Filter{})
		if err != nil {
			t.Fatal(err)
		}
		if got[0].Score != 60 || got[0].Level != PriorityHigh || got[1].Score != 45 || got[1].Level != PriorityMedium {
			t.Fatalf("scores = %d %s, %d %s", got[0].Score, got[0].Level, got[1].Score, got[1].Level)
		}
	})

	t.Run("an overdue note sorts above an equal score with no target", func(t *testing.T) {
		s, _ := newTestStore(t)
		mustCreate(t, s, Note{Title: "No target", Priority: PriorityHigh, Status: StatusOpen}, "")
		mustCreate(t, s, Note{Title: "Overdue", Priority: PriorityMedium, Status: StatusOpen, Target: inDays(-2)}, "")
		mustCreate(t, s, Note{Title: "Same score, no target", Priority: PriorityHigh, Status: StatusOpen}, "")
		got, err := s.List(Filter{})
		if err != nil {
			t.Fatal(err)
		}
		// Overdue is 40+30 = 70; the two high notes are 60 each, ordered by id.
		if titles(got) != "Overdue | No target | Same score, no target" {
			t.Fatalf("List() = %q", titles(got))
		}
		if got, _ = s.List(Filter{Overdue: true}); titles(got) != "Overdue" {
			t.Fatalf("List(overdue) = %q", titles(got))
		}
	})

	t.Run("an empty database lists nothing", func(t *testing.T) {
		s, _ := newTestStore(t)
		got, err := s.List(Filter{})
		if err != nil || len(got) != 0 {
			t.Fatalf("List() = %v, %v", got, err)
		}
	})
}

func TestUpdate(t *testing.T) {
	t.Run("rewrites the file in place, counts the edit and logs what moved", func(t *testing.T) {
		s, conn := newTestStore(t)
		account(t, conn, "INTER")
		n := mustCreate(t, s, Note{Title: "Health care", Priority: PriorityLow, Status: StatusOpen,
			Tags: []string{"health"}}, "old body")

		n.Title = "Better health care"
		n.Priority = PriorityHigh
		n.Tags = []string{"health", "urgent"}
		n.Accounts = []string{"INTER"}
		if err := s.Update(n, "new body"); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(n.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Path != "1-health-care.md" {
			t.Errorf("path = %q; want the original name kept", got.Path)
		}
		if got.Title != "Better health care" || got.Priority != PriorityHigh || got.EditCount != 1 ||
			strings.Join(got.Tags, ",") != "health,urgent" || strings.Join(got.Accounts, ",") != "INTER" {
			t.Errorf("Get() = %+v", got)
		}
		data, err := os.ReadFile(s.Path(got))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "title: Better health care\n") || !strings.HasSuffix(string(data), "\n\nnew body\n") {
			t.Errorf("file =\n%s", data)
		}
		trail, err := logs.List(conn, logs.Filter{Entity: "note", Action: "edited"})
		if err != nil {
			t.Fatal(err)
		}
		if len(trail) != 1 {
			t.Fatalf("trail = %+v; want one edited row", trail)
		}
		for _, want := range []string{`"title"`, `"priority"`, `"tags"`, `"accounts"`, `"body"`} {
			if !strings.Contains(trail[0].Changes, want) {
				t.Errorf("changes = %s; want %s in it", trail[0].Changes, want)
			}
		}
		if strings.Contains(trail[0].Changes, `"status"`) {
			t.Errorf("changes = %s; status did not move", trail[0].Changes)
		}
	})

	t.Run("a missing note", func(t *testing.T) {
		s, _ := newTestStore(t)
		n := Note{ID: 9, Title: "x", Priority: PriorityLow, Status: StatusOpen}
		if err := s.Update(n, ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update() = %v; want ErrNotFound", err)
		}
	})
}

func TestDelete(t *testing.T) {
	t.Run("takes the row, its tags and its file", func(t *testing.T) {
		s, conn := newTestStore(t)
		n := mustCreate(t, s, Note{Title: "Health care", Priority: PriorityLow, Status: StatusOpen,
			Tags: []string{"health"}}, "body")
		if err := s.Delete(n.ID); err != nil {
			t.Fatal(err)
		}
		if count(t, conn, "notes") != 0 || count(t, conn, "note_tags") != 0 {
			t.Fatal("the row or its tags survived")
		}
		if _, err := os.Stat(s.Path(n)); !os.IsNotExist(err) {
			t.Fatalf("the file survived: %v", err)
		}
		trail, err := logs.List(conn, logs.Filter{Entity: "note", Action: "deleted"})
		if err != nil || len(trail) != 1 {
			t.Fatalf("trail = %+v, %v; want one deleted row", trail, err)
		}
	})

	t.Run("a note whose file is already gone", func(t *testing.T) {
		s, _ := newTestStore(t)
		n := mustCreate(t, s, Note{Title: "Health care", Priority: PriorityLow, Status: StatusOpen}, "")
		if err := os.Remove(s.Path(n)); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete(n.ID); err != nil {
			t.Fatalf("Delete() = %v; want the missing file not to matter", err)
		}
	})

	t.Run("a missing note", func(t *testing.T) {
		s, _ := newTestStore(t)
		if err := s.Delete(9); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Delete() = %v; want ErrNotFound", err)
		}
	})
}

func TestTouch(t *testing.T) {
	s, conn := newTestStore(t)
	n := mustCreate(t, s, Note{Title: "Health care", Priority: PriorityLow, Status: StatusOpen}, "")
	for i := 0; i < 3; i++ {
		if err := s.Touch(n.ID); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Get(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReadCount != 3 || got.LastReadAt == "" || got.EditCount != 0 {
		t.Fatalf("Get() = reads %d, last %q, edits %d", got.ReadCount, got.LastReadAt, got.EditCount)
	}
	if got.Score != 23 {
		t.Errorf("score = %d; want 23 (base 20 + 3 reads)", got.Score)
	}
	if err := s.Touch(9); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Touch(9) = %v; want ErrNotFound", err)
	}
	if n := count(t, conn, "logs"); n != 1 {
		t.Errorf("%d log rows; want reads not to log", n)
	}
}

func TestActivity(t *testing.T) {
	t.Run("counts distinct transactions on every linked thing, per window", func(t *testing.T) {
		s, conn := newTestStore(t)
		acc := account(t, conn, "INTER")
		crd := card(t, conn, "NUCRD")
		gl := goal(t, conn, "Laptop")
		other := account(t, conn, "OTHER")
		file(t, conn, acc, 0, 0, 3)
		file(t, conn, acc, 0, gl, 10) // on the account and the goal: counted once
		file(t, conn, acc, 0, 0, 60)
		file(t, conn, acc, 0, 0, 120) // too old
		file(t, conn, 0, crd, 0, 5)
		file(t, conn, other, 0, gl, 40) // reaches the note only through the goal
		file(t, conn, other, 0, 0, 1)   // not linked at all

		n := mustCreate(t, s, Note{Title: "Linked", Priority: PriorityLow, Status: StatusOpen,
			Accounts: []string{"INTER"}, Cards: []string{"NUCRD"}, Goals: []int64{gl}}, "")
		m := mustCreate(t, s, Note{Title: "Unlinked", Priority: PriorityLow, Status: StatusOpen}, "")

		got, err := s.Activity([]int64{n.ID, m.ID}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if got[n.ID] != (Activity{Tx30: 3, Tx90: 5}) {
			t.Errorf("linked = %+v; want 3 in 30 days, 5 in 90", got[n.ID])
		}
		if _, ok := got[m.ID]; ok {
			t.Errorf("unlinked = %+v; want no entry", got[m.ID])
		}
	})

	t.Run("no ids is no query", func(t *testing.T) {
		s, _ := newTestStore(t)
		got, err := s.Activity(nil, time.Now())
		if err != nil || len(got) != 0 {
			t.Fatalf("Activity(nil) = %v, %v", got, err)
		}
	})
}

func TestAllTags(t *testing.T) {
	s, _ := newTestStore(t)
	mustCreate(t, s, Note{Title: "a", Priority: PriorityLow, Status: StatusOpen, Tags: []string{"health", "bank"}}, "")
	mustCreate(t, s, Note{Title: "b", Priority: PriorityLow, Status: StatusOpen, Tags: []string{"bank"}}, "")
	got, err := s.AllTags()
	if err != nil || strings.Join(got, ",") != "bank,health" {
		t.Fatalf("AllTags() = %v, %v", got, err)
	}
}

func TestDir(t *testing.T) {
	t.Run("PECUNIA_NOTES wins", func(t *testing.T) {
		t.Setenv("PECUNIA_DB", "/tmp/x/pecunia.db")
		t.Setenv("PECUNIA_NOTES", "/tmp/elsewhere")
		if got, _ := Dir(); got != "/tmp/elsewhere" {
			t.Fatalf("Dir() = %q", got)
		}
	})

	t.Run("beside the database by default", func(t *testing.T) {
		t.Setenv("PECUNIA_DB", "/tmp/x/pecunia.db")
		t.Setenv("PECUNIA_NOTES", "")
		if got, _ := Dir(); got != "/tmp/x/notes" {
			t.Fatalf("Dir() = %q", got)
		}
	})

	t.Run("a dev build keeps its notes beside its database and ignores the environment", func(t *testing.T) {
		t.Setenv("PECUNIA_NOTES", "/tmp/elsewhere")
		withDevDB(t, "/repo/pecunia.dev.db")
		if got, _ := Dir(); got != "/repo/pecunia.dev.notes" {
			t.Fatalf("Dir() = %q", got)
		}
	})
}
