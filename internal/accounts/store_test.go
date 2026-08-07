package accounts

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"kakei/internal/db"
)

// newTestStore gives the caller its own SQLite file in its own temp dir, so no
// two cases ever share state. Call it inside the subtest, not the parent, or
// the cases go back to sharing one database.
//
// It is deliberately a real file rather than :memory: — the schema, the UNIQUE
// index and the CHECK constraints are what most of these tests are about, and
// only the real migration path builds them.
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

// mustCreate inserts an account and fails the test if it cannot.
func mustCreate(t *testing.T, s *Store, a Account) Account {
	t.Helper()
	if err := s.Create(&a); err != nil {
		t.Fatalf("create %s: %v", a.Code, err)
	}
	return a
}

// listNames returns the account names in the order List gives them back.
func listNames(t *testing.T, s *Store) []string {
	t.Helper()
	all, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range all {
		names = append(names, a.Name)
	}
	return names
}

func wallet() Account {
	return Account{Code: "WLLT2", Name: "Wallet", Color: "green", Currency: "BTC", Balance: 150000000}
}

func TestCreate(t *testing.T) {
	t.Run("assigns id and uppercases the code", func(t *testing.T) {
		s := newTestStore(t)
		a := Account{Code: " wllt2 ", Name: "Wallet", Color: "green", Currency: "BTC"}
		if err := s.Create(&a); err != nil {
			t.Fatal(err)
		}
		if a.ID == 0 {
			t.Error("create left ID at 0")
		}
		if a.Code != "WLLT2" {
			t.Errorf("code = %q; want the normalized WLLT2", a.Code)
		}
	})

	t.Run("defaults description and is_frozen", func(t *testing.T) {
		s := newTestStore(t)
		a := mustCreate(t, s, Account{Code: "DFLT1", Name: "Defaults", Color: "red", Currency: "USD"})
		got, err := s.Get(a.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Description != "" || got.IsFrozen {
			t.Errorf("got description %q frozen %v; want empty and active", got.Description, got.IsFrozen)
		}
		if got.CreatedAt == "" || got.UpdatedAt == "" {
			t.Errorf("timestamps not filled in: %+v", got)
		}
	})

	t.Run("rejects a duplicate code with a readable error", func(t *testing.T) {
		s := newTestStore(t)
		mustCreate(t, s, wallet())

		err := s.Create(&Account{Code: "wllt2", Name: "Other", Color: "red", Currency: "USD"})
		if err == nil {
			t.Fatal("duplicate code was accepted")
		}
		if !strings.Contains(err.Error(), "already in use") {
			t.Errorf("error %q does not say the code is taken", err)
		}
	})

	t.Run("rejects a code the CHECK constraint refuses", func(t *testing.T) {
		s := newTestStore(t)
		// Create does not call ValidateCode — the length(code) = 5 CHECK in the
		// migration is the last line of defence, and this pins it.
		for _, code := range []string{"AB", "TOOLONG"} {
			if err := s.Create(&Account{Code: code, Name: "Bad", Color: "red", Currency: "USD"}); err == nil {
				t.Errorf("code %q was accepted", code)
			}
		}
	})
}

func TestGet(t *testing.T) {
	t.Run("round trips every field", func(t *testing.T) {
		s := newTestStore(t)
		a := mustCreate(t, s, Account{
			Code: "FULL1", Name: "Full", Description: "every column",
			Color: "violet", Currency: "BRL", Balance: -12345,
		})

		got, err := s.Get(a.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Code != "FULL1" || got.Name != "Full" || got.Description != "every column" ||
			got.Color != "violet" || got.Currency != "BRL" || got.Balance != -12345 {
			t.Fatalf("round trip changed the account: %+v", got)
		}
	})

	t.Run("missing id is ErrNotFound", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := s.Get(404); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get(404) = %v; want ErrNotFound", err)
		}
	})
}

