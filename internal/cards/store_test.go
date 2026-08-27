package cards

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

func mustCreate(t *testing.T, s *Store, c Card) Card {
	t.Helper()
	if err := s.Create(&c); err != nil {
		t.Fatalf("create %s: %v", c.Code, err)
	}
	return c
}

// listNames returns the card names in the order List gives them back.
func listNames(t *testing.T, s *Store) []string {
	t.Helper()
	all, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range all {
		names = append(names, c.Name)
	}
	return names
}

func nubank() Card {
	return Card{
		Code: "NUCRD", Name: "Nubank", Color: "violet", Currency: "BRL",
		Limit: 500000, Balance: 123850, ClosingDay: 15, DueDay: 22,
	}
}

func TestCreate(t *testing.T) {
	t.Run("assigns id and uppercases the code", func(t *testing.T) {
		s := newTestStore(t)
		c := nubank()
		c.Code = " nucrd "
		if err := s.Create(&c); err != nil {
			t.Fatal(err)
		}
		if c.ID == 0 {
			t.Error("create left ID at 0")
		}
		if c.Code != "NUCRD" {
			t.Errorf("code = %q; want the normalized NUCRD", c.Code)
		}
	})

	t.Run("defaults the description and fills the timestamps", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, Card{
			Code: "DFLT1", Name: "Defaults", Color: "red", Currency: "USD",
			ClosingDay: 1, DueDay: 10,
		})
		got, err := s.Get(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Description != "" || got.Limit != 0 || got.Balance != 0 {
			t.Errorf("defaults not applied: %+v", got)
		}
		// A card declines past its limit unless its issuer says otherwise, so
		// false is the safe default.
		if got.OverLimitAllowed {
			t.Errorf("over-limit defaulted to allowed: %+v", got)
		}
		if got.CreatedAt == "" || got.UpdatedAt == "" {
			t.Errorf("timestamps not filled in: %+v", got)
		}
	})

	t.Run("rejects a duplicate code with a readable error", func(t *testing.T) {
		s := newTestStore(t)
		mustCreate(t, s, nubank())

		dup := nubank()
		dup.Code = "nucrd"
		dup.Name = "Other"
		err := s.Create(&dup)
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
			c := nubank()
			c.Code = code
			if err := s.Create(&c); err == nil {
				t.Errorf("code %q was accepted", code)
			}
		}
	})

	t.Run("rejects a day outside 1-31", func(t *testing.T) {
		s := newTestStore(t)
		cases := []struct {
			name              string
			closing, due, seq int
		}{
			{"closing zero", 0, 10, 1},
			{"closing 32", 32, 10, 2},
			{"due zero", 10, 0, 3},
			{"due 32", 10, 32, 4},
		}
		for _, tc := range cases {
			c := nubank()
			c.Code = "DAY0" + string(rune('0'+tc.seq))
			c.ClosingDay, c.DueDay = tc.closing, tc.due
			if err := s.Create(&c); err == nil {
				t.Errorf("%s was accepted", tc.name)
			}
		}
	})
}

