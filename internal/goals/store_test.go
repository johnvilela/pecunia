package goals

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	conn := newTestDB(t)
	return NewStore(conn), conn
}

func mustCreate(t *testing.T, s *Store, g Goal) Goal {
	t.Helper()
	if err := s.Create(&g); err != nil {
		t.Fatalf("create %s: %v", g.Name, err)
	}
	return g
}

// account puts one account in the database to file the linked transactions
// against. Raw SQL, because this package cannot import the one that owns them.
func account(t *testing.T, conn *sql.DB, code, currency string) int64 {
	t.Helper()
	res, err := conn.Exec(
		`INSERT INTO accounts (code, name, color, balance, currency) VALUES (?, ?, 'orange', 0, ?)`,
		code, code, currency)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// file writes one transaction against the account, naming a goal when goalID is
// not zero.
func file(t *testing.T, conn *sql.DB, accountID, goalID int64, kind string, value int64) {
	t.Helper()
	var goal any
	if goalID != 0 {
		goal = goalID
	}
	if _, err := conn.Exec(
		`INSERT INTO transactions (title, account_id, value, kind, date, goal_id)
		 VALUES ('Something', ?, ?, ?, '2026-08-08', ?)`,
		accountID, value, kind, goal); err != nil {
		t.Fatal(err)
	}
}

func TestCreateAndGet(t *testing.T) {
	t.Run("a goal comes back as it was written", func(t *testing.T) {
		s, _ := newTestStore(t)
		g := mustCreate(t, s, laptop())
		if g.ID == 0 {
			t.Fatal("Create left the id at zero")
		}

		got, err := s.Get(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "New laptop" || got.Target != 500000 ||
			got.Currency != "BRL" || got.Kind != KindSaving {
			t.Fatalf("Get = %+v; want the goal that was written", got)
		}
		if got.Description != "money for the new machine" {
			t.Errorf("description = %q; want it kept", got.Description)
		}
	})

	t.Run("a goal with nothing against it is at zero", func(t *testing.T) {
		s, _ := newTestStore(t)
		g := mustCreate(t, s, laptop())
		got, err := s.Get(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Net != 0 || got.Progress() != 0 {
			t.Fatalf("Net/Progress = %d/%d; want both zero", got.Net, got.Progress())
		}
	})

	t.Run("an unknown id is not found", func(t *testing.T) {
		s, _ := newTestStore(t)
		if _, err := s.Get(404); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get(404) = %v; want ErrNotFound", err)
		}
	})

	t.Run("a broken goal never reaches the table", func(t *testing.T) {
		s, _ := newTestStore(t)
		g := laptop()
		g.Target = 0
		if err := s.Create(&g); err == nil {
			t.Fatal("Create with a zero target succeeded; want Validate to refuse it")
		}
		all, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 0 {
			t.Fatalf("List = %d goals; want none written", len(all))
		}
	})
}

func TestProgressFromTransactions(t *testing.T) {
	cases := []struct {
		name string
		kind string // the goal's kind
		// each pair is one transaction: its kind and its value
		filed []struct {
			kind  string
			value int64
		}
		want int64
	}{
		{"an income climbs a saving goal", KindSaving,
			[]struct {
				kind  string
				value int64
			}{{"income", 30000}}, 30000},
		{"an outcome lowers a saving goal", KindSaving,
			[]struct {
				kind  string
				value int64
			}{{"income", 30000}, {"outcome", 10000}}, 20000},
		{"an outcome climbs a paying goal", KindPaying,
			[]struct {
				kind  string
				value int64
			}{{"outcome", 30000}}, 30000},
		{"an income lowers a paying goal", KindPaying,
			[]struct {
				kind  string
				value int64
			}{{"outcome", 30000}, {"income", 10000}}, 20000},
		{"several transactions add up", KindSaving,
			[]struct {
				kind  string
				value int64
			}{{"income", 10000}, {"income", 20000}, {"income", 5000}}, 35000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, conn := newTestStore(t)
			g := laptop()
			g.Kind = tc.kind
			g = mustCreate(t, s, g)
			acc := account(t, conn, "INTER", "BRL")
			for _, f := range tc.filed {
				file(t, conn, acc, g.ID, f.kind, f.value)
			}

			got, err := s.Get(g.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Progress() != tc.want {
				t.Fatalf("Progress() = %d; want %d", got.Progress(), tc.want)
			}
		})
	}

	t.Run("a transaction naming no goal counts for nothing", func(t *testing.T) {
		s, conn := newTestStore(t)
		g := mustCreate(t, s, laptop())
		acc := account(t, conn, "INTER", "BRL")
		file(t, conn, acc, 0, "income", 90000)

		got, err := s.Get(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Progress() != 0 {
			t.Fatalf("Progress() = %d; want 0 — that transaction names no goal", got.Progress())
		}
	})

	t.Run("another goal's transactions count for nothing", func(t *testing.T) {
		s, conn := newTestStore(t)
		mine := mustCreate(t, s, laptop())
		other := laptop()
		other.Name = "Holiday"
		other = mustCreate(t, s, other)
		acc := account(t, conn, "INTER", "BRL")
		file(t, conn, acc, other.ID, "income", 90000)

		got, err := s.Get(mine.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Progress() != 0 {
			t.Fatalf("Progress() = %d; want 0", got.Progress())
		}
	})
}

func TestList(t *testing.T) {
	t.Run("goals come back ordered by name", func(t *testing.T) {
		s, _ := newTestStore(t)
		for _, name := range []string{"Zebra fund", "Apartment", "Motorbike"} {
			g := laptop()
			g.Name = name
			mustCreate(t, s, g)
		}
		all, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, g := range all {
			names = append(names, g.Name)
		}
		want := []string{"Apartment", "Motorbike", "Zebra fund"}
		if strings.Join(names, ",") != strings.Join(want, ",") {
			t.Fatalf("List = %v; want %v", names, want)
		}
	})

	t.Run("every goal carries its own progress", func(t *testing.T) {
		s, conn := newTestStore(t)
		acc := account(t, conn, "INTER", "BRL")

		one := laptop()
		one.Name = "Apartment"
		one = mustCreate(t, s, one)
		two := laptop()
		two.Name = "Motorbike"
		two.Kind = KindPaying
		two = mustCreate(t, s, two)
		three := laptop()
		three.Name = "Zebra fund"
		three = mustCreate(t, s, three)

		file(t, conn, acc, one.ID, "income", 10000)
		file(t, conn, acc, two.ID, "outcome", 25000)
		// three gets nothing.

		all, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]int64{"Apartment": 10000, "Motorbike": 25000, "Zebra fund": 0}
		for _, g := range all {
			if got := g.Progress(); got != want[g.Name] {
				t.Errorf("%s Progress() = %d; want %d", g.Name, got, want[g.Name])
			}
		}
	})

	t.Run("an empty database lists nothing", func(t *testing.T) {
		s, _ := newTestStore(t)
		all, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 0 {
			t.Fatalf("List = %d goals; want none", len(all))
		}
	})
}

func TestUpdate(t *testing.T) {
	t.Run("the fields are rewritten and updated_at moves", func(t *testing.T) {
		s, conn := newTestStore(t)
		g := mustCreate(t, s, laptop())
		if _, err := conn.Exec(
			`UPDATE goals SET updated_at = '2000-01-01 00:00:00' WHERE id = ?`, g.ID); err != nil {
			t.Fatal(err)
		}

		g.Name = "Better laptop"
		g.Target = 700000
		g.Kind = KindPaying
		if err := s.Update(g, ""); err != nil {
			t.Fatal(err)
		}

		got, err := s.Get(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "Better laptop" || got.Target != 700000 || got.Kind != KindPaying {
			t.Fatalf("Get after Update = %+v; want the new values", got)
		}
		if got.UpdatedAt == "2000-01-01 00:00:00" {
			t.Error("updated_at did not move")
		}
	})

	t.Run("an unknown id is not found", func(t *testing.T) {
		s, _ := newTestStore(t)
		g := laptop()
		g.ID = 404
		if err := s.Update(g, ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update(404) = %v; want ErrNotFound", err)
		}
	})

	t.Run("a broken goal is refused", func(t *testing.T) {
		s, _ := newTestStore(t)
		g := mustCreate(t, s, laptop())
		g.Name = ""
		if err := s.Update(g, ""); err == nil {
			t.Fatal("Update with a blank name succeeded")
		}
	})

	t.Run("the currency may change while nothing is linked", func(t *testing.T) {
		s, _ := newTestStore(t)
		g := mustCreate(t, s, laptop())
		g.Currency = "USD"
		if err := s.Update(g, ""); err != nil {
			t.Fatalf("Update = %v; want the currency to change freely", err)
		}
	})

	t.Run("the currency may not change once a transaction is linked", func(t *testing.T) {
		s, conn := newTestStore(t)
		g := mustCreate(t, s, laptop())
		acc := account(t, conn, "INTER", "BRL")
		file(t, conn, acc, g.ID, "income", 10000)

		g.Currency = "BTC"
		err := s.Update(g, "")
		if err == nil || !strings.Contains(err.Error(), "linked") {
			t.Fatalf("Update = %v; want it refused because a transaction is linked", err)
		}

		got, err := s.Get(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Currency != "BRL" {
			t.Errorf("currency = %q; want it left at BRL", got.Currency)
		}
	})

	t.Run("everything else still changes while a transaction is linked", func(t *testing.T) {
		s, conn := newTestStore(t)
		g := mustCreate(t, s, laptop())
		acc := account(t, conn, "INTER", "BRL")
		file(t, conn, acc, g.ID, "income", 10000)

		g.Name = "Better laptop"
		g.Target = 700000
		if err := s.Update(g, ""); err != nil {
			t.Fatalf("Update = %v; want everything but the currency to move", err)
		}
	})
}

func TestDelete(t *testing.T) {
	t.Run("the goal is gone", func(t *testing.T) {
		s, _ := newTestStore(t)
		g := mustCreate(t, s, laptop())
		if err := s.Delete(g.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Get(g.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get after Delete = %v; want ErrNotFound", err)
		}
	})

	t.Run("an unknown id is not found", func(t *testing.T) {
		s, _ := newTestStore(t)
		if err := s.Delete(404); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Delete(404) = %v; want ErrNotFound", err)
		}
	})

	t.Run("the transactions that pointed at it keep their money", func(t *testing.T) {
		s, conn := newTestStore(t)
		g := mustCreate(t, s, laptop())
		acc := account(t, conn, "INTER", "BRL")
		file(t, conn, acc, g.ID, "income", 10000)

		if err := s.Delete(g.ID); err != nil {
			t.Fatalf("Delete with a transaction linked = %v; want it allowed", err)
		}

		var n int
		var goal sql.NullInt64
		if err := conn.QueryRow(
			`SELECT count(*), max(goal_id) FROM transactions`).Scan(&n, &goal); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("%d transactions left; want the row to survive its goal", n)
		}
		if goal.Valid {
			t.Errorf("goal_id = %d; want NULL", goal.Int64)
		}
	})
}

func TestLinked(t *testing.T) {
	t.Run("counts only this goal's transactions", func(t *testing.T) {
		s, conn := newTestStore(t)
		mine := mustCreate(t, s, laptop())
		other := laptop()
		other.Name = "Holiday"
		other = mustCreate(t, s, other)
		acc := account(t, conn, "INTER", "BRL")
		file(t, conn, acc, mine.ID, "income", 1000)
		file(t, conn, acc, mine.ID, "income", 2000)
		file(t, conn, acc, other.ID, "income", 3000)
		file(t, conn, acc, 0, "income", 4000)

		n, err := s.Linked(mine.ID)
		if err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("Linked = %d; want 2", n)
		}
	})

	t.Run("a goal with none counts zero", func(t *testing.T) {
		s, _ := newTestStore(t)
		g := mustCreate(t, s, laptop())
		n, err := s.Linked(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("Linked = %d; want 0", n)
		}
	})
}

func TestTargetLog(t *testing.T) {
	t.Run("changing the target writes an entry", func(t *testing.T) {
		s, _ := newTestStore(t)
		g := mustCreate(t, s, laptop())

		g.Target = 350000
		if err := s.Update(g, "consegui um desconto"); err != nil {
			t.Fatal(err)
		}

		log, err := s.TargetLog(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(log) != 1 {
			t.Fatalf("log has %d entries; want 1", len(log))
		}
		if log[0].Previous != 500000 || log[0].Target != 350000 {
			t.Errorf("entry = %d → %d; want 500000 → 350000", log[0].Previous, log[0].Target)
		}
		if log[0].Note != "consegui um desconto" {
			t.Errorf("note = %q; want it kept", log[0].Note)
		}
		if log[0].CreatedAt == "" {
			t.Error("the entry has no date on it")
		}
	})

	t.Run("the note is optional", func(t *testing.T) {
		s, _ := newTestStore(t)
		g := mustCreate(t, s, laptop())
		g.Target = 350000
		if err := s.Update(g, ""); err != nil {
			t.Fatal(err)
		}
		log, err := s.TargetLog(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(log) != 1 || log[0].Note != "" {
			t.Fatalf("log = %+v; want one entry with no note", log)
		}
	})

	t.Run("an edit that leaves the target alone writes nothing", func(t *testing.T) {
		s, _ := newTestStore(t)
		g := mustCreate(t, s, laptop())

		g.Name = "Better laptop"
		g.Description = "mudei o nome"
		if err := s.Update(g, "this note has nothing to explain"); err != nil {
			t.Fatal(err)
		}

		log, err := s.TargetLog(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(log) != 0 {
			t.Fatalf("log has %d entries; want none — the target never moved", len(log))
		}
	})

	t.Run("creating a goal logs nothing", func(t *testing.T) {
		s, _ := newTestStore(t)
		g := mustCreate(t, s, laptop())
		log, err := s.TargetLog(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(log) != 0 {
			t.Fatalf("log has %d entries on a new goal; want none", len(log))
		}
	})

	t.Run("several changes come back newest first", func(t *testing.T) {
		s, conn := newTestStore(t)
		g := mustCreate(t, s, laptop())

		for i, step := range []struct {
			target int64
			note   string
		}{{400000, "primeiro corte"}, {350000, "consegui um desconto"}, {300000, "outro desconto"}} {
			g.Target = step.target
			if err := s.Update(g, step.note); err != nil {
				t.Fatal(err)
			}
			// datetime('now') has one-second resolution, so without this the three
			// entries share a timestamp and the order is the id's to decide. The
			// offset counts up with the loop, which is the order they happened in.
			if _, err := conn.Exec(
				`UPDATE goal_target_log SET created_at = datetime('now', ? || ' seconds') WHERE note = ?`,
				i, step.note); err != nil {
				t.Fatal(err)
			}
		}

		log, err := s.TargetLog(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(log) != 3 {
			t.Fatalf("log has %d entries; want 3", len(log))
		}
		if log[0].Note != "outro desconto" || log[2].Note != "primeiro corte" {
			t.Fatalf("log order = %q … %q; want newest first", log[0].Note, log[2].Note)
		}
		// Each entry says what it moved from, so the whole story reads without
		// walking the chain.
		if log[0].Previous != 350000 || log[0].Target != 300000 {
			t.Errorf("newest entry = %d → %d; want 350000 → 300000", log[0].Previous, log[0].Target)
		}
	})

	t.Run("a refused update logs nothing", func(t *testing.T) {
		s, _ := newTestStore(t)
		g := mustCreate(t, s, laptop())

		g.Target = 350000
		g.Name = ""
		if err := s.Update(g, "should not be written"); err == nil {
			t.Fatal("an update with a blank name succeeded")
		}
		log, err := s.TargetLog(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(log) != 0 {
			t.Fatalf("log has %d entries after a refused update; want none", len(log))
		}
	})

	t.Run("the log goes when the goal goes", func(t *testing.T) {
		s, _ := newTestStore(t)
		g := mustCreate(t, s, laptop())
		g.Target = 350000
		if err := s.Update(g, "desconto"); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete(g.ID); err != nil {
			t.Fatal(err)
		}
		log, err := s.TargetLog(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(log) != 0 {
			t.Fatalf("%d entries survived the goal; want none", len(log))
		}
	})

	t.Run("a goal that never moved has an empty log", func(t *testing.T) {
		s, _ := newTestStore(t)
		g := mustCreate(t, s, laptop())
		log, err := s.TargetLog(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(log) != 0 {
			t.Fatalf("log = %+v; want it empty", log)
		}
	})
}
