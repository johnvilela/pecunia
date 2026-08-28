package recurring

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"pecunia/internal/logs"
	"pecunia/internal/transactions"
)

// world is a database with an account, a card and a category to hang a bill
// off, plus the store under test.
type world struct {
	conn     *sql.DB
	store    *Store
	account  int64
	card     int64
	category int64
}

func newWorld(t *testing.T) *world {
	t.Helper()
	conn := newTestDB(t)
	w := &world{conn: conn, store: NewStore(conn), account: seedAccount(t, conn), card: seedCard(t, conn)}
	if err := conn.QueryRow(
		`INSERT INTO categories (code, name, color) VALUES ('UTILS', 'Utilities', 'amber')
		 RETURNING id`).Scan(&w.category); err != nil {
		t.Fatal(err)
	}
	return w
}

// bill is what most cases start from: energy, paid from the account.
func (w *world) bill() Bill {
	return Bill{
		Code: "ENERG", Name: "Energy", Description: "Neoenergia", Color: "amber",
		Expected: 21490, OpenDay: 5, DueDay: 15, Active: true,
		Account: transactions.Ref{ID: w.account}, Category: transactions.Ref{ID: w.category},
		Tags: []string{"home", "fixed"},
	}
}

func (w *world) create(t *testing.T, b Bill) Bill {
	t.Helper()
	if err := w.store.Create(&b); err != nil {
		t.Fatalf("create %s: %v", b.Code, err)
	}
	return b
}

// pay files one payment against a bill, through the transactions store, which
// is the only thing that ever writes one.
func (w *world) pay(t *testing.T, bill Bill, value int64, date, cycle string) {
	t.Helper()
	tr := transactions.Transaction{
		Title: bill.Name, Value: value, Kind: transactions.KindOutcome, Date: date,
		Account: transactions.Ref{ID: w.account}, Recurring: transactions.Ref{ID: bill.ID}, Cycle: cycle,
	}
	if err := transactions.NewStore(w.conn).Create(&tr); err != nil {
		t.Fatalf("pay %s %s: %v", bill.Code, cycle, err)
	}
}

