package bills

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"pecunia/internal/cards"
	"pecunia/internal/db"
	"pecunia/internal/logs"
)

// newTestStore gives the caller its own SQLite file, its own card, and a clock
// it controls. Call it inside the subtest, not the parent, or the cases go back
// to sharing one database.
//
// The clock is pinned because every one of these tests is about dates: with the
// real one, "the bill that is still open" would be a different row each month.
func newTestStore(t *testing.T, today string, closingDay, dueDay int) (*Store, cards.Card) {
	t.Helper()
	t.Setenv("PECUNIA_DB", filepath.Join(t.TempDir(), "pecunia.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	c := card(closingDay, dueDay)
	c.ID = 0
	if err := cards.NewStore(conn).Create(&c); err != nil {
		t.Fatal(err)
	}
	s := NewStore(conn)
	s.now = func() time.Time { return mustDate(t, today) }
	return s, c
}

// charge writes a card transaction without going through the transactions
// package, which imports this one.
func charge(t *testing.T, s *Store, c cards.Card, date, title string, value int64, kind string) int64 {
	t.Helper()
	res, err := s.db.Exec(
		`INSERT INTO transactions (title, card_id, value, kind, date) VALUES (?, ?, ?, ?, ?)`,
		title, c.ID, value, kind, date)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// payment writes an account outcome that names a bill — what `pecunia cc pay`
// produces.
func payment(t *testing.T, s *Store, billID, value int64, date string) {
	t.Helper()
	var accountID int64
	if err := s.db.QueryRow(`SELECT id FROM accounts WHERE code = 'INTER'`).Scan(&accountID); errors.Is(err, sql.ErrNoRows) {
		res, err := s.db.Exec(
			`INSERT INTO accounts (code, name, color, currency) VALUES ('INTER', 'Inter', 'orange', 'BRL')`)
		if err != nil {
			t.Fatal(err)
		}
		if accountID, err = res.LastInsertId(); err != nil {
			t.Fatal(err)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO transactions (title, account_id, value, kind, date, pays_bill_id)
		 VALUES ('Bill NUCRD', ?, ?, 'outcome', ?, ?)`, accountID, value, date, billID); err != nil {
		t.Fatal(err)
	}
	if err := Refresh(s.db, billID); err != nil {
		t.Fatal(err)
	}
}

func closingDates(t *testing.T, bs []Bill) []string {
	t.Helper()
	var out []string
	for _, b := range bs {
		out = append(out, b.ClosesOn)
	}
	return out
}

func TestEnsure(t *testing.T) {
	t.Run("a card with no transactions gets only its open bill", func(t *testing.T) {
		s, c := newTestStore(t, "2026-08-06", 10, 20)
		got, err := s.List(c)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].ClosesOn != "2026-08-10" || got[0].Status != StatusOpen {
			t.Fatalf("List = %+v; want one open bill closing 2026-08-10", got)
		}
		if got[0].DueOn != "2026-08-20" {
			t.Fatalf("due = %s; want 2026-08-20", got[0].DueOn)
		}
	})

	t.Run("fills every cycle from the first transaction to the open one", func(t *testing.T) {
		s, c := newTestStore(t, "2026-08-06", 10, 20)
		charge(t, s, c, "2026-05-20", "Groceries", 12000, "outcome")

		got, err := s.List(c)
		if err != nil {
			t.Fatal(err)
		}
		// Newest first. 2026-05-20 falls in the cycle closing 2026-06-10.
		want := []string{"2026-08-10", "2026-07-10", "2026-06-10"}
		if dates := closingDates(t, got); len(dates) != 3 ||
			dates[0] != want[0] || dates[1] != want[1] || dates[2] != want[2] {
			t.Fatalf("closing dates = %v; want %v", dates, want)
		}
	})

	t.Run("running twice writes nothing new", func(t *testing.T) {
		s, c := newTestStore(t, "2026-08-06", 10, 20)
		charge(t, s, c, "2026-05-20", "Groceries", 12000, "outcome")

		first, err := s.List(c)
		if err != nil {
			t.Fatal(err)
		}
		second, err := s.List(c)
		if err != nil {
			t.Fatal(err)
		}
		if len(first) != len(second) {
			t.Fatalf("%d bills became %d on the second read", len(first), len(second))
		}
		for i := range first {
			if first[i].ID != second[i].ID {
				t.Fatalf("bill %d changed id from %d to %d", i, first[i].ID, second[i].ID)
			}
		}
	})

	t.Run("a card closing on the 31st still tiles the months", func(t *testing.T) {
		s, c := newTestStore(t, "2026-04-06", 31, 10)
		charge(t, s, c, "2026-01-15", "Groceries", 12000, "outcome")

		got, err := s.List(c)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"2026-04-30", "2026-03-31", "2026-02-28", "2026-01-31"}
		dates := closingDates(t, got)
		if len(dates) != len(want) {
			t.Fatalf("closing dates = %v; want %v", dates, want)
		}
		for i := range want {
			if dates[i] != want[i] {
				t.Fatalf("closing dates = %v; want %v", dates, want)
			}
		}
	})
}

func TestTotals(t *testing.T) {
	t.Run("a charge lands in the cycle its date falls in", func(t *testing.T) {
		s, c := newTestStore(t, "2026-08-06", 10, 20)
		charge(t, s, c, "2026-07-11", "First day of the cycle", 10000, "outcome")
		charge(t, s, c, "2026-08-10", "Last day of the cycle", 20000, "outcome")
		charge(t, s, c, "2026-07-10", "The cycle before", 50000, "outcome")

		got, err := s.List(c)
		if err != nil {
			t.Fatal(err)
		}
		byDate := map[string]int64{}
		for _, b := range got {
			byDate[b.ClosesOn] = b.Total
		}
		if byDate["2026-08-10"] != 30000 {
			t.Errorf("the open bill totals %d; want 30000", byDate["2026-08-10"])
		}
		if byDate["2026-07-10"] != 50000 {
			t.Errorf("the closed bill totals %d; want 50000", byDate["2026-07-10"])
		}
	})

	t.Run("an income on the card is a credit against the bill", func(t *testing.T) {
		s, c := newTestStore(t, "2026-08-06", 10, 20)
		charge(t, s, c, "2026-08-01", "Phone", 100000, "outcome")
		charge(t, s, c, "2026-08-02", "Refunded", 30000, "income")

		open, err := s.Open(c)
		if err != nil {
			t.Fatal(err)
		}
		if open.Total != 70000 {
			t.Fatalf("total = %d; want 70000", open.Total)
		}
	})

	t.Run("a payment is not a charge on any bill", func(t *testing.T) {
		// The payment is an account transaction, so it can never show up as
		// spending on the card that would inflate the next bill.
		s, c := newTestStore(t, "2026-08-06", 10, 20)
		charge(t, s, c, "2026-07-05", "Groceries", 89050, "outcome")

		july, err := s.Get(c, "2026-07-10")
		if err != nil {
			t.Fatal(err)
		}
		payment(t, s, july.ID, 89050, "2026-07-20")

		open, err := s.Open(c)
		if err != nil {
			t.Fatal(err)
		}
		if open.Total != 0 {
			t.Fatalf("the open bill totals %d; a payment leaked into it", open.Total)
		}
		charges, err := s.Charges(open)
		if err != nil {
			t.Fatal(err)
		}
		if len(charges) != 0 {
			t.Fatalf("the open bill lists %+v; want nothing", charges)
		}
	})

	t.Run("an open total follows the ledger; a closed one freezes", func(t *testing.T) {
		s, c := newTestStore(t, "2026-08-06", 10, 20)
		charge(t, s, c, "2026-08-01", "Groceries", 10000, "outcome")
		if _, err := s.List(c); err != nil {
			t.Fatal(err)
		}

		// Still open: the total follows.
		charge(t, s, c, "2026-08-02", "More groceries", 5000, "outcome")
		open, err := s.Open(c)
		if err != nil {
			t.Fatal(err)
		}
		if open.Total != 15000 {
			t.Fatalf("the open total = %d; want 15000", open.Total)
		}

		// Once the cycle closes the snapshot is taken and stops moving, which is
		// the trade the stored total buys.
		s.now = func() time.Time { return mustDate(t, "2026-08-20") }
		if _, err := s.List(c); err != nil {
			t.Fatal(err)
		}
		charge(t, s, c, "2026-08-03", "Backdated", 90000, "outcome")
		closed, err := s.Get(c, "2026-08-10")
		if err != nil {
			t.Fatal(err)
		}
		if closed.Status != StatusClosed {
			t.Fatalf("status = %q; want closed", closed.Status)
		}
		if closed.Total != 15000 {
			t.Fatalf("the closed total = %d; want it frozen at 15000", closed.Total)
		}
		live, err := s.LiveTotal(closed)
		if err != nil {
			t.Fatal(err)
		}
		if live != 105000 {
			t.Fatalf("LiveTotal = %d; want 105000 so the drift can be shown", live)
		}
	})
}

func TestPaymentsMoveTheStatus(t *testing.T) {
	setup := func(t *testing.T) (*Store, cards.Card, Bill) {
		t.Helper()
		s, c := newTestStore(t, "2026-08-06", 10, 20)
		charge(t, s, c, "2026-07-05", "Groceries", 89050, "outcome")
		s.now = func() time.Time { return mustDate(t, "2026-08-06") }
		if _, err := s.List(c); err != nil {
			t.Fatal(err)
		}
		b, err := s.Get(c, "2026-07-10")
		if err != nil {
			t.Fatal(err)
		}
		return s, c, b
	}

	t.Run("part of it is partial", func(t *testing.T) {
		s, c, b := setup(t)
		payment(t, s, b.ID, 40000, "2026-07-20")

		got, err := s.Get(c, "2026-07-10")
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != StatusPartial || got.Paid != 40000 || got.Remaining() != 49050 {
			t.Fatalf("got %s, paid %d, %d left; want partial, 40000, 49050",
				got.Status, got.Paid, got.Remaining())
		}
	})

	t.Run("the rest of it is paid", func(t *testing.T) {
		s, c, b := setup(t)
		payment(t, s, b.ID, 40000, "2026-07-20")
		payment(t, s, b.ID, 49050, "2026-07-21")

		got, err := s.Get(c, "2026-07-10")
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != StatusPaid || got.Remaining() != 0 {
			t.Fatalf("got %s with %d left; want paid with nothing left", got.Status, got.Remaining())
		}
	})

	t.Run("taking the payment away puts it back", func(t *testing.T) {
		s, c, b := setup(t)
		payment(t, s, b.ID, 89050, "2026-07-20")
		if _, err := s.db.Exec(`DELETE FROM transactions WHERE pays_bill_id = ?`, b.ID); err != nil {
			t.Fatal(err)
		}
		if err := Refresh(s.db, b.ID); err != nil {
			t.Fatal(err)
		}

		got, err := s.Get(c, "2026-07-10")
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != StatusClosed || got.Paid != 0 {
			t.Fatalf("got %s, paid %d; want closed and nothing paid", got.Status, got.Paid)
		}
	})
}

func TestOldestUnpaid(t *testing.T) {
	t.Run("picks the earliest bill still owing", func(t *testing.T) {
		s, c := newTestStore(t, "2026-08-06", 10, 20)
		charge(t, s, c, "2026-06-05", "June", 10000, "outcome")
		charge(t, s, c, "2026-07-05", "July", 20000, "outcome")
		if _, err := s.List(c); err != nil {
			t.Fatal(err)
		}

		june, err := s.Get(c, "2026-06-10")
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.OldestUnpaid(c)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != june.ID {
			t.Fatalf("OldestUnpaid closes %s; want %s", got.ClosesOn, june.ClosesOn)
		}

		payment(t, s, june.ID, 10000, "2026-06-20")
		got, err = s.OldestUnpaid(c)
		if err != nil {
			t.Fatal(err)
		}
		if got.ClosesOn != "2026-07-10" {
			t.Fatalf("after paying June, OldestUnpaid closes %s; want 2026-07-10", got.ClosesOn)
		}
	})

	t.Run("nothing owing is ErrNotFound", func(t *testing.T) {
		s, c := newTestStore(t, "2026-08-06", 10, 20)
		if _, err := s.OldestUnpaid(c); !errors.Is(err, ErrNotFound) {
			t.Fatalf("OldestUnpaid with nothing owing = %v; want ErrNotFound", err)
		}
	})
}

func TestCharges(t *testing.T) {
	s, c := newTestStore(t, "2026-08-06", 10, 20)
	charge(t, s, c, "2026-08-02", "Coffee", 1500, "outcome")
	charge(t, s, c, "2026-08-01", "Phone", 20000, "outcome")

	open, err := s.Open(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Charges(open)
	if err != nil {
		t.Fatal(err)
	}
	// Oldest first: a bill reads like a statement, not like the transaction list.
	if len(got) != 2 || got[0].Title != "Phone" || got[1].Title != "Coffee" {
		t.Fatalf("Charges = %+v; want Phone then Coffee", got)
	}
	if got[0].Value != 20000 || got[0].Kind != "outcome" {
		t.Fatalf("first charge = %+v", got[0])
	}
}

func TestGetMissing(t *testing.T) {
	s, c := newTestStore(t, "2026-08-06", 10, 20)
	if _, err := s.Get(c, "1999-01-10"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get of a cycle that never existed = %v; want ErrNotFound", err)
	}
}

func TestUnpaidLeavesTheOpenBillOut(t *testing.T) {
	// Settling a total that is still moving is not paying a bill, so the open one
	// is never offered — even when it already has charges on it.
	s, c := newTestStore(t, "2026-08-06", 10, 20)
	charge(t, s, c, "2026-08-01", "This month", 31200, "outcome")
	charge(t, s, c, "2026-07-05", "Last month", 89050, "outcome")

	owing, err := s.Unpaid(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(owing) != 1 || owing[0].ClosesOn != "2026-07-10" {
		t.Fatalf("Unpaid = %v; want only the closed July bill", closingDates(t, owing))
	}
}

// The clock is a field so a test can pin it, but only this package could reach
// it. A caller that has already decided its own today — a summary asks every
// module the same date — has to be able to hand it over, or the one answer
// coming from the wall clock is the one that disagrees.
func TestNewStoreAt(t *testing.T) {
	t.Run("takes the clock it is given", func(t *testing.T) {
		t.Setenv("PECUNIA_DB", filepath.Join(t.TempDir(), "pecunia.db"))
		conn, err := db.Open()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })

		c := card(10, 20)
		c.ID = 0
		if err := cards.NewStore(conn).Create(&c); err != nil {
			t.Fatal(err)
		}

		s := NewStoreAt(conn, func() time.Time { return mustDate(t, "2026-08-06") })
		charge(t, s, c, "2026-07-05", "Groceries", 89050, "outcome")

		all, err := s.List(c)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) == 0 {
			t.Fatal("no bills at all — the card should have July's and August's by now")
		}
		for _, b := range all {
			// August 10th is the cycle still taking charges on the 6th; anything
			// past it was invented against a clock nobody asked for.
			if b.ClosesOn > "2026-08-10" {
				t.Errorf("a bill closing on %s exists; want nothing past the clock it was given",
					b.ClosesOn)
			}
		}
	})
}

func TestAuditTrail(t *testing.T) {
	t.Run("a generated bill is logged as the system's doing, once", func(t *testing.T) {
		s, c := newTestStore(t, "2026-08-20", 15, 22)
		charge(t, s, c, "2026-06-10", "Groceries", 12000, "outcome")

		all, err := s.List(c)
		if err != nil {
			t.Fatal(err)
		}
		es, err := logs.List(s.db, logs.Filter{Entity: "card_bill", Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		if len(es) != len(all) {
			t.Fatalf("trail has %d card_bill rows for %d bills; want one each", len(es), len(all))
		}
		for _, e := range es {
			if e.Source != logs.System || e.Action != "created" {
				t.Fatalf("row = %+v; want system/created", e)
			}
		}

		// A second read generates nothing, so it logs nothing.
		if _, err := s.List(c); err != nil {
			t.Fatal(err)
		}
		again, err := logs.List(s.db, logs.Filter{Entity: "card_bill", Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		if len(again) != len(es) {
			t.Fatalf("trail grew from %d to %d rows on a plain read; want no change", len(es), len(again))
		}
	})

	t.Run("a payment refreshing totals logs nothing", func(t *testing.T) {
		s, c := newTestStore(t, "2026-08-20", 15, 22)
		charge(t, s, c, "2026-07-10", "Groceries", 12000, "outcome")
		bill, err := s.OldestUnpaid(c)
		if err != nil {
			t.Fatal(err)
		}
		before, err := logs.List(s.db, logs.Filter{Entity: "card_bill", Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		payment(t, s, bill.ID, 12000, "2026-08-16")
		after, err := logs.List(s.db, logs.Filter{Entity: "card_bill", Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != len(before) {
			t.Fatalf("trail grew from %d to %d rows on a payment; want no change", len(before), len(after))
		}
	})
}

// stamp marks every bill's updated_at with a sentinel, so any later write —
// datetime('now') — stands out regardless of clock granularity.
func stamp(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE card_bills SET updated_at = '2000-01-01'`); err != nil {
		t.Fatal(err)
	}
}

// stamped returns how many bills still carry the sentinel.
func stamped(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(
		`SELECT count(*) FROM card_bills WHERE updated_at = '2000-01-01'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestReadsWriteOnlyWhenStale(t *testing.T) {
	t.Run("a read that finds nothing stale writes nothing", func(t *testing.T) {
		s, c := newTestStore(t, "2026-08-20", 15, 22)
		charge(t, s, c, "2026-06-10", "Groceries", 12000, "outcome")

		all, err := s.List(c)
		if err != nil {
			t.Fatal(err)
		}
		stamp(t, s)
		if _, err := s.List(c); err != nil {
			t.Fatal(err)
		}
		if n := stamped(t, s); n != len(all) {
			t.Fatalf("%d of %d bills kept the sentinel; want a clean read to write nothing", n, len(all))
		}
	})

	t.Run("a stale total is written once, and only that bill", func(t *testing.T) {
		s, c := newTestStore(t, "2026-08-20", 15, 22)
		charge(t, s, c, "2026-06-10", "Groceries", 12000, "outcome")
		all, err := s.List(c)
		if err != nil {
			t.Fatal(err)
		}
		stamp(t, s)

		// Straight past the store, the way a bug or another writer would.
		charge(t, s, c, "2026-08-18", "Padaria", 3000, "outcome")
		if _, err := s.List(c); err != nil {
			t.Fatal(err)
		}
		if n := stamped(t, s); n != len(all)-1 {
			t.Fatalf("%d of %d bills kept the sentinel; want only the stale one rewritten", n, len(all))
		}
		open, err := s.Open(c)
		if err != nil {
			t.Fatal(err)
		}
		if open.Total != 3000 {
			t.Fatalf("open total = %d; want the catch-up to have happened", open.Total)
		}

		stamp(t, s)
		if _, err := s.List(c); err != nil {
			t.Fatal(err)
		}
		if n := stamped(t, s); n != len(all) {
			t.Fatalf("%d of %d bills kept the sentinel; want the second read to write nothing", n, len(all))
		}
	})

	t.Run("time passing writes the flip once", func(t *testing.T) {
		s, c := newTestStore(t, "2026-08-20", 15, 22)
		charge(t, s, c, "2026-08-10", "Groceries", 12000, "outcome")
		if _, err := s.List(c); err != nil {
			t.Fatal(err)
		}
		stamp(t, s)

		s.now = func() time.Time { return mustDate(t, "2026-09-16") }
		all, err := s.List(c)
		if err != nil {
			t.Fatal(err)
		}
		var flipped Bill
		for _, b := range all {
			if b.ClosesOn == "2026-09-15" {
				flipped = b
			}
		}
		if flipped.Status == StatusOpen || flipped.Status == "" {
			t.Fatalf("the passed cycle is %q; want it closed by the read", flipped.Status)
		}

		stamp(t, s)
		if _, err := s.List(c); err != nil {
			t.Fatal(err)
		}
		if n := stamped(t, s); n != len(all) {
			t.Fatalf("%d of %d bills kept the sentinel; want the read after the flip to write nothing", n, len(all))
		}
	})
}