func TestGet(t *testing.T) {
	t.Run("round trips every field", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, Card{
			Code: "FULL1", Name: "Full", Description: "every column",
			Color: "amber", Currency: "BTC", Limit: 100000000, Balance: -5000,
			ClosingDay: 31, DueDay: 1, OverLimitAllowed: true,
		})

		got, err := s.Get(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Code != "FULL1" || got.Name != "Full" || got.Description != "every column" ||
			got.Color != "amber" || got.Currency != "BTC" || got.Limit != 100000000 ||
			got.Balance != -5000 || got.ClosingDay != 31 || got.DueDay != 1 ||
			!got.OverLimitAllowed {
			t.Fatalf("round trip changed the card: %+v", got)
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
		c := mustCreate(t, s, nubank())

		for _, ref := range []string{"NUCRD", "nucrd", " Nucrd "} {
			got, err := s.ByCode(ref)
			if err != nil || got.ID != c.ID {
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
	c := mustCreate(t, s, nubank())

	// All digits means id, anything else means code — one store, many refs, so
	// these share a database on purpose: they are one behaviour, not many.
	for _, ref := range []string{"NUCRD", "nucrd", " nucrd ", "1", " 1 "} {
		t.Run("finds "+ref, func(t *testing.T) {
			got, err := s.Resolve(ref)
			if err != nil || got.ID != c.ID {
				t.Fatalf("Resolve(%q) = %+v, %v", ref, got, err)
			}
		})
	}

	for _, ref := range []string{"NOPE1", "999", ""} {
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
			c := nubank()
			c.Code, c.Name = n[:1]+"AAA1", n
			mustCreate(t, s, c)
		}

		if got := listNames(t, s); strings.Join(got, ",") != "Apple,Middle,Zebra" {
			t.Fatalf("List order = %v; want alphabetical", got)
		}
	})
}

func TestUpdate(t *testing.T) {
	t.Run("persists every changed field", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, nubank())

		c.Name = "Nubank Ultravioleta"
		c.Description = "principal"
		c.Code = "nuvio"
		c.Color = "teal"
		c.Currency = "USD"
		c.Limit = 900000
		c.ClosingDay = 3
		c.DueDay = 28
		c.OverLimitAllowed = true
		if err := s.Update(c); err != nil {
			t.Fatal(err)
		}

		got, err := s.Get(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "Nubank Ultravioleta" || got.Description != "principal" ||
			got.Code != "NUVIO" || got.Color != "teal" || got.Currency != "USD" ||
			got.Limit != 900000 || got.ClosingDay != 3 || got.DueDay != 28 ||
			!got.OverLimitAllowed {
			t.Fatalf("update did not stick: %+v", got)
		}
	})

	t.Run("an edit never touches the balance", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, nubank())
		c.Balance = 1
		if err := s.Update(c); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Balance != nubank().Balance {
			t.Fatalf("balance = %d after an edit; want the ledger to be the only thing that moves it", got.Balance)
		}
	})

	t.Run("lowering the limit under the held balance is still refused", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, nubank()) // owes 123850
		c.Limit = 100000
		c.Balance = c.Limit // what a stale caller might pass; the stored balance is what counts
		if err := s.Update(c); err == nil {
			t.Fatal("a limit below the held balance was accepted")
		}
	})

	t.Run("missing id is ErrNotFound", func(t *testing.T) {
		// The same bug shipped once in accounts: a silent nil here makes
		// `kakei cc e` on a deleted card print "updated".
		s := newTestStore(t)
		c := nubank()
		c.ID = 404
		if err := s.Update(c); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update of a missing id = %v; want ErrNotFound", err)
		}
	})

	t.Run("rejects taking another card's code", func(t *testing.T) {
		s := newTestStore(t)
		mustCreate(t, s, nubank())
		other := nubank()
		other.Code, other.Name = "ITAU1", "Itaú"
		other = mustCreate(t, s, other)

		other.Code = "NUCRD"
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
		c := mustCreate(t, s, nubank())
		c.Name = "Renamed"
		if err := s.Update(c); err != nil {
			t.Fatalf("update with an unchanged code = %v", err)
		}
	})
}

func TestDelete(t *testing.T) {
	t.Run("removes the row", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, nubank())

		if err := s.Delete(c.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Get(c.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("card still readable after delete: %v", err)
		}
	})

	t.Run("deleting twice is ErrNotFound", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, nubank())
		if err := s.Delete(c.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete(c.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("second delete = %v; want ErrNotFound", err)
		}
	})

	t.Run("frees the code for reuse", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, nubank())
		if err := s.Delete(c.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.Create(&Card{
			Code: "NUCRD", Name: "Reused", Color: "red", Currency: "USD",
			ClosingDay: 1, DueDay: 10,
		}); err != nil {
			t.Fatalf("code not released by delete: %v", err)
		}
	})
}

func TestCodeTaken(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, nubank())

	cases := []struct {
		code string
		want bool
	}{
		{"NUCRD", true},
		{"nucrd", true}, // normalized before the lookup
		{" nucrd ", true},
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

func TestCodeSpaceIsPerTable(t *testing.T) {
	// A card and an account may share a code: `kakei cc X` and `kakei ac X`
	// each look in their own table.
	s := newTestStore(t)
	c := nubank()
	c.Code = "INTER"
	mustCreate(t, s, c)

	if taken, err := s.CodeTaken("INTER"); err != nil || !taken {
		t.Fatalf("CodeTaken on the card's own code = %v, %v", taken, err)
	}
}

func TestSuggestCode(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, nubank())

	code, err := s.SuggestCode()
	if err != nil {
		t.Fatal(err)
	}
	if err := core.ValidateCode(code); err != nil {
		t.Fatalf("suggested %q: %v", code, err)
	}
	if taken, _ := s.CodeTaken(code); taken {
		t.Fatalf("SuggestCode returned the taken code %q", code)
	}
}

