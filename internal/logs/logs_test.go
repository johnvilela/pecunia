package logs

import (
	"database/sql"
	"reflect"
	"testing"
)

// stamp writes a row with an explicit created_at, straight past Record, which
// is what lets the date-range cases put rows on chosen days.
func stamp(t *testing.T, conn *sql.DB, entity string, id int64, createdAt string) {
	t.Helper()
	if _, err := conn.Exec(
		`INSERT INTO logs (source, action, entity, entity_id, created_at) VALUES ('user', 'created', ?, ?, ?)`,
		entity, id, createdAt); err != nil {
		t.Fatal(err)
	}
}

func TestRecord(t *testing.T) {
	t.Run("a recorded action comes back whole", func(t *testing.T) {
		conn := newTestDB(t)
		if err := Record(conn, User, "created", "account", 7); err != nil {
			t.Fatal(err)
		}
		got, err := List(conn, Filter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d entries; want 1", len(got))
		}
		e := got[0]
		if e.Source != User || e.Action != "created" || e.Entity != "account" || e.EntityID != 7 {
			t.Fatalf("entry = %+v; want user/created/account/7", e)
		}
		if e.Changes != "" {
			t.Fatalf("changes = %q; want empty on a create", e.Changes)
		}
		if e.CreatedAt == "" {
			t.Fatal("created_at is empty; want the database to stamp it")
		}
	})

	t.Run("an edit stores only what changed, as JSON", func(t *testing.T) {
		conn := newTestDB(t)
		changes := Diff(
			F("name", "Cash", "Wallet"),
			F("color", "green", "green"),
		)
		if err := RecordEdit(conn, User, "account", 7, changes); err != nil {
			t.Fatal(err)
		}
		got, err := List(conn, Filter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d entries; want 1", len(got))
		}
		if got[0].Action != "edited" {
			t.Fatalf("action = %q; want edited", got[0].Action)
		}
		want := `{"name":{"old":"Cash","new":"Wallet"}}`
		if got[0].Changes != want {
			t.Fatalf("changes = %s; want %s", got[0].Changes, want)
		}
	})

	t.Run("an edit that changed nothing writes nothing", func(t *testing.T) {
		conn := newTestDB(t)
		if err := RecordEdit(conn, User, "account", 7, Diff(F("name", "Cash", "Cash"))); err != nil {
			t.Fatal(err)
		}
		got, err := List(conn, Filter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("got %d entries; want none for a no-op edit", len(got))
		}
	})
}

func TestDiff(t *testing.T) {
	t.Run("keeps the changed and drops the unchanged", func(t *testing.T) {
		got := Diff(
			F("name", "Cash", "Wallet"),
			F("balance", int64(80000), int64(80000)),
			F("frozen", false, true),
		)
		want := map[string]Change{
			"name":   {Old: "Cash", New: "Wallet"},
			"frozen": {Old: false, New: true},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Diff = %v; want %v", got, want)
		}
	})

	t.Run("compares tags by value", func(t *testing.T) {
		if got := Diff(F("tags", []string{"a", "b"}, []string{"a", "b"})); len(got) != 0 {
			t.Fatalf("Diff = %v; want equal slices dropped", got)
		}
		if got := Diff(F("tags", []string{"a"}, []string{"a", "b"})); len(got) != 1 {
			t.Fatalf("Diff = %v; want changed slices kept", got)
		}
	})

	t.Run("money passes through as numbers", func(t *testing.T) {
		got := Diff(F("value", int64(80000), int64(95000)))
		if got["value"].Old != int64(80000) || got["value"].New != int64(95000) {
			t.Fatalf("Diff = %v; want the raw int64s", got)
		}
	})
}

func TestList(t *testing.T) {
	t.Run("filters narrow by source, action, entity and id", func(t *testing.T) {
		conn := newTestDB(t)
		if err := Record(conn, User, "created", "account", 1); err != nil {
			t.Fatal(err)
		}
		if err := Record(conn, User, "deleted", "account", 2); err != nil {
			t.Fatal(err)
		}
		if err := Record(conn, System, "created", "card_bill", 1); err != nil {
			t.Fatal(err)
		}

		for _, c := range []struct {
			name string
			f    Filter
			want int
		}{
			{"by action", Filter{Action: "created"}, 2},
			{"by entity", Filter{Entity: "account"}, 2},
			{"by entity and id", Filter{Entity: "account", EntityID: 2}, 1},
			{"by source", Filter{Source: System}, 1},
			{"all together", Filter{Source: User, Action: "created", Entity: "account", EntityID: 1}, 1},
		} {
			got, err := List(conn, c.f)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != c.want {
				t.Fatalf("%s: got %d entries; want %d", c.name, len(got), c.want)
			}
		}
	})

	t.Run("the date range keeps both named days", func(t *testing.T) {
		conn := newTestDB(t)
		stamp(t, conn, "account", 1, "2026-08-01 09:00:00")
		stamp(t, conn, "account", 2, "2026-08-15 09:00:00")
		stamp(t, conn, "account", 3, "2026-08-27 09:00:00")

		got, err := List(conn, Filter{From: "2026-08-01", To: "2026-08-15"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d entries; want 2 — both edge days included", len(got))
		}
		if got, _ := List(conn, Filter{From: "2026-08-16"}); len(got) != 1 {
			t.Fatalf("got %d entries from the 16th on; want 1", len(got))
		}
	})

	t.Run("ten newest come first, unless asked for more", func(t *testing.T) {
		conn := newTestDB(t)
		for i := int64(1); i <= 12; i++ {
			if err := Record(conn, User, "created", "transaction", i); err != nil {
				t.Fatal(err)
			}
		}
		got, err := List(conn, Filter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 10 {
			t.Fatalf("got %d entries; want the default of 10", len(got))
		}
		if got[0].EntityID != 12 {
			t.Fatalf("first entry is for id %d; want the newest, 12", got[0].EntityID)
		}
		if got, _ := List(conn, Filter{Limit: 3}); len(got) != 3 {
			t.Fatalf("got %d entries with a limit of 3; want 3", len(got))
		}
	})
}
