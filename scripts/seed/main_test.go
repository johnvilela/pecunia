package main

import (
	"path/filepath"
	"testing"

	"kakei/internal/accounts"
	"kakei/internal/cards"
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

func newTestCardStore(t *testing.T) *cards.Store {
	t.Helper()
	t.Setenv("KAKEI_DB", filepath.Join(t.TempDir(), "kakei.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return cards.NewStore(conn)
}

func TestSeedCards(t *testing.T) {
	t.Run("inserts every fixture", func(t *testing.T) {
		s := newTestCardStore(t)
		n, err := seedCards(s)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(cardFixtures) {
			t.Fatalf("seedCards inserted %d; want %d", n, len(cardFixtures))
		}

		all, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != len(cardFixtures) {
			t.Fatalf("database holds %d cards; want %d", len(all), len(cardFixtures))
		}
	})

	t.Run("running twice changes nothing", func(t *testing.T) {
		s := newTestCardStore(t)
		if _, err := seedCards(s); err != nil {
			t.Fatal(err)
		}

		n, err := seedCards(s)
		if err != nil {
			t.Fatalf("second seed: %v", err)
		}
		if n != 0 {
			t.Fatalf("second seed inserted %d; want 0", n)
		}
	})

	t.Run("leaves an edited fixture alone", func(t *testing.T) {
		s := newTestCardStore(t)
		if _, err := seedCards(s); err != nil {
			t.Fatal(err)
		}

		c, err := s.ByCode(cardFixtures[0].Code)
		if err != nil {
			t.Fatal(err)
		}
		c.Balance = 42
		if err := s.Update(c); err != nil {
			t.Fatal(err)
		}

		if _, err := seedCards(s); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.ByCode(cardFixtures[0].Code); got.Balance != 42 {
			t.Fatalf("seed overwrote a local edit: balance is %d", got.Balance)
		}
	})
}

func TestCardFixturesAreValid(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range cardFixtures {
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
			if f.ClosingDay < 1 || f.ClosingDay > 31 || f.DueDay < 1 || f.DueDay > 31 {
				t.Errorf("days %d/%d are outside 1-31", f.ClosingDay, f.DueDay)
			}
		})
	}
}

// The fixtures exist to exercise every branch of the renderers.
func TestCardFixturesCoverTheRenderBranches(t *testing.T) {
	var overLimit, zeroBalance, noDescription bool
	for _, f := range cardFixtures {
		overLimit = overLimit || f.Available() < 0
		zeroBalance = zeroBalance || f.Balance == 0
		noDescription = noDescription || f.Description == ""
	}
	if !overLimit || !zeroBalance || !noDescription {
		t.Fatalf("fixtures miss a branch: overLimit=%v zeroBalance=%v noDescription=%v",
			overLimit, zeroBalance, noDescription)
	}
}