func TestByCode(t *testing.T) {
	t.Run("normalizes the lookup", func(t *testing.T) {
		s := newTestStore(t)
		a := mustCreate(t, s, wallet())

		for _, ref := range []string{"WLLT2", "wllt2", " Wllt2 "} {
			got, err := s.ByCode(ref)
			if err != nil || got.ID != a.ID {
				t.Errorf("ByCode(%q) = %+v, %v", ref, got, err)
			}
		}
	})

	t.Run("missing code is ErrNotFound", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := s.ByCode("NOPE1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ByCode of a missing code = %v; want ErrNotFound", err)
		}
	})
}

func TestResolve(t *testing.T) {
	s := newTestStore(t)
	a := mustCreate(t, s, wallet())

	// All digits means id, anything else means code — one store, many refs, so
	// these share a database on purpose: they are one behaviour, not many.
	found := []string{"WLLT2", "wllt2", " wllt2 ", "1", " 1 "}
	for _, ref := range found {
		t.Run("finds "+ref, func(t *testing.T) {
			got, err := s.Resolve(ref)
			if err != nil || got.ID != a.ID {
				t.Fatalf("Resolve(%q) = %+v, %v", ref, got, err)
			}
		})
	}

	missing := []string{"NOPE1", "999", ""}
	for _, ref := range missing {
		t.Run("misses "+ref, func(t *testing.T) {
			if _, err := s.Resolve(ref); !errors.Is(err, ErrNotFound) {
				t.Fatalf("Resolve(%q) = %v; want ErrNotFound", ref, err)
			}
		})
	}
}

func TestList(t *testing.T) {
	t.Run("empty database lists nothing", func(t *testing.T) {
		s := newTestStore(t)
		all, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 0 {
			t.Fatalf("List on an empty database = %+v", all)
		}
	})

	t.Run("orders by name, not by insertion", func(t *testing.T) {
		s := newTestStore(t)
		for _, n := range []string{"Zebra", "Apple", "Middle"} {
			mustCreate(t, s, Account{Code: n[:1] + "AAA1", Name: n, Color: "red", Currency: "USD"})
		}

		if got := listNames(t, s); strings.Join(got, ",") != "Apple,Middle,Zebra" {
			t.Fatalf("List order = %v; want alphabetical", got)
		}
	})

	t.Run("sinks frozen accounts to the bottom", func(t *testing.T) {
		s := newTestStore(t)
		for _, n := range []string{"Zebra", "Apple", "Middle"} {
			mustCreate(t, s, Account{Code: n[:1] + "AAA1", Name: n, Color: "red", Currency: "USD"})
		}
		frozen, err := s.ByCode("AAAA1") // Apple, first alphabetically
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.ToggleFreeze(frozen.ID); err != nil {
			t.Fatal(err)
		}

		// Frozen accounts still list — hiding them is the command's job — but
		// they sort after every active one, whatever their name.
		if got := listNames(t, s); strings.Join(got, ",") != "Middle,Zebra,Apple" {
			t.Fatalf("List order = %v; want the frozen Apple last", got)
		}
	})
}

func TestUpdate(t *testing.T) {
	t.Run("persists every changed field", func(t *testing.T) {
		s := newTestStore(t)
		a := mustCreate(t, s, wallet())

		a.Name = "Cold Wallet"
		a.Description = "hardware"
		a.Code = "cold9"
		a.Balance = 1
		a.Currency = "USD"
		a.IsFrozen = true
		if err := s.Update(a); err != nil {
			t.Fatal(err)
		}

		got, err := s.Get(a.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "Cold Wallet" || got.Description != "hardware" || got.Code != "COLD9" ||
			got.Balance != 1 || got.Currency != "USD" || !got.IsFrozen {
			t.Fatalf("update did not stick: %+v", got)
		}
	})

	t.Run("missing id is ErrNotFound", func(t *testing.T) {
		s := newTestStore(t)
		a := wallet()
		a.ID = 404
		if err := s.Update(a); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update of a missing id = %v; want ErrNotFound", err)
		}
	})

	t.Run("rejects taking another account's code", func(t *testing.T) {
		s := newTestStore(t)
		mustCreate(t, s, wallet())
		other := mustCreate(t, s, Account{Code: "SAVE1", Name: "Savings", Color: "blue", Currency: "USD"})

		other.Code = "WLLT2"
		err := s.Update(other)
		if err == nil {
			t.Fatal("update to a taken code was accepted")
		}
		if !strings.Contains(err.Error(), "already in use") {
			t.Errorf("error %q does not say the code is taken", err)
		}
	})

	t.Run("keeping its own code is fine", func(t *testing.T) {
		s := newTestStore(t)
		a := mustCreate(t, s, wallet())
		a.Name = "Renamed"
		if err := s.Update(a); err != nil {
			t.Fatalf("update with an unchanged code = %v", err)
		}
	})
}

