package categories

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"kakei/internal/core"
	"kakei/internal/db"
	"kakei/internal/logs"
)

// newTestStore gives the caller its own SQLite file in its own temp dir, so no
// two cases ever share state. Call it inside the subtest, not the parent.
//
// A real file rather than :memory: — the UNIQUE index and the CHECK constraints
// are what most of these tests are about, and only the migration path builds
// them.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("KAKEI_DB", filepath.Join(t.TempDir(), "kakei.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return NewStore(conn)
}

func mustCreate(t *testing.T, s *Store, c Category) Category {
	t.Helper()
	if err := s.Create(&c, logs.User); err != nil {
		t.Fatalf("create %s: %v", c.Code, err)
	}
	return c
}

func home() Category {
	return Category{Code: "HOME1", Name: "Home", Description: "rent and repairs", Color: "blue"}
}

func TestSchema(t *testing.T) {
	t.Run("the code is unique", func(t *testing.T) {
		s := newTestStore(t)
		mustCreate(t, s, home())
		dup := home()
		dup.Name = "House"
		err := s.Create(&dup, logs.User)
		if err == nil || !strings.Contains(err.Error(), "already in use") {
			t.Fatalf("create duplicate = %v; want a readable code clash", err)
		}
	})

	t.Run("the code must be five characters", func(t *testing.T) {
		s := newTestStore(t)
		c := home()
		c.Code = "ABC"
		if err := s.Create(&c, logs.User); err == nil {
			t.Fatal("create with a 3-character code succeeded; want the CHECK to reject it")
		}
	})

	t.Run("description defaults to empty and timestamps fill themselves", func(t *testing.T) {
		s := newTestStore(t)
		c := home()
		c.Description = ""
		c = mustCreate(t, s, c)
		got, err := s.Get(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Description != "" {
			t.Fatalf("description = %q; want empty", got.Description)
		}
		if got.CreatedAt == "" || got.UpdatedAt == "" {
			t.Fatalf("timestamps = %q/%q; want both filled", got.CreatedAt, got.UpdatedAt)
		}
	})
}

func TestCreateAndGet(t *testing.T) {
	t.Run("round-trips every field", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, home())
		if c.ID == 0 {
			t.Fatal("Create left the id at zero")
		}
		got, err := s.Get(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Code != "HOME1" || got.Name != "Home" || got.Description != "rent and repairs" || got.Color != "blue" {
			t.Fatalf("got %+v; want the values it was created with", got)
		}
	})

	t.Run("normalizes the code on the way in", func(t *testing.T) {
		s := newTestStore(t)
		c := home()
		c.Code = " home1 "
		c = mustCreate(t, s, c)
		if c.Code != "HOME1" {
			t.Fatalf("Code = %q; want HOME1", c.Code)
		}
	})

	t.Run("a missing id is not found", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := s.Get(404); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get(404) = %v; want ErrNotFound", err)
		}
	})
}

func TestList(t *testing.T) {
	t.Run("sorts by name", func(t *testing.T) {
		s := newTestStore(t)
		mustCreate(t, s, Category{Code: "WORK1", Name: "Work", Color: "indigo"})
		mustCreate(t, s, Category{Code: "FOOD1", Name: "Food", Color: "lime"})
		mustCreate(t, s, Category{Code: "PETS1", Name: "Pets", Color: "orange"})

		all, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, c := range all {
			got = append(got, c.Name)
		}
		if strings.Join(got, ",") != "Food,Pets,Work" {
			t.Fatalf("List order = %v; want Food,Pets,Work", got)
		}
	})

	t.Run("an empty table lists nothing", func(t *testing.T) {
		s := newTestStore(t)
		all, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 0 {
			t.Fatalf("List = %d rows; want 0", len(all))
		}
	})
}

func TestByCodeAndResolve(t *testing.T) {
	t.Run("ByCode ignores case and surrounding space", func(t *testing.T) {
		s := newTestStore(t)
		mustCreate(t, s, home())
		for _, ref := range []string{"HOME1", "home1", " Home1 "} {
			got, err := s.ByCode(ref)
			if err != nil {
				t.Fatalf("ByCode(%q): %v", ref, err)
			}
			if got.Name != "Home" {
				t.Fatalf("ByCode(%q) = %q", ref, got.Name)
			}
		}
	})

	t.Run("Resolve takes an id or a code", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, home())
		for _, ref := range []string{strconv.FormatInt(c.ID, 10), "HOME1"} {
			got, err := s.Resolve(ref)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", ref, err)
			}
			if got.ID != c.ID {
				t.Fatalf("Resolve(%q) = id %d; want %d", ref, got.ID, c.ID)
			}
		}
	})

	t.Run("an unknown reference is not found", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := s.Resolve("NOPE1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Resolve = %v; want ErrNotFound", err)
		}
	})
}