func TestAmountPrecisionSurvivesStorage(t *testing.T) {
	cases := []struct {
		name     string
		currency string
		limit    int64
		balance  int64
		want     string
	}{
		{"one satoshi of a bitcoin limit", "BTC", 100000000, 1, "₿0.00000001"},
		{"cents", "USD", 500000, 5, "$0.05"},
		{"a refund", "BRL", 500000, -1234, "R$-12.34"},
		{"zero", "EUR", 0, 0, "€0.00"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			c := mustCreate(t, s, Card{
				Code: string(rune('A'+i)) + "AMT1", Name: tc.name, Color: "red",
				Currency: tc.currency, Limit: tc.limit, Balance: tc.balance,
				ClosingDay: 1, DueDay: 10,
			})

			got, err := s.Get(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Balance != tc.balance || got.Limit != tc.limit || got.Fmt(got.Balance) != tc.want {
				t.Fatalf("stored %d/%d shown as %q; want %d/%d and %q",
					got.Limit, got.Balance, got.Fmt(got.Balance), tc.limit, tc.balance, tc.want)
			}
		})
	}
}

func TestNameIsRequired(t *testing.T) {
	// The form's "name is required" validator does not run when huh returns
	// without a submit — a pty whose stdin hits EOF does exactly that. The
	// store is the boundary every caller crosses, so it is the one that has
	// to hold.
	for _, name := range []string{"", "   ", "\t"} {
		t.Run("create rejects "+strconv.Quote(name), func(t *testing.T) {
			s := newTestStore(t)
			c := nubank()
			c.Name = name
			if err := s.Create(&c); err == nil {
				t.Fatalf("a card named %q was created", name)
			}
		})

		t.Run("update rejects "+strconv.Quote(name), func(t *testing.T) {
			s := newTestStore(t)
			c := mustCreate(t, s, nubank())
			c.Name = name
			if err := s.Update(c); err == nil {
				t.Fatalf("a card was renamed to %q", name)
			}
		})
	}
}

func TestBalanceMayNotPassTheLimit(t *testing.T) {
	// The form validator is not enough on its own: huh can return without
	// running it, and the seed script never opens a form at all.
	over := func() Card {
		c := nubank()
		c.Limit, c.Balance = 300000, 412000
		return c
	}

	t.Run("create refuses it", func(t *testing.T) {
		s := newTestStore(t)
		c := over()
		if err := s.Create(&c); err == nil {
			t.Fatal("a card over its limit was created")
		}
	})

	t.Run("create allows it when the card may go over", func(t *testing.T) {
		s := newTestStore(t)
		c := over()
		c.OverLimitAllowed = true
		if err := s.Create(&c); err != nil {
			t.Fatalf("an over-limit-allowed card was refused: %v", err)
		}
	})

	t.Run("update refuses it", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, nubank())
		c.Balance = c.Limit + 1
		if err := s.Update(c); err == nil {
			t.Fatal("a card was updated past its limit")
		}
	})

	t.Run("update refuses revoking the allowance while over", func(t *testing.T) {
		s := newTestStore(t)
		c := over()
		c.OverLimitAllowed = true
		c = mustCreate(t, s, c)

		c.OverLimitAllowed = false
		if err := s.Update(c); err == nil {
			t.Fatal("the allowance was revoked while the card was still over its limit")
		}
	})
}