func TestCreateAndGet(t *testing.T) {
	t.Run("round trips everything it was given", func(t *testing.T) {
		w := newWorld(t)
		made := w.create(t, w.bill())
		if made.ID == 0 {
			t.Fatal("create left no id behind")
		}

		got, err := w.store.Get(made.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Code != "ENERG" || got.Name != "Energy" || got.Description != "Neoenergia" {
			t.Errorf("got %+v, want the bill it was given", got)
		}
		if got.Expected != 21490 || got.OpenDay != 5 || got.DueDay != 15 {
			t.Errorf("amount and days = %d, %d, %d — want 21490, 5, 15", got.Expected, got.OpenDay, got.DueDay)
		}
		if got.Account.ID != w.account || got.Account.Code != "INTER" {
			t.Errorf("account = %+v, want INTER joined in", got.Account)
		}
		if got.Category.Code != "UTILS" {
			t.Errorf("category = %+v, want UTILS joined in", got.Category)
		}
		if got.Currency != "BRL" {
			t.Errorf("currency = %q, want BRL — it comes from whatever pays it", got.Currency)
		}
		if strings.Join(got.Tags, ",") != "fixed,home" {
			t.Errorf("tags = %v, want them normalized and sorted", got.Tags)
		}
		if !got.Active {
			t.Error("a new bill must be active")
		}
	})

	t.Run("a card bill takes its currency from the card", func(t *testing.T) {
		w := newWorld(t)
		b := w.bill()
		b.Code, b.Name = "NFLIX", "Netflix"
		b.Account, b.Card = transactions.Ref{}, transactions.Ref{ID: w.card}
		got, err := w.store.Get(w.create(t, b).ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Card.Code != "NUCRD" || got.Currency != "BRL" {
			t.Errorf("card = %+v currency = %q, want NUCRD in BRL", got.Card, got.Currency)
		}
	})

	t.Run("normalizes the code", func(t *testing.T) {
		w := newWorld(t)
		b := w.bill()
		b.Code = " energ "
		if got := w.create(t, b).Code; got != "ENERG" {
			t.Errorf("code = %q, want ENERG", got)
		}
	})

	t.Run("refuses a bill the model would not have", func(t *testing.T) {
		w := newWorld(t)
		b := w.bill()
		b.Name = ""
		if err := w.store.Create(&b); err == nil {
			t.Fatal("a nameless bill was written")
		}
	})

	t.Run("says which code was taken", func(t *testing.T) {
		w := newWorld(t)
		w.create(t, w.bill())
		b := w.bill()
		err := w.store.Create(&b)
		if err == nil || !strings.Contains(err.Error(), "ENERG") {
			t.Fatalf("error = %v, want one naming the code", err)
		}
	})

	t.Run("an unknown id is not found", func(t *testing.T) {
		w := newWorld(t)
		if _, err := w.store.Get(404); !errors.Is(err, ErrNotFound) {
			t.Fatalf("error = %v, want ErrNotFound", err)
		}
	})
}

func TestByCode(t *testing.T) {
	w := newWorld(t)
	made := w.create(t, w.bill())

	t.Run("finds it however it is typed", func(t *testing.T) {
		for _, ref := range []string{"ENERG", "energ", " EnErG "} {
			got, err := w.store.ByCode(ref)
			if err != nil {
				t.Fatalf("%q: %v", ref, err)
			}
			if got.ID != made.ID {
				t.Errorf("%q found bill %d, want %d", ref, got.ID, made.ID)
			}
		}
	})

	t.Run("an unknown code is not found", func(t *testing.T) {
		if _, err := w.store.ByCode("NOPE1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("error = %v, want ErrNotFound", err)
		}
	})
}

func TestListPayments(t *testing.T) {
	t.Run("carries every cycle's payments back with the bill", func(t *testing.T) {
		w := newWorld(t)
		energy := w.create(t, w.bill())
		w.pay(t, energy, 20000, "2026-07-08", "2026-07")
		w.pay(t, energy, 21490, "2026-08-08", "2026-08")
		w.pay(t, energy, 1000, "2026-08-20", "2026-08") // a second slice of the same month

		all, err := w.store.List(false)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 1 {
			t.Fatalf("listed %d bills, want 1", len(all))
		}
		if got := all[0].Payments["2026-07"]; got.Value != 20000 || got.Count != 1 {
			t.Errorf("July = %+v, want 20000 over 1 payment", got)
		}
		if got := all[0].Payments["2026-08"]; got.Value != 22490 || got.Count != 2 {
			t.Errorf("August = %+v, want 22490 over 2 payments", got)
		}
	})

	t.Run("one bill's payments never land on another", func(t *testing.T) {
		w := newWorld(t)
		energy := w.create(t, w.bill())
		other := w.bill()
		other.Code, other.Name = "WATER", "Water"
		water := w.create(t, other)
		w.pay(t, energy, 20000, "2026-08-08", "2026-08")

		all, err := w.store.List(false)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range all {
			if b.ID == water.ID && len(b.Payments) != 0 {
				t.Errorf("water carries %+v, and nothing was paid against it", b.Payments)
			}
		}
	})

	t.Run("Get carries them too", func(t *testing.T) {
		w := newWorld(t)
		energy := w.create(t, w.bill())
		w.pay(t, energy, 20000, "2026-08-08", "2026-08")

		got, err := w.store.Get(energy.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Payments["2026-08"].Count != 1 {
			t.Errorf("payments = %+v, want August's one", got.Payments)
		}
	})

	t.Run("Payments lists the transactions themselves, newest first", func(t *testing.T) {
		w := newWorld(t)
		energy := w.create(t, w.bill())
		w.pay(t, energy, 20000, "2026-07-08", "2026-07")
		w.pay(t, energy, 21490, "2026-08-08", "2026-08")

		ts, err := w.store.Payments(energy.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(ts) != 2 {
			t.Fatalf("listed %d payments, want 2", len(ts))
		}
		if ts[0].Date != "2026-08-08" {
			t.Errorf("first payment is %s, want the newest", ts[0].Date)
		}
	})
}

func TestListActive(t *testing.T) {
	w := newWorld(t)
	w.create(t, w.bill())
	gone := w.bill()
	gone.Code, gone.Name = "NFLIX", "Netflix"
	cancelled := w.create(t, gone)
	if err := w.store.SetActive(cancelled.ID, false); err != nil {
		t.Fatal(err)
	}

	t.Run("leaves an archived bill out", func(t *testing.T) {
		all, err := w.store.List(false)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 1 || all[0].Code != "ENERG" {
			t.Fatalf("listed %d bills, want only ENERG", len(all))
		}
	})

	t.Run("takes it back when asked", func(t *testing.T) {
		all, err := w.store.List(true)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 2 {
			t.Fatalf("listed %d bills, want both", len(all))
		}
	})

	t.Run("an archived bill is still readable", func(t *testing.T) {
		got, err := w.store.Get(cancelled.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Active {
			t.Error("the bill is still active after being archived")
		}
	})
}

func TestUpdate(t *testing.T) {
	t.Run("writes the new bill and its tags", func(t *testing.T) {
		w := newWorld(t)
		made := w.create(t, w.bill())
		made.Name, made.Expected, made.DueDay = "Energia", 25000, 20
		made.Tags = []string{"home"}
		if err := w.store.Update(made); err != nil {
			t.Fatal(err)
		}

		got, err := w.store.Get(made.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "Energia" || got.Expected != 25000 || got.DueDay != 20 {
			t.Errorf("got %+v, want the edit written through", got)
		}
		if strings.Join(got.Tags, ",") != "home" {
			t.Errorf("tags = %v, want the old ones replaced", got.Tags)
		}
	})

	t.Run("moving a bill to a card takes the account off", func(t *testing.T) {
		w := newWorld(t)
		made := w.create(t, w.bill())
		made.Account, made.Card = transactions.Ref{}, transactions.Ref{ID: w.card}
		if err := w.store.Update(made); err != nil {
			t.Fatal(err)
		}
		got, err := w.store.Get(made.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Account.ID != 0 || got.Card.ID != w.card {
			t.Errorf("source = account %d card %d, want the card alone", got.Account.ID, got.Card.ID)
		}
	})

	t.Run("an unknown bill is not found", func(t *testing.T) {
		w := newWorld(t)
		b := w.bill()
		b.ID = 404
		if err := w.store.Update(b); !errors.Is(err, ErrNotFound) {
			t.Fatalf("error = %v, want ErrNotFound", err)
		}
	})
}

func TestDelete(t *testing.T) {
	t.Run("unlinks the payments instead of taking them with it", func(t *testing.T) {
		w := newWorld(t)
		energy := w.create(t, w.bill())
		w.pay(t, energy, 20000, "2026-08-08", "2026-08")

		n, err := w.store.Linked(energy.ID)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("linked = %d, want 1", n)
		}
		if err := w.store.Delete(energy.ID); err != nil {
			t.Fatal(err)
		}

		left, err := transactions.NewStore(w.conn).List(transactions.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(left) != 1 {
			t.Fatalf("%d transactions left, want the payment to have survived", len(left))
		}
		if left[0].Recurring.ID != 0 || left[0].Cycle != "" {
			t.Errorf("payment still names bill %d cycle %q", left[0].Recurring.ID, left[0].Cycle)
		}
	})

	t.Run("an unknown bill is not found", func(t *testing.T) {
		w := newWorld(t)
		if err := w.store.Delete(404); !errors.Is(err, ErrNotFound) {
			t.Fatalf("error = %v, want ErrNotFound", err)
		}
	})
}

func TestCodeTaken(t *testing.T) {
	w := newWorld(t)
	w.create(t, w.bill())

	taken, err := w.store.CodeTaken("ENERG")
	if err != nil || !taken {
		t.Errorf("ENERG taken = %v, %v — want true", taken, err)
	}
	taken, err = w.store.CodeTaken("WATER")
	if err != nil || taken {
		t.Errorf("WATER taken = %v, %v — want false", taken, err)
	}
}

// audit is every trail row for recurring bills so far, oldest first.
func (w *world) audit(t *testing.T) []logs.Entry {
	t.Helper()
	es, err := logs.List(w.conn, logs.Filter{Entity: "recurring", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(es)-1; i < j; i, j = i+1, j-1 {
		es[i], es[j] = es[j], es[i]
	}
	return es
}

func TestAuditTrail(t *testing.T) {
	t.Run("create, edit, archive and delete each leave one row", func(t *testing.T) {
		w := newWorld(t)
		b := w.create(t, w.bill())
		b.Expected = 23990
		if err := w.store.Update(b); err != nil {
			t.Fatal(err)
		}
		if err := w.store.SetActive(b.ID, false); err != nil {
			t.Fatal(err)
		}
		if err := w.store.Delete(b.ID); err != nil {
			t.Fatal(err)
		}

		es := w.audit(t)
		if len(es) != 4 {
			t.Fatalf("trail has %d rows; want 4", len(es))
		}
		for i, want := range []string{"created", "edited", "edited", "deleted"} {
			if es[i].Action != want {
				t.Fatalf("row %d = %+v; want %s", i, es[i], want)
			}
		}
		if !strings.Contains(es[1].Changes, `"expected"`) || strings.Contains(es[1].Changes, `"name"`) {
			t.Errorf("edit changes = %s; want the expected move alone", es[1].Changes)
		}
		if !strings.Contains(es[2].Changes, `"active":{"old":true,"new":false}`) {
			t.Errorf("archive changes = %s; want the active flip", es[2].Changes)
		}
	})

	t.Run("archiving an archived bill records nothing", func(t *testing.T) {
		w := newWorld(t)
		b := w.create(t, w.bill())
		if err := w.store.SetActive(b.ID, false); err != nil {
			t.Fatal(err)
		}
		if err := w.store.SetActive(b.ID, false); err != nil {
			t.Fatal(err)
		}
		if es := w.audit(t); len(es) != 2 {
			t.Fatalf("trail has %d rows after a no-op archive; want 2", len(es))
		}
	})
}
