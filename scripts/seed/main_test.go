package main

import (
	"path/filepath"
	"testing"

	"kakei/internal/accounts"
	"kakei/internal/core"
	"kakei/internal/db"
)

func newTestStore(t *testing.T) *accounts.Store {
	t.Helper()
	t.Setenv("KAKEI_DB", filepath.Join(t.TempDir(), "kakei.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return accounts.NewStore(conn)
}

func TestSeed(t *testing.T) {
	t.Run("inserts every fixture", func(t *testing.T) {
		s := newTestStore(t)
		n, err := seed(s)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(fixtures) {
			t.Fatalf("seed inserted %d; want %d", n, len(fixtures))
		}

		all, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != len(fixtures) {
			t.Fatalf("database holds %d accounts; want %d", len(all), len(fixtures))
		}
	})

	t.Run("running twice changes nothing", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := seed(s); err != nil {
			t.Fatal(err)
		}

		n, err := seed(s)
		if err != nil {
			t.Fatalf("second seed: %v", err)
		}
		if n != 0 {
			t.Fatalf("second seed inserted %d; want 0", n)
		}

		all, _ := s.List()
		if len(all) != len(fixtures) {
			t.Fatalf("database holds %d accounts after two seeds; want %d", len(all), len(fixtures))
		}
	})

	t.Run("leaves an edited fixture alone", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := seed(s); err != nil {
			t.Fatal(err)
		}

		a, err := s.ByCode(fixtures[0].Code)
		if err != nil {
			t.Fatal(err)
		}
		a.Balance = 42
		if err := s.Update(a); err != nil {
			t.Fatal(err)
		}

		if _, err := seed(s); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.ByCode(fixtures[0].Code); got.Balance != 42 {
			t.Fatalf("seed overwrote a local edit: balance is %d", got.Balance)
		}
	})
}

func TestFixturesAreValid(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range fixtures {
		t.Run(f.Code, func(t *testing.T) {
			if err := core.ValidateCode(f.Code); err != nil {
				t.Errorf("code %q: %v", f.Code, err)
			}
			if seen[f.Code] {
				t.Errorf("code %q appears twice", f.Code)
			}
			seen[f.Code] = true

			if f.Name == "" {
				t.Error("fixture has no name")
			}
			if core.ColorByName(f.Color).Name != f.Color {
				t.Errorf("color %q is not in the palette", f.Color)
			}
			if core.CurrencyByCode(f.Currency).Code != f.Currency {
				t.Errorf("currency %q is not supported", f.Currency)
			}
		})
	}
}