// Same as the accounts case: a transaction always names exactly one account or
// card, so the card cannot be deleted out from under it. Raw INSERT because
// importing kakei/internal/transactions here would be an import cycle.
func TestDeleteWhileTransactionsPointAtIt(t *testing.T) {
	t.Run("says what is blocking it", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, nubank())
		if _, err := s.db.Exec(
			`INSERT INTO transactions (title, card_id, value, kind, date)
			 VALUES ('Groceries', ?, 12000, 'outcome', '2026-08-08')`, c.ID); err != nil {
			t.Fatal(err)
		}

		err := s.Delete(c.ID)
		if err == nil {
			t.Fatal("delete = nil; want the foreign key to refuse it")
		}
		if !strings.Contains(err.Error(), "transaction") {
			t.Fatalf("delete = %q; want it to name transactions", err)
		}
	})
}

// file writes one transaction against the card. Raw SQL, and through the
// store's own handle, because this package cannot import the one that owns
// transactions.
func file(t *testing.T, s *Store, cardID int64, value int64) {
	t.Helper()
	if _, err := s.db.Exec(
		`INSERT INTO transactions (title, card_id, value, kind, date)
		 VALUES ('Compra', ?, ?, 'outcome', '2026-08-08')`, cardID, value); err != nil {
		t.Fatal(err)
	}
}

// A card's currency is the scale its limit, its balance and every charge on it
// are stored at. Moving it under live history re-reads all three.
func TestCurrencyIsFrozenOnceAnythingIsCharged(t *testing.T) {
	t.Run("the currency cannot move under recorded charges", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, Card{Code: "NUCRD", Name: "Nubank", Color: "violet",
			Currency: "BRL", Limit: 500000, ClosingDay: 20, DueDay: 28})
		file(t, s, c.ID, 50000)

		c.Currency = "BTC"
		err := s.Update(c)
		if err == nil {
			t.Fatal("the currency was changed under a recorded charge; want it refused")
		}
		if !strings.Contains(err.Error(), "BRL") {
			t.Fatalf("err = %v; want it to name the currency already recorded", err)
		}

		got, err := s.Get(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Currency != "BRL" {
			t.Fatalf("currency = %q; want the refused write to have changed nothing", got.Currency)
		}
	})

	t.Run("the currency moves freely while nothing is charged", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, Card{Code: "NUCRD", Name: "Nubank", Color: "violet",
			Currency: "BRL", Limit: 500000, ClosingDay: 20, DueDay: 28})

		c.Currency = "USD"
		if err := s.Update(c); err != nil {
			t.Fatalf("changing the currency of an unused card: %v", err)
		}
		got, err := s.Get(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Currency != "USD" {
			t.Fatalf("currency = %q; want USD", got.Currency)
		}
	})

	t.Run("everything else still edits with charges on it", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, Card{Code: "NUCRD", Name: "Nubank", Color: "violet",
			Currency: "BRL", Limit: 500000, ClosingDay: 20, DueDay: 28})
		file(t, s, c.ID, 50000)

		c.Name, c.Limit, c.DueDay = "Nubank Ultravioleta", 800000, 25
		if err := s.Update(c); err != nil {
			t.Fatalf("an edit that left the currency alone was refused: %v", err)
		}
	})
}

func TestLinked(t *testing.T) {
	s := newTestStore(t)
	c := mustCreate(t, s, Card{Code: "NUCRD", Name: "Nubank", Color: "violet",
		Currency: "BRL", Limit: 500000, ClosingDay: 20, DueDay: 28})
	if n, err := s.Linked(c.ID); err != nil || n != 0 {
		t.Fatalf("Linked on an unused card = %d, %v; want 0", n, err)
	}
	file(t, s, c.ID, 50000)
	if n, err := s.Linked(c.ID); err != nil || n != 1 {
		t.Fatalf("Linked = %d, %v; want 1", n, err)
	}
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
	t.Run("create, edit and delete each leave one row", func(t *testing.T) {
		s := newTestStore(t)
		c := mustCreate(t, s, nubank())
		c.Limit = 600000
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
		for i, want := range []string{"created", "edited", "deleted"} {
			if es[i].Action != want || es[i].Entity != "card" || es[i].EntityID != c.ID {
				t.Errorf("row %d = %+v; want %s/card/%d", i, es[i], want, c.ID)
			}
		}
		if !strings.Contains(es[1].Changes, `"limit"`) || strings.Contains(es[1].Changes, `"name"`) {
			t.Errorf("changes = %s; want the limit move and nothing else", es[1].Changes)
		}
	})
}