func TestUpdate(t *testing.T) {
	t.Run("writes every field back", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, home())
		c.Name = "Housing"
		c.Description = "mortgage"
		c.Color = "teal"
		c.Code = "HOUS1"
		if err := s.Update(c); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "Housing" || got.Description != "mortgage" || got.Color != "teal" || got.Code != "HOUS1" {
			t.Fatalf("got %+v; want the edited values", got)
		}
	})

	t.Run("a missing id is not found", func(t *testing.T) {
		s := newTestStore(t)
		c := home()
		c.ID = 404
		if err := s.Update(c); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update(404) = %v; want ErrNotFound", err)
		}
	})

	t.Run("taking another category's code is refused", func(t *testing.T) {
		s := newTestStore(t)
		mustCreate(t, s, home())
		other := mustCreate(t, s, Category{Code: "WORK1", Name: "Work", Color: "indigo"})
		other.Code = "HOME1"
		err := s.Update(other)
		if err == nil || !strings.Contains(err.Error(), "already in use") {
			t.Fatalf("Update = %v; want a readable code clash", err)
		}
	})
}

func TestDelete(t *testing.T) {
	t.Run("removes the row", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, home())
		if err := s.Delete(c.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Get(c.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get after delete = %v; want ErrNotFound", err)
		}
	})

	t.Run("a missing id is not found", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.Delete(404); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Delete(404) = %v; want ErrNotFound", err)
		}
	})
}

// TestNameIsRequired pins the guard at the store boundary rather than in the
// form: huh returns without running its validators when stdin ends mid-form.
// See wiki/gotchas/huh-form-skips-validators-on-eof.
func TestNameIsRequired(t *testing.T) {
	for _, blank := range []string{"", "   ", "\t"} {
		t.Run("create rejects "+strconv.Quote(blank), func(t *testing.T) {
			s := newTestStore(t)
			c := home()
			c.Name = blank
			if err := s.Create(&c, logs.User); err == nil {
				t.Fatal("Create accepted a blank name")
			}
		})

		t.Run("update rejects "+strconv.Quote(blank), func(t *testing.T) {
			s := newTestStore(t)
			c := mustCreate(t, s, home())
			c.Name = blank
			if err := s.Update(c); err == nil {
				t.Fatal("Update accepted a blank name")
			}
		})
	}
}

func TestCodeTakenAndSuggest(t *testing.T) {
	t.Run("CodeTaken sees an existing code, in any case", func(t *testing.T) {
		s := newTestStore(t)
		mustCreate(t, s, home())
		for _, ref := range []string{"HOME1", "home1"} {
			taken, err := s.CodeTaken(ref)
			if err != nil {
				t.Fatal(err)
			}
			if !taken {
				t.Fatalf("CodeTaken(%q) = false", ref)
			}
		}
		taken, err := s.CodeTaken("FREE1")
		if err != nil {
			t.Fatal(err)
		}
		if taken {
			t.Fatal("CodeTaken(FREE1) = true")
		}
	})

	t.Run("SuggestCode returns a valid free code", func(t *testing.T) {
		s := newTestStore(t)
		code, err := s.SuggestCode()
		if err != nil {
			t.Fatal(err)
		}
		if err := core.ValidateCode(code); err != nil {
			t.Fatalf("SuggestCode returned %q: %v", code, err)
		}
		taken, err := s.CodeTaken(code)
		if err != nil {
			t.Fatal(err)
		}
		if taken {
			t.Fatalf("SuggestCode returned the taken code %q", code)
		}
	})
}