func TestDelete(t *testing.T) {
	t.Run("removes the row", func(t *testing.T) {
		s := newTestStore(t)
		a := mustCreate(t, s, wallet())

		if err := s.Delete(a.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Get(a.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("account still readable after delete: %v", err)
		}
	})

	t.Run("deleting twice is ErrNotFound", func(t *testing.T) {
		s := newTestStore(t)
		a := mustCreate(t, s, wallet())
		if err := s.Delete(a.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete(a.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("second delete = %v; want ErrNotFound", err)
		}
	})

	t.Run("frees the code for reuse", func(t *testing.T) {
		s := newTestStore(t)
		a := mustCreate(t, s, wallet())
		if err := s.Delete(a.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.Create(&Account{Code: "WLLT2", Name: "Reused", Color: "red", Currency: "USD"}); err != nil {
			t.Fatalf("code not released by delete: %v", err)
		}
	})
}

func TestToggleFreeze(t *testing.T) {
	t.Run("alternates and stores the new state", func(t *testing.T) {
		s := newTestStore(t)
		a := mustCreate(t, s, Account{Code: "FRZ99", Name: "Savings", Color: "blue", Currency: "BRL"})

		for i, want := range []bool{true, false, true} {
			got, err := s.ToggleFreeze(a.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("toggle %d returned %v; want %v", i+1, got, want)
			}
			stored, _ := s.Get(a.ID)
			if stored.IsFrozen != want {
				t.Fatalf("toggle %d stored %v; want %v", i+1, stored.IsFrozen, want)
			}
		}
	})

	t.Run("missing id is ErrNotFound", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := s.ToggleFreeze(404); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ToggleFreeze(404) = %v; want ErrNotFound", err)
		}
	})
}

func TestCodeTaken(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, wallet())

	cases := []struct {
		code string
		want bool
	}{
		{"WLLT2", true},
		{"wllt2", true}, // normalized before the lookup
		{" wllt2 ", true},
		{"FREE1", false},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			got, err := s.CodeTaken(tc.code)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("CodeTaken(%q) = %v; want %v", tc.code, got, tc.want)
			}
		})
	}
}

func TestSuggestCode(t *testing.T) {
	t.Run("returns a valid, free code", func(t *testing.T) {
		s := newTestStore(t)
		mustCreate(t, s, wallet())

		code, err := s.SuggestCode()
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateCode(code); err != nil {
			t.Fatalf("suggested %q: %v", code, err)
		}
		if taken, _ := s.CodeTaken(code); taken {
			t.Fatalf("SuggestCode returned the taken code %q", code)
		}
	})
}

func TestBalancePrecisionSurvivesStorage(t *testing.T) {
	cases := []struct {
		name     string
		currency string
		balance  int64
		want     string
	}{
		{"one satoshi", "BTC", 1, "0.00000001"},
		{"whole bitcoin", "BTC", 100000000, "1.00000000"},
		{"cents", "USD", 5, "0.05"},
		{"negative", "BRL", -1234, "-12.34"},
		{"zero", "EUR", 0, "0.00"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			a := mustCreate(t, s, Account{
				Code:     string(rune('A'+i)) + "BAL1",
				Name:     tc.name,
				Color:    "red",
				Currency: tc.currency,
				Balance:  tc.balance,
			})

			got, err := s.Get(a.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Balance != tc.balance || got.Amount() != tc.want {
				t.Fatalf("stored %d shown as %q; want %d / %q",
					got.Balance, got.Amount(), tc.balance, tc.want)
			}
		})
	}
}