func TestStarter(t *testing.T) {
	t.Run("every code is valid and unique", func(t *testing.T) {
		seen := map[string]bool{}
		for _, c := range Starter {
			if err := core.ValidateCode(c.Code); err != nil {
				t.Fatalf("%s: %v", c.Code, err)
			}
			if seen[c.Code] {
				t.Fatalf("%s appears twice", c.Code)
			}
			seen[c.Code] = true
		}
	})

	t.Run("every name is valid", func(t *testing.T) {
		for _, c := range Starter {
			if err := core.ValidateName(c.Name); err != nil {
				t.Fatalf("%s: %v", c.Code, err)
			}
		}
	})

	t.Run("every color is a real palette entry", func(t *testing.T) {
		for _, c := range Starter {
			if core.ColorByName(c.Color).Name != c.Color {
				t.Fatalf("%s: color %q is not in the palette", c.Code, c.Color)
			}
		}
	})
}

func TestSeed(t *testing.T) {
	t.Run("fills an empty database", func(t *testing.T) {
		s := newTestStore(t)
		n, err := Seed(s)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(Starter) {
			t.Fatalf("Seed = %d; want %d", n, len(Starter))
		}
		all, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != len(Starter) {
			t.Fatalf("List = %d rows; want %d", len(all), len(Starter))
		}
	})

	t.Run("running twice adds nothing", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := Seed(s); err != nil {
			t.Fatal(err)
		}
		n, err := Seed(s)
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("second Seed = %d; want 0", n)
		}
	})

	t.Run("an edited starter survives a re-seed", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := Seed(s); err != nil {
			t.Fatal(err)
		}
		c, err := s.ByCode(Starter[0].Code)
		if err != nil {
			t.Fatal(err)
		}
		c.Name = "Mine now"
		if err := s.Update(c); err != nil {
			t.Fatal(err)
		}
		if _, err := Seed(s); err != nil {
			t.Fatal(err)
		}
		got, err := s.ByCode(Starter[0].Code)
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "Mine now" {
			t.Fatalf("name = %q; want the edit to survive", got.Name)
		}
	})

	t.Run("a deleted starter stays deleted", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := Seed(s); err != nil {
			t.Fatal(err)
		}
		c, err := s.ByCode(Starter[0].Code)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Delete(c.ID); err != nil {
			t.Fatal(err)
		}
		n, err := Seed(s)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("Seed = %d; want it to top up the one that is missing", n)
		}
	})
}

// trail is every audit row so far, oldest first.
func trail(t *testing.T, s *Store) []logs.Entry {
	t.Helper()
	es, err := logs.List(s.db, logs.Filter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(es)-1; i < j; i, j = i+1, j-1 {
		es[i], es[j] = es[j], es[i]
	}
	return es
}

func TestAuditTrail(t *testing.T) {
	t.Run("a create logs the source it was given", func(t *testing.T) {
		s := newTestStore(t)
		c := home()
		if err := s.Create(&c, logs.System); err != nil {
			t.Fatal(err)
		}
		es := trail(t, s)
		if len(es) != 1 || es[0].Source != logs.System || es[0].Entity != "category" {
			t.Fatalf("trail = %+v; want one system category create", es)
		}
	})

	t.Run("edit and delete each leave one user row", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, home())
		c.Name = "House"
		if err := s.Update(c); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete(c.ID); err != nil {
			t.Fatal(err)
		}
		es := trail(t, s)
		if len(es) != 3 {
			t.Fatalf("trail has %d rows; want 3", len(es))
		}
		if es[1].Action != "edited" || !strings.Contains(es[1].Changes, `"name"`) {
			t.Errorf("edit row = %+v; want the name move", es[1])
		}
		if es[2].Action != "deleted" || es[2].Source != logs.User {
			t.Errorf("delete row = %+v; want user/deleted", es[2])
		}
	})

	t.Run("the starter seed logs as the system, once", func(t *testing.T) {
		s := newTestStore(t)
		n, err := Seed(s)
		if err != nil {
			t.Fatal(err)
		}
		es := trail(t, s)
		if len(es) != n {
			t.Fatalf("trail has %d rows after seeding %d starters; want one each", len(es), n)
		}
		for _, e := range es {
			if e.Source != logs.System || e.Action != "created" || e.Entity != "category" {
				t.Fatalf("seed row = %+v; want system/created/category", e)
			}
		}
		if _, err := Seed(s); err != nil {
			t.Fatal(err)
		}
		if again := trail(t, s); len(again) != n {
			t.Fatalf("trail has %d rows after a second seed; want still %d", len(again), n)
		}
	})
}
