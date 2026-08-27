package transactions

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"kakei/internal/accounts"
	"kakei/internal/cards"
	"kakei/internal/categories"
	"kakei/internal/logs"
)

// world is a database with something of everything to file a transaction
// against, plus the stores to read the balances back out of.
type world struct {
	conn     *sql.DB
	store    *Store
	accounts *accounts.Store
	cards    *cards.Store
	inter    accounts.Account // BRL 1000.00
	cash     accounts.Account // BRL 150.00
	nucrd    cards.Card       // limit 5000.00, owes nothing, declines at the limit
	itau     cards.Card       // limit 3000.00, owes nothing, may go over
	food     categories.Category
	work     categories.Category
}

func newWorld(t *testing.T) *world {
	t.Helper()
	conn := newTestDB(t)
	w := &world{
		conn:     conn,
		store:    NewStore(conn),
		accounts: accounts.NewStore(conn),
		cards:    cards.NewStore(conn),
	}
	w.inter = accounts.Account{Code: "INTER", Name: "Banco Inter", Color: "orange", Currency: "BRL", Balance: 100000}
	w.cash = accounts.Account{Code: "CASH1", Name: "Carteira", Color: "green", Currency: "BRL", Balance: 15000}
	for _, a := range []*accounts.Account{&w.inter, &w.cash} {
		if err := w.accounts.Create(a); err != nil {
			t.Fatal(err)
		}
	}
	w.nucrd = cards.Card{Code: "NUCRD", Name: "Nubank", Color: "violet", Currency: "BRL",
		Limit: 500000, ClosingDay: 15, DueDay: 22}
	w.itau = cards.Card{Code: "ITAU1", Name: "Itau", Color: "orange", Currency: "BRL",
		Limit: 300000, ClosingDay: 1, DueDay: 8, OverLimitAllowed: true}
	for _, c := range []*cards.Card{&w.nucrd, &w.itau} {
		if err := w.cards.Create(c); err != nil {
			t.Fatal(err)
		}
	}
	w.food = categories.Category{Code: "FOOD1", Name: "Food", Color: "lime"}
	w.work = categories.Category{Code: "WORK1", Name: "Work", Color: "indigo"}
	cs := categories.NewStore(conn)
	for _, c := range []*categories.Category{&w.food, &w.work} {
		if err := cs.Create(c, logs.User); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// tx is the transaction most cases start from: BRL 120.00 spent from INTER.
func (w *world) tx() Transaction {
	return Transaction{
		Title:    "Groceries",
		Value:    12000,
		Kind:     KindOutcome,
		Date:     "2026-08-08",
		Account:  Ref{ID: w.inter.ID},
		Category: Ref{ID: w.food.ID},
	}
}

func (w *world) create(t *testing.T, tr Transaction) Transaction {
	t.Helper()
	if err := w.store.Create(&tr); err != nil {
		t.Fatalf("create %q: %v", tr.Title, err)
	}
	return tr
}

func (w *world) accountBalance(t *testing.T, id int64) int64 {
	t.Helper()
	a, err := w.accounts.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return a.Balance
}

func (w *world) cardBalance(t *testing.T, id int64) int64 {
	t.Helper()
	c, err := w.cards.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return c.Balance
}

func (w *world) count(t *testing.T) int {
	t.Helper()
	var n int
	if err := w.conn.QueryRow(`SELECT count(*) FROM transactions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateAndGet(t *testing.T) {
	t.Run("a round trip carries the tags and every joined reference", func(t *testing.T) {
		w := newWorld(t)
		made := w.create(t, func() Transaction {
			tr := w.tx()
			tr.Description = "supermarket"
			tr.Tags = []string{"Food", "weekly"}
			return tr
		}())
		if made.ID == 0 {
			t.Fatal("Create left the id at zero")
		}

		got, err := w.store.Get(made.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Title != "Groceries" || got.Description != "supermarket" {
			t.Fatalf("got %+v", got)
		}
		if got.Value != 12000 || got.Kind != KindOutcome || got.Date != "2026-08-08" {
			t.Fatalf("got %+v", got)
		}
		if got.Account.Code != "INTER" || got.Account.Name != "Banco Inter" || got.Account.Color != "orange" {
			t.Fatalf("account ref = %+v; want it filled in from the join", got.Account)
		}
		if got.Card.ID != 0 {
			t.Fatalf("card ref = %+v; want it empty", got.Card)
		}
		if got.Category.Code != "FOOD1" || got.Category.Color != "lime" {
			t.Fatalf("category ref = %+v; want it filled in from the join", got.Category)
		}
		if got.Currency != "BRL" {
			t.Fatalf("currency = %q; want it inherited from the account", got.Currency)
		}
		if strings.Join(got.Tags, ",") != "food,weekly" {
			t.Fatalf("tags = %q; want them normalized and sorted", got.Tags)
		}
		if got.CreatedAt == "" || got.UpdatedAt == "" {
			t.Fatalf("timestamps = %q / %q", got.CreatedAt, got.UpdatedAt)
		}
	})

	t.Run("a card transaction inherits the card's currency", func(t *testing.T) {
		w := newWorld(t)
		tr := w.tx()
		tr.Account = Ref{}
		tr.Card = Ref{ID: w.nucrd.ID}
		made := w.create(t, tr)

		got, err := w.store.Get(made.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !got.IsCard() || got.Card.Code != "NUCRD" || got.Currency != "BRL" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("a transaction may have no category", func(t *testing.T) {
		w := newWorld(t)
		tr := w.tx()
		tr.Category = Ref{}
		made := w.create(t, tr)

		got, err := w.store.Get(made.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Category.ID != 0 || got.Category.Code != "" {
			t.Fatalf("category = %+v; want it empty", got.Category)
		}
	})

	t.Run("an unknown id is not found", func(t *testing.T) {
		w := newWorld(t)
		if _, err := w.store.Get(999); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get(999) = %v; want ErrNotFound", err)
		}
	})

	t.Run("a broken transaction never reaches the database", func(t *testing.T) {
		w := newWorld(t)
		tr := w.tx()
		tr.Title = "   "
		if err := w.store.Create(&tr); err == nil {
			t.Fatal("Create with a blank title = nil; want the store to refuse it")
		}
		if n := w.count(t); n != 0 {
			t.Fatalf("%d row(s) written; want none", n)
		}
	})
}

func TestBalanceOnCreate(t *testing.T) {
	t.Run("an outcome lowers the account", func(t *testing.T) {
		w := newWorld(t)
		w.create(t, w.tx())
		if got := w.accountBalance(t, w.inter.ID); got != 100000-12000 {
			t.Fatalf("balance = %d; want %d", got, 100000-12000)
		}
	})

	t.Run("an income raises the account", func(t *testing.T) {
		w := newWorld(t)
		tr := w.tx()
		tr.Kind = KindIncome
		w.create(t, tr)
		if got := w.accountBalance(t, w.inter.ID); got != 100000+12000 {
			t.Fatalf("balance = %d; want %d", got, 100000+12000)
		}
	})

	t.Run("an outcome raises what the card owes", func(t *testing.T) {
		w := newWorld(t)
		tr := w.tx()
		tr.Account, tr.Card = Ref{}, Ref{ID: w.nucrd.ID}
		w.create(t, tr)
		// A card's balance is debt: spending on it is not the same direction as
		// spending from an account.
		if got := w.cardBalance(t, w.nucrd.ID); got != 12000 {
			t.Fatalf("card balance = %d; want 12000", got)
		}
	})

	t.Run("an income lowers what the card owes", func(t *testing.T) {
		w := newWorld(t)
		spend := w.tx()
		spend.Account, spend.Card = Ref{}, Ref{ID: w.nucrd.ID}
		spend.Value = 50000
		w.create(t, spend)

		pay := w.tx()
		pay.Account, pay.Card = Ref{}, Ref{ID: w.nucrd.ID}
		pay.Title, pay.Kind, pay.Value = "Paid the bill", KindIncome, 20000
		w.create(t, pay)

		if got := w.cardBalance(t, w.nucrd.ID); got != 30000 {
			t.Fatalf("card balance = %d; want 30000", got)
		}
	})

	t.Run("an account may be taken negative", func(t *testing.T) {
		w := newWorld(t)
		tr := w.tx()
		tr.Account = Ref{ID: w.cash.ID}
		tr.Value = 20000 // more than the 150.00 in it
		w.create(t, tr)
		if got := w.accountBalance(t, w.cash.ID); got != 15000-20000 {
			t.Fatalf("balance = %d; want it overdrawn at %d", got, 15000-20000)
		}
	})
}

func TestCardLimit(t *testing.T) {
	t.Run("a card that declines at its limit refuses the transaction whole", func(t *testing.T) {
		w := newWorld(t)
		tr := w.tx()
		tr.Account, tr.Card = Ref{}, Ref{ID: w.nucrd.ID}
		tr.Value = 600000 // over the 5000.00 limit

		err := w.store.Create(&tr)
		if err == nil || !strings.Contains(err.Error(), "over the") {
			t.Fatalf("create past the limit = %v; want the card's own refusal", err)
		}
		if n := w.count(t); n != 0 {
			t.Fatalf("%d row(s) survived the refusal; want none", n)
		}
		if got := w.cardBalance(t, w.nucrd.ID); got != 0 {
			t.Fatalf("card balance = %d after the refusal; want it untouched at 0", got)
		}
	})

	t.Run("a card allowed over its limit takes it", func(t *testing.T) {
		w := newWorld(t)
		tr := w.tx()
		tr.Account, tr.Card = Ref{}, Ref{ID: w.itau.ID}
		tr.Value = 400000 // over the 3000.00 limit, which this card allows
		w.create(t, tr)
		if got := w.cardBalance(t, w.itau.ID); got != 400000 {
			t.Fatalf("card balance = %d; want 400000", got)
		}
	})

	t.Run("an edit that would breach the limit is refused whole", func(t *testing.T) {
		w := newWorld(t)
		tr := w.tx()
		tr.Account, tr.Card = Ref{}, Ref{ID: w.nucrd.ID}
		made := w.create(t, tr)

		made.Value = 600000
		if err := w.store.Update(made, ScopeOne); err == nil {
			t.Fatal("update past the limit = nil; want the card's refusal")
		}
		got, err := w.store.Get(made.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Value != 12000 {
			t.Fatalf("value = %d after the refusal; want it unchanged at 12000", got.Value)
		}
		if b := w.cardBalance(t, w.nucrd.ID); b != 12000 {
			t.Fatalf("card balance = %d after the refusal; want it unchanged at 12000", b)
		}
	})
}

func TestBalanceOnUpdate(t *testing.T) {
	t.Run("changing the value moves the account by the difference", func(t *testing.T) {
		w := newWorld(t)
		made := w.create(t, w.tx())
		made.Value = 20000
		if err := w.store.Update(made, ScopeOne); err != nil {
			t.Fatal(err)
		}
		if got := w.accountBalance(t, w.inter.ID); got != 100000-20000 {
			t.Fatalf("balance = %d; want %d", got, 100000-20000)
		}
	})

	t.Run("flipping the kind moves the account by twice the value", func(t *testing.T) {
		w := newWorld(t)
		made := w.create(t, w.tx())
		made.Kind = KindIncome
		if err := w.store.Update(made, ScopeOne); err != nil {
			t.Fatal(err)
		}
		if got := w.accountBalance(t, w.inter.ID); got != 100000+12000 {
			t.Fatalf("balance = %d; want %d", got, 100000+12000)
		}
	})

	t.Run("moving it to another account puts both right", func(t *testing.T) {
		w := newWorld(t)
		made := w.create(t, w.tx())
		made.Account = Ref{ID: w.cash.ID}
		if err := w.store.Update(made, ScopeOne); err != nil {
			t.Fatal(err)
		}
		if got := w.accountBalance(t, w.inter.ID); got != 100000 {
			t.Fatalf("the old account is at %d; want it back at 100000", got)
		}
		if got := w.accountBalance(t, w.cash.ID); got != 15000-12000 {
			t.Fatalf("the new account is at %d; want %d", got, 15000-12000)
		}
	})

	t.Run("moving it from an account to a card puts both right", func(t *testing.T) {
		w := newWorld(t)
		made := w.create(t, w.tx())
		made.Account, made.Card = Ref{}, Ref{ID: w.nucrd.ID}
		if err := w.store.Update(made, ScopeOne); err != nil {
			t.Fatal(err)
		}
		if got := w.accountBalance(t, w.inter.ID); got != 100000 {
			t.Fatalf("the account is at %d; want it back at 100000", got)
		}
		// And in the card's own direction, not the account's.
		if got := w.cardBalance(t, w.nucrd.ID); got != 12000 {
			t.Fatalf("the card is at %d; want 12000", got)
		}
	})

	t.Run("the tags are replaced, not added to", func(t *testing.T) {
		w := newWorld(t)
		tr := w.tx()
		tr.Tags = []string{"food", "weekly"}
		made := w.create(t, tr)

		made.Tags = []string{"restaurant"}
		if err := w.store.Update(made, ScopeOne); err != nil {
			t.Fatal(err)
		}
		got, err := w.store.Get(made.ID)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(got.Tags, ",") != "restaurant" {
			t.Fatalf("tags = %q; want only the new one", got.Tags)
		}
	})

	t.Run("it bumps updated_at", func(t *testing.T) {
		w := newWorld(t)
		made := w.create(t, w.tx())
		if _, err := w.conn.Exec(
			`UPDATE transactions SET updated_at = '2000-01-01 00:00:00' WHERE id = ?`, made.ID); err != nil {
			t.Fatal(err)
		}
		made.Title = "Groceries and fuel"
		if err := w.store.Update(made, ScopeOne); err != nil {
			t.Fatal(err)
		}
		got, err := w.store.Get(made.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.UpdatedAt == "2000-01-01 00:00:00" {
			t.Fatal("updated_at was left alone")
		}
	})

	t.Run("an unknown id is not found", func(t *testing.T) {
		w := newWorld(t)
		tr := w.tx()
		tr.ID = 999
		if err := w.store.Update(tr, ScopeOne); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update(999) = %v; want ErrNotFound", err)
		}
	})

	t.Run("a broken edit leaves both the row and the balance alone", func(t *testing.T) {
		w := newWorld(t)
		made := w.create(t, w.tx())
		made.Title = ""
		if err := w.store.Update(made, ScopeOne); err == nil {
			t.Fatal("Update with a blank title = nil; want the store to refuse it")
		}
		got, err := w.store.Get(made.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Title != "Groceries" {
			t.Fatalf("title = %q; want it unchanged", got.Title)
		}
		if b := w.accountBalance(t, w.inter.ID); b != 100000-12000 {
			t.Fatalf("balance = %d; want it unchanged at %d", b, 100000-12000)
		}
	})
}

func TestBalanceOnDelete(t *testing.T) {
	t.Run("deleting gives the account its money back", func(t *testing.T) {
		w := newWorld(t)
		made := w.create(t, w.tx())
		if err := w.store.Delete(made.ID, ScopeOne); err != nil {
			t.Fatal(err)
		}
		if got := w.accountBalance(t, w.inter.ID); got != 100000 {
			t.Fatalf("balance = %d; want it back at 100000", got)
		}
		if n := w.count(t); n != 0 {
			t.Fatalf("%d row(s) left; want none", n)
		}
	})

	t.Run("deleting takes the debt off the card", func(t *testing.T) {
		w := newWorld(t)
		tr := w.tx()
		tr.Account, tr.Card = Ref{}, Ref{ID: w.nucrd.ID}
		made := w.create(t, tr)
		if err := w.store.Delete(made.ID, ScopeOne); err != nil {
			t.Fatal(err)
		}
		if got := w.cardBalance(t, w.nucrd.ID); got != 0 {
			t.Fatalf("card balance = %d; want it back at 0", got)
		}
	})

	t.Run("an unknown id is not found", func(t *testing.T) {
		w := newWorld(t)
		if err := w.store.Delete(999, ScopeOne); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Delete(999) = %v; want ErrNotFound", err)
		}
	})
}

// listed is the ids a filter returns, in the order it returned them.
func listed(t *testing.T, s *Store, f Filter) []int64 {
	t.Helper()
	got, err := s.List(f)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, len(got))
	for i, tr := range got {
		ids[i] = tr.ID
	}
	return ids
}

func TestList(t *testing.T) {
	// build seeds five transactions spread over two months, two accounts, a
	// card, two categories and a handful of tags — enough for every filter to
	// have something to leave out.
	build := func(t *testing.T) (*world, map[string]int64) {
		t.Helper()
		w := newWorld(t)
		id := map[string]int64{}

		mk := func(name string, f func(*Transaction)) {
			tr := w.tx()
			tr.Title = name
			f(&tr)
			id[name] = w.create(t, tr).ID
		}
		mk("Groceries", func(tr *Transaction) {
			tr.Date, tr.Tags = "2026-08-08", []string{"food", "weekly"}
		})
		mk("Coffee", func(tr *Transaction) {
			tr.Date, tr.Value, tr.Tags = "2026-08-20", 800, []string{"food"}
		})
		mk("Salary", func(tr *Transaction) {
			tr.Date, tr.Kind, tr.Value = "2026-08-01", KindIncome, 500000
			tr.Category = Ref{ID: w.work.ID}
		})
		mk("Old coffee", func(tr *Transaction) {
			tr.Date, tr.Account = "2026-07-15", Ref{ID: w.cash.ID}
			tr.Description = "the good place"
		})
		mk("Card lunch", func(tr *Transaction) {
			tr.Date, tr.Account, tr.Card = "2026-08-12", Ref{}, Ref{ID: w.nucrd.ID}
			tr.Category = Ref{}
		})
		return w, id
	}

	t.Run("an empty filter returns everything, newest first", func(t *testing.T) {
		w, id := build(t)
		got := listed(t, w.store, Filter{})
		want := []int64{id["Coffee"], id["Card lunch"], id["Groceries"], id["Salary"], id["Old coffee"]}
		if len(got) != len(want) {
			t.Fatalf("got %v; want %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("got %v; want %v", got, want)
			}
		}
	})

	cases := []struct {
		name   string
		filter func(*world, map[string]int64) Filter
		want   []string
	}{
		{"one day", func(w *world, id map[string]int64) Filter {
			return Filter{From: "2026-08-08", To: "2026-08-08"}
		}, []string{"Groceries"}},
		{"a range", func(w *world, id map[string]int64) Filter {
			return Filter{From: "2026-08-01", To: "2026-08-12"}
		}, []string{"Card lunch", "Groceries", "Salary"}},
		{"from only", func(w *world, id map[string]int64) Filter {
			return Filter{From: "2026-08-01"}
		}, []string{"Coffee", "Card lunch", "Groceries", "Salary"}},
		{"to only", func(w *world, id map[string]int64) Filter {
			return Filter{To: "2026-07-31"}
		}, []string{"Old coffee"}},
		{"a tag", func(w *world, id map[string]int64) Filter {
			return Filter{Tag: "food"}
		}, []string{"Coffee", "Groceries"}},
		{"a tag matches whole, not by prefix", func(w *world, id map[string]int64) Filter {
			return Filter{Tag: "foo"}
		}, nil},
		{"a title search", func(w *world, id map[string]int64) Filter {
			return Filter{Search: "coffee"}
		}, []string{"Coffee", "Old coffee"}},
		{"a search reaches the description too", func(w *world, id map[string]int64) Filter {
			return Filter{Search: "good place"}
		}, []string{"Old coffee"}},
		{"a category", func(w *world, id map[string]int64) Filter {
			return Filter{CategoryID: w.work.ID}
		}, []string{"Salary"}},
		{"an account", func(w *world, id map[string]int64) Filter {
			return Filter{AccountID: w.cash.ID}
		}, []string{"Old coffee"}},
		{"a card", func(w *world, id map[string]int64) Filter {
			return Filter{CardID: w.nucrd.ID}
		}, []string{"Card lunch"}},
		{"two filters narrow together", func(w *world, id map[string]int64) Filter {
			return Filter{Tag: "food", From: "2026-08-10"}
		}, []string{"Coffee"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, id := build(t)
			got := listed(t, w.store, tc.filter(w, id))
			if len(got) != len(tc.want) {
				t.Fatalf("%d row(s); want %d (%v)", len(got), len(tc.want), tc.want)
			}
			for i, title := range tc.want {
				if got[i] != id[title] {
					t.Fatalf("row %d is not %q; got %v, want %v", i, title, got, id)
				}
			}
		})
	}
}

func TestAllTags(t *testing.T) {
	t.Run("each tag once, sorted", func(t *testing.T) {
		w := newWorld(t)
		for _, tags := range [][]string{{"food", "weekly"}, {"food"}, {"work"}} {
			tr := w.tx()
			tr.Tags = tags
			w.create(t, tr)
		}
		got, err := w.store.AllTags()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(got, ",") != "food,weekly,work" {
			t.Fatalf("AllTags = %q; want food,weekly,work", got)
		}
	})

	t.Run("an empty database has none", func(t *testing.T) {
		w := newWorld(t)
		got, err := w.store.AllTags()
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("AllTags = %q; want nothing", got)
		}
	})
}

// bill writes a card_bills row directly. The bills store generates them from the
// clock, and every case here is about a fixed date.
func (w *world) bill(t *testing.T, card cards.Card, closesOn, dueOn string, total int64) int64 {
	t.Helper()
	res, err := w.conn.Exec(
		`INSERT INTO card_bills (card_id, closes_on, due_on, total, status)
		 VALUES (?, ?, ?, ?, 'closed')`, card.ID, closesOn, dueOn, total)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (w *world) billStatus(t *testing.T, id int64) string {
	t.Helper()
	var status string
	if err := w.conn.QueryRow(`SELECT status FROM card_bills WHERE id = ?`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

// series is the rows of an installment purchase, in order.
func (w *world) series(t *testing.T, groupID int64) []Transaction {
	t.Helper()
	got, err := w.store.Series(groupID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// phone is the task's own example: a 1000.00 purchase on NUCRD split five ways.
func (w *world) phone() Transaction {
	return Transaction{
		Title: "Phone", Value: 100000, Kind: KindOutcome, Date: "2026-08-14",
		Card: Ref{ID: w.nucrd.ID}, Installment: Installment{Count: 5},
	}
}

func TestCreateInstallments(t *testing.T) {
	t.Run("writes one row per bill, dated a month apart", func(t *testing.T) {
		w := newWorld(t)
		made := w.create(t, w.phone())

		rows := w.series(t, made.ID)
		if len(rows) != 5 {
			t.Fatalf("%d rows written; want 5", len(rows))
		}
		wantDates := []string{"2026-08-14", "2026-09-14", "2026-10-14", "2026-11-14", "2026-12-14"}
		for i, r := range rows {
			if r.Date != wantDates[i] {
				t.Errorf("row %d dated %s; want %s", i+1, r.Date, wantDates[i])
			}
			if r.Value != 20000 {
				t.Errorf("row %d is worth %d; want 20000", i+1, r.Value)
			}
			if r.Installment.Seq != int64(i+1) || r.Installment.Count != 5 {
				t.Errorf("row %d is %d/%d; want %d/5", i+1, r.Installment.Seq, r.Installment.Count, i+1)
			}
			if r.Installment.Group != made.ID {
				t.Errorf("row %d groups under %d; want %d", i+1, r.Installment.Group, made.ID)
			}
			if r.Title != "Phone" {
				t.Errorf("row %d is titled %q; the position belongs in its own column", i+1, r.Title)
			}
		}
	})

	t.Run("the whole purchase hits the limit at once", func(t *testing.T) {
		// What a real issuer does: the limit is committed at the till, not a
		// fifth at a time.
		w := newWorld(t)
		w.create(t, w.phone())
		if got := w.cardBalance(t, w.nucrd.ID); got != 100000 {
			t.Fatalf("card owes %d after a 5x purchase of 100000; want the lot", got)
		}
	})

	t.Run("the odd cents ride on the first", func(t *testing.T) {
		w := newWorld(t)
		tr := w.phone()
		tr.Value, tr.Installment.Count = 100000, 3
		made := w.create(t, tr)

		rows := w.series(t, made.ID)
		if rows[0].Value != 33334 || rows[1].Value != 33333 || rows[2].Value != 33333 {
			t.Fatalf("split = %d/%d/%d; want 33334/33333/33333",
				rows[0].Value, rows[1].Value, rows[2].Value)
		}
		if got := w.cardBalance(t, w.nucrd.ID); got != 100000 {
			t.Fatalf("card owes %d; the split lost or invented %d", got, got-100000)
		}
	})

	t.Run("a series from the 31st clamps into the short months", func(t *testing.T) {
		w := newWorld(t)
		tr := w.phone()
		tr.Date, tr.Installment.Count = "2026-01-31", 3
		made := w.create(t, tr)

		rows := w.series(t, made.ID)
		want := []string{"2026-01-31", "2026-02-28", "2026-03-31"}
		for i, r := range rows {
			if r.Date != want[i] {
				t.Fatalf("dates = %s/%s/%s; want %v", rows[0].Date, rows[1].Date, rows[2].Date, want)
			}
		}
	})

	t.Run("a limit that refuses the series leaves nothing behind", func(t *testing.T) {
		// The rollback is what makes the refusal clean: no rows, no moved balance,
		// not even the installments that would have fitted on their own.
		w := newWorld(t)
		tr := w.phone()
		tr.Value = 600000 // NUCRD's limit is 500000 and it declines at it
		if err := w.store.Create(&tr); err == nil {
			t.Fatal("a series over the card's limit was accepted")
		}
		if n := w.count(t); n != 0 {
			t.Fatalf("%d row(s) survived the refusal", n)
		}
		if got := w.cardBalance(t, w.nucrd.ID); got != 0 {
			t.Fatalf("card owes %d after a refused series", got)
		}
	})

	t.Run("one installment is an ordinary transaction", func(t *testing.T) {
		w := newWorld(t)
		tr := w.phone()
		tr.Installment.Count = 1
		made := w.create(t, tr)

		got, err := w.store.Get(made.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.IsInstallment() || got.Installment.Group != 0 || got.Installment.Count > 1 {
			t.Fatalf("a single charge was recorded as a series: %+v", got.Installment)
		}
		if n := w.count(t); n != 1 {
			t.Fatalf("%d rows written for one installment", n)
		}
	})
}

func TestDeleteScope(t *testing.T) {
	// The middle of the series, so "this one" and "this and the rest" differ.
	third := func(t *testing.T, w *world) (Transaction, Transaction) {
		t.Helper()
		made := w.create(t, w.phone())
		return made, w.series(t, made.ID)[2]
	}

	t.Run("one installment leaves the rest alone", func(t *testing.T) {
		w := newWorld(t)
		made, row := third(t, w)

		if err := w.store.Delete(row.ID, ScopeOne); err != nil {
			t.Fatal(err)
		}
		if n := len(w.series(t, made.ID)); n != 4 {
			t.Fatalf("%d rows left; want 4", n)
		}
		if got := w.cardBalance(t, w.nucrd.ID); got != 80000 {
			t.Fatalf("card owes %d; want 80000 back from 100000", got)
		}
	})

	t.Run("this one and the ones after it", func(t *testing.T) {
		w := newWorld(t)
		made, row := third(t, w)

		if err := w.store.Delete(row.ID, ScopeForward); err != nil {
			t.Fatal(err)
		}
		rows := w.series(t, made.ID)
		if len(rows) != 2 || rows[0].Installment.Seq != 1 || rows[1].Installment.Seq != 2 {
			t.Fatalf("rows left = %+v; want installments 1 and 2", rows)
		}
		if got := w.cardBalance(t, w.nucrd.ID); got != 40000 {
			t.Fatalf("card owes %d; want 40000", got)
		}
	})

	t.Run("the whole series", func(t *testing.T) {
		w := newWorld(t)
		_, row := third(t, w)

		if err := w.store.Delete(row.ID, ScopeAll); err != nil {
			t.Fatal(err)
		}
		if n := w.count(t); n != 0 {
			t.Fatalf("%d row(s) left after deleting the series", n)
		}
		if got := w.cardBalance(t, w.nucrd.ID); got != 0 {
			t.Fatalf("card owes %d after the series went away", got)
		}
	})

	t.Run("a scope wider than one row is harmless on a plain transaction", func(t *testing.T) {
		w := newWorld(t)
		made := w.create(t, w.tx())
		if err := w.store.Delete(made.ID, ScopeAll); err != nil {
			t.Fatal(err)
		}
		if n := w.count(t); n != 0 {
			t.Fatalf("%d row(s) left", n)
		}
	})
}

func TestUpdateScope(t *testing.T) {
	t.Run("the series takes the new title and category", func(t *testing.T) {
		w := newWorld(t)
		made := w.create(t, w.phone())
		row := w.series(t, made.ID)[2]

		row.Title = "Phone, refurbished"
		row.Category = Ref{ID: w.work.ID}
		row.Tags = []string{"gadget"}
		if err := w.store.Update(row, ScopeAll); err != nil {
			t.Fatal(err)
		}

		for i, r := range w.series(t, made.ID) {
			if r.Title != "Phone, refurbished" {
				t.Errorf("row %d is titled %q", i+1, r.Title)
			}
			if r.Category.Code != "WORK1" {
				t.Errorf("row %d is filed under %q", i+1, r.Category.Code)
			}
			if len(r.Tags) != 1 || r.Tags[0] != "gadget" {
				t.Errorf("row %d carries %v", i+1, r.Tags)
			}
		}
	})

	t.Run("each installment keeps its own date and amount", func(t *testing.T) {
		// Re-splitting a live series is a different operation; an edit that
		// stamped one date and one amount over five rows would be a data loss.
		w := newWorld(t)
		made := w.create(t, w.phone())
		row := w.series(t, made.ID)[0]

		row.Title = "Renamed"
		row.Date = "2026-08-01"
		row.Value = 50000
		if err := w.store.Update(row, ScopeAll); err != nil {
			t.Fatal(err)
		}

		rows := w.series(t, made.ID)
		if rows[0].Date != "2026-08-01" || rows[0].Value != 50000 {
			t.Fatalf("the edited row did not take its own change: %+v", rows[0])
		}
		if rows[1].Date != "2026-09-14" || rows[1].Value != 20000 {
			t.Fatalf("a sibling took the edited row's date or amount: %+v", rows[1])
		}
		// 100000 - 20000 + 50000
		if got := w.cardBalance(t, w.nucrd.ID); got != 130000 {
			t.Fatalf("card owes %d; want 130000", got)
		}
	})

	t.Run("one row only", func(t *testing.T) {
		w := newWorld(t)
		made := w.create(t, w.phone())
		row := w.series(t, made.ID)[2]

		row.Title = "Only this one"
		if err := w.store.Update(row, ScopeOne); err != nil {
			t.Fatal(err)
		}
		rows := w.series(t, made.ID)
		if rows[2].Title != "Only this one" || rows[0].Title != "Phone" {
			t.Fatalf("ScopeOne reached further than one row: %q / %q", rows[0].Title, rows[2].Title)
		}
	})
}

func TestPayBill(t *testing.T) {
	setup := func(t *testing.T) (*world, int64) {
		t.Helper()
		w := newWorld(t)
		// A cycle with 890.50 charged on it, already closed.
		tr := w.tx()
		tr.Account, tr.Card = Ref{}, Ref{ID: w.nucrd.ID}
		tr.Value, tr.Date = 89050, "2026-07-05"
		w.create(t, tr)
		return w, w.bill(t, w.nucrd, "2026-07-15", "2026-07-22", 89050)
	}

	t.Run("moves the account and the card, and marks the bill", func(t *testing.T) {
		w, billID := setup(t)
		if err := w.store.PayBill(billID, w.inter.ID, 89050, "2026-07-20"); err != nil {
			t.Fatal(err)
		}

		if got := w.accountBalance(t, w.inter.ID); got != 100000-89050 {
			t.Errorf("INTER = %d; want %d", got, 100000-89050)
		}
		if got := w.cardBalance(t, w.nucrd.ID); got != 0 {
			t.Errorf("NUCRD owes %d; want nothing", got)
		}
		if got := w.billStatus(t, billID); got != "paid" {
			t.Errorf("bill is %q; want paid", got)
		}
	})

	t.Run("part of it is partial", func(t *testing.T) {
		w, billID := setup(t)
		if err := w.store.PayBill(billID, w.inter.ID, 40000, "2026-07-20"); err != nil {
			t.Fatal(err)
		}
		if got := w.cardBalance(t, w.nucrd.ID); got != 49050 {
			t.Errorf("NUCRD owes %d; want 49050", got)
		}
		if got := w.billStatus(t, billID); got != "partial" {
			t.Errorf("bill is %q; want partial", got)
		}
	})

	t.Run("the payment is not a charge on the card", func(t *testing.T) {
		w, billID := setup(t)
		if err := w.store.PayBill(billID, w.inter.ID, 89050, "2026-07-20"); err != nil {
			t.Fatal(err)
		}
		found, err := w.store.List(Filter{CardID: w.nucrd.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 1 || found[0].Title != "Groceries" {
			t.Fatalf("the card lists %+v; the payment should be on the account", found)
		}
	})

	t.Run("deleting it puts both balances and the status back", func(t *testing.T) {
		w, billID := setup(t)
		if err := w.store.PayBill(billID, w.inter.ID, 89050, "2026-07-20"); err != nil {
			t.Fatal(err)
		}
		found, err := w.store.List(Filter{AccountID: w.inter.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 1 {
			t.Fatalf("expected one payment on INTER, got %+v", found)
		}
		if found[0].PaysBillID != billID {
			t.Fatalf("the payment names bill %d; want %d", found[0].PaysBillID, billID)
		}

		if err := w.store.Delete(found[0].ID, ScopeOne); err != nil {
			t.Fatal(err)
		}
		if got := w.accountBalance(t, w.inter.ID); got != 100000 {
			t.Errorf("INTER = %d; want its money back", got)
		}
		if got := w.cardBalance(t, w.nucrd.ID); got != 89050 {
			t.Errorf("NUCRD owes %d; want the debt back", got)
		}
		if got := w.billStatus(t, billID); got != "closed" {
			t.Errorf("bill is %q; want closed again", got)
		}
	})

	t.Run("editing the amount moves both sides", func(t *testing.T) {
		w, billID := setup(t)
		if err := w.store.PayBill(billID, w.inter.ID, 89050, "2026-07-20"); err != nil {
			t.Fatal(err)
		}
		found, _ := w.store.List(Filter{AccountID: w.inter.ID})
		tr := found[0]
		tr.Value = 40000
		if err := w.store.Update(tr, ScopeOne); err != nil {
			t.Fatal(err)
		}

		if got := w.accountBalance(t, w.inter.ID); got != 100000-40000 {
			t.Errorf("INTER = %d; want %d", got, 100000-40000)
		}
		if got := w.cardBalance(t, w.nucrd.ID); got != 49050 {
			t.Errorf("NUCRD owes %d; want 49050", got)
		}
		if got := w.billStatus(t, billID); got != "partial" {
			t.Errorf("bill is %q; want partial", got)
		}
	})

	t.Run("a bill that does not exist is refused", func(t *testing.T) {
		w, _ := setup(t)
		if err := w.store.PayBill(404, w.inter.ID, 1000, "2026-07-20"); err == nil {
			t.Fatal("a payment against a missing bill was accepted")
		}
		if n := w.count(t); n != 1 {
			t.Fatalf("%d rows; the refused payment left one behind", n)
		}
	})
}

// goal puts one goal in the world's database and hands back its id. Raw SQL
// rather than goals.NewStore, so this package's tests stay as free of the other
// module as the package itself is.
func (w *world) goal(t *testing.T, name, currency string) int64 {
	t.Helper()
	res, err := w.conn.Exec(
		`INSERT INTO goals (name, target, currency, kind) VALUES (?, 500000, ?, 'saving')`,
		name, currency)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestGoalLink(t *testing.T) {
	t.Run("a linked transaction comes back carrying its goal", func(t *testing.T) {
		w := newWorld(t)
		id := w.goal(t, "New laptop", "BRL")
		tr := w.tx()
		tr.Goal = Ref{ID: id}
		tr = w.create(t, tr)

		got, err := w.store.Get(tr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Goal.ID != id {
			t.Fatalf("Goal.ID = %d; want %d", got.Goal.ID, id)
		}
		if got.Goal.Name != "New laptop" {
			t.Errorf("Goal.Name = %q; want the join to fill it in", got.Goal.Name)
		}
		if got.GoalCurrency != "BRL" {
			t.Errorf("GoalCurrency = %q; want BRL", got.GoalCurrency)
		}
	})

	t.Run("a transaction naming no goal comes back with none", func(t *testing.T) {
		w := newWorld(t)
		tr := w.create(t, w.tx())
		got, err := w.store.Get(tr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Goal.ID != 0 {
			t.Fatalf("Goal.ID = %d; want 0", got.Goal.ID)
		}
	})

	t.Run("an edit can attach, move and drop the goal", func(t *testing.T) {
		w := newWorld(t)
		one := w.goal(t, "New laptop", "BRL")
		two := w.goal(t, "Holiday", "BRL")
		tr := w.create(t, w.tx())

		tr.Goal = Ref{ID: one}
		if err := w.store.Update(tr, ScopeOne); err != nil {
			t.Fatal(err)
		}
		got, _ := w.store.Get(tr.ID)
		if got.Goal.ID != one {
			t.Fatalf("after attaching, Goal.ID = %d; want %d", got.Goal.ID, one)
		}

		got.Goal = Ref{ID: two}
		if err := w.store.Update(got, ScopeOne); err != nil {
			t.Fatal(err)
		}
		got, _ = w.store.Get(tr.ID)
		if got.Goal.ID != two {
			t.Fatalf("after moving, Goal.ID = %d; want %d", got.Goal.ID, two)
		}

		got.Goal = Ref{}
		if err := w.store.Update(got, ScopeOne); err != nil {
			t.Fatal(err)
		}
		got, _ = w.store.Get(tr.ID)
		if got.Goal.ID != 0 {
			t.Fatalf("after dropping, Goal.ID = %d; want 0", got.Goal.ID)
		}
	})

	t.Run("a goal in another currency is refused even when the caller only knew the id", func(t *testing.T) {
		w := newWorld(t)
		id := w.goal(t, "Satoshis", "BTC")
		tr := w.tx() // filed against INTER, which is BRL
		tr.Goal = Ref{ID: id}

		err := w.store.Create(&tr)
		if err == nil {
			t.Fatal("a BRL transaction was linked to a BTC goal")
		}
		if !strings.Contains(err.Error(), "currency") {
			t.Fatalf("Create = %v; want it to say why", err)
		}
		if n := w.count(t); n != 0 {
			t.Fatalf("%d transactions written; want the refusal to leave none", n)
		}
	})

	t.Run("an edit onto a goal in another currency is refused too", func(t *testing.T) {
		w := newWorld(t)
		id := w.goal(t, "Satoshis", "BTC")
		tr := w.create(t, w.tx())

		tr.Goal = Ref{ID: id}
		if err := w.store.Update(tr, ScopeOne); err == nil {
			t.Fatal("an edit linked a BRL transaction to a BTC goal")
		}
		got, _ := w.store.Get(tr.ID)
		if got.Goal.ID != 0 {
			t.Errorf("Goal.ID = %d; want the refused edit to leave none", got.Goal.ID)
		}
	})

	t.Run("a goal that does not exist is refused", func(t *testing.T) {
		w := newWorld(t)
		tr := w.tx()
		tr.Goal = Ref{ID: 404}
		if err := w.store.Create(&tr); err == nil {
			t.Fatal("a link to a goal that is not there was accepted")
		}
	})

	t.Run("every row of an installment series carries the goal", func(t *testing.T) {
		w := newWorld(t)
		id := w.goal(t, "New phone", "BRL")
		tr := w.tx()
		tr.Account, tr.Card = Ref{}, Ref{ID: w.nucrd.ID}
		tr.Title, tr.Value = "Phone", 100000
		tr.Installment = Installment{Count: 5}
		tr.Goal = Ref{ID: id}
		tr = w.create(t, tr)

		series, err := w.store.Series(tr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(series) != 5 {
			t.Fatalf("series has %d rows; want 5", len(series))
		}
		for _, row := range series {
			if row.Goal.ID != id {
				t.Errorf("installment %d has Goal.ID %d; want %d", row.Installment.Seq, row.Goal.ID, id)
			}
		}
	})
}

func TestListByGoal(t *testing.T) {
	t.Run("narrows to the linked transactions", func(t *testing.T) {
		w := newWorld(t)
		mine := w.goal(t, "New laptop", "BRL")
		other := w.goal(t, "Holiday", "BRL")

		linked := w.tx()
		linked.Title, linked.Goal = "Toward the laptop", Ref{ID: mine}
		w.create(t, linked)

		elsewhere := w.tx()
		elsewhere.Title, elsewhere.Goal = "Toward the holiday", Ref{ID: other}
		w.create(t, elsewhere)

		w.create(t, w.tx()) // no goal at all

		got, err := w.store.List(Filter{GoalID: mine})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Title != "Toward the laptop" {
			t.Fatalf("List by goal = %d rows (%+v); want only the linked one", len(got), got)
		}
	})

	t.Run("a goal with nothing linked lists nothing", func(t *testing.T) {
		w := newWorld(t)
		id := w.goal(t, "New laptop", "BRL")
		w.create(t, w.tx())

		got, err := w.store.List(Filter{GoalID: id})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("List by an empty goal = %d rows; want none", len(got))
		}
	})
}

func TestGoalAcrossASeries(t *testing.T) {
	// A goal is a label, so it carries across a series the way the title and the
	// category do — the scope is what says how far.
	w := newWorld(t)
	id := w.goal(t, "New phone", "BRL")
	tr := w.tx()
	tr.Account, tr.Card = Ref{}, Ref{ID: w.nucrd.ID}
	tr.Title, tr.Value = "Phone", 100000
	tr.Installment = Installment{Count: 5}
	tr = w.create(t, tr)

	first, err := w.store.Get(tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	first.Goal = Ref{ID: id}
	if err := w.store.Update(first, ScopeAll); err != nil {
		t.Fatal(err)
	}

	series, err := w.store.Series(tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range series {
		if row.Goal.ID != id {
			t.Errorf("installment %d has Goal.ID %d; want the whole series linked", row.Installment.Seq, row.Goal.ID)
		}
	}

	// And with ScopeOne it reaches only the row it was asked about.
	other := w.goal(t, "Holiday", "BRL")
	series[0].Goal = Ref{ID: other}
	if err := w.store.Update(series[0], ScopeOne); err != nil {
		t.Fatal(err)
	}
	after, err := w.store.Series(tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after[0].Goal.ID != other {
		t.Errorf("the edited row has Goal.ID %d; want %d", after[0].Goal.ID, other)
	}
	if after[1].Goal.ID != id {
		t.Errorf("a sibling moved to Goal.ID %d; want it left at %d", after[1].Goal.ID, id)
	}
}

// recurringBill writes a recurring bill straight through SQL. This package
// cannot import kakei/internal/recurring — that package imports this one,
// because a payment names the bill it settles.
func (w *world) recurringBill(t *testing.T, code string, account int64) int64 {
	t.Helper()
	var id int64
	if err := w.conn.QueryRow(
		`INSERT INTO recurring_bills (code, name, color, expected, account_id, open_day, due_day)
		 VALUES (?, 'Energy', 'amber', 21490, ?, 5, 15) RETURNING id`, code, account).
		Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRecurringPayments(t *testing.T) {
	t.Run("a payment carries its bill and its cycle back out", func(t *testing.T) {
		w := newWorld(t)
		bill := w.recurringBill(t, "ENERG", w.inter.ID)

		tr := w.tx()
		tr.Title, tr.Value = "Energy", 21490
		tr.Recurring, tr.Cycle = Ref{ID: bill}, "2026-08"
		got, err := w.store.Get(w.create(t, tr).ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Recurring.ID != bill {
			t.Errorf("bill = %d, want %d", got.Recurring.ID, bill)
		}
		if got.Recurring.Code != "ENERG" {
			t.Errorf("bill code = %q, want ENERG — the join has to carry it", got.Recurring.Code)
		}
		if got.Cycle != "2026-08" {
			t.Errorf("cycle = %q, want 2026-08", got.Cycle)
		}
	})

	t.Run("a payment still moves the balance it came out of", func(t *testing.T) {
		w := newWorld(t)
		bill := w.recurringBill(t, "ENERG", w.inter.ID)
		before := w.accountBalance(t, w.inter.ID)

		tr := w.tx()
		tr.Value = 21490
		tr.Recurring, tr.Cycle = Ref{ID: bill}, "2026-08"
		w.create(t, tr)

		if got := w.accountBalance(t, w.inter.ID); got != before-21490 {
			t.Errorf("balance = %d, want %d — a bill payment is an ordinary outcome", got, before-21490)
		}
	})

	t.Run("the filter narrows to one bill", func(t *testing.T) {
		w := newWorld(t)
		energy := w.recurringBill(t, "ENERG", w.inter.ID)
		water := w.recurringBill(t, "WATER", w.inter.ID)

		tr := w.tx()
		tr.Recurring, tr.Cycle = Ref{ID: energy}, "2026-07"
		july := w.create(t, tr)
		tr.Cycle, tr.Date = "2026-08", "2026-08-08"
		august := w.create(t, tr)
		tr.Recurring, tr.Cycle = Ref{ID: water}, "2026-08"
		w.create(t, tr)
		w.create(t, w.tx()) // nothing to do with a bill at all

		// Newest first, like every other list.
		if got, want := listed(t, w.store, Filter{RecurringID: energy}), []int64{august.ID, july.ID}; !equal(got, want) {
			t.Errorf("listed %v, want %v", got, want)
		}
	})

	t.Run("an edit can take the cycle off with the bill", func(t *testing.T) {
		w := newWorld(t)
		bill := w.recurringBill(t, "ENERG", w.inter.ID)
		tr := w.tx()
		tr.Recurring, tr.Cycle = Ref{ID: bill}, "2026-08"
		made := w.create(t, tr)

		made.Recurring, made.Cycle = Ref{}, ""
		if err := w.store.Update(made, ScopeOne); err != nil {
			t.Fatal(err)
		}
		got, err := w.store.Get(made.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Recurring.ID != 0 || got.Cycle != "" {
			t.Errorf("still linked to bill %d cycle %q after being unlinked", got.Recurring.ID, got.Cycle)
		}
	})
}

func equal(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A frozen account is out of play. The form already leaves it off the list, but
// the form is not the only way in: a bill payment, a recurring payment, an
// import or a bot all reach the store directly.
func TestFrozenAccountsRefuseNewMoney(t *testing.T) {
	// freeze puts CASH1 out of play and hands its id back.
	freeze := func(t *testing.T, w *world) int64 {
		t.Helper()
		if _, err := w.accounts.ToggleFreeze(w.cash.ID); err != nil {
			t.Fatal(err)
		}
		return w.cash.ID
	}

	t.Run("a transaction against a frozen account is refused", func(t *testing.T) {
		w := newWorld(t)
		frozen := freeze(t, w)

		tr := w.tx()
		tr.Account = Ref{ID: frozen}
		err := w.store.Create(&tr)
		if err == nil {
			t.Fatal("money was filed against a frozen account; want it refused")
		}
		if !strings.Contains(err.Error(), "frozen") {
			t.Fatalf("err = %v; want it to say the account is frozen", err)
		}
	})

	t.Run("the refused write leaves no row and no moved balance", func(t *testing.T) {
		w := newWorld(t)
		frozen := freeze(t, w)
		before := w.accountBalance(t, frozen)

		tr := w.tx()
		tr.Account = Ref{ID: frozen}
		if err := w.store.Create(&tr); err == nil {
			t.Fatal("want the create refused")
		}
		if n := w.count(t); n != 0 {
			t.Fatalf("%d transactions written; want none", n)
		}
		if got := w.accountBalance(t, frozen); got != before {
			t.Fatalf("balance moved to %d; want it left at %d", got, before)
		}
	})

	t.Run("a bill payment cannot be filed from a frozen account either", func(t *testing.T) {
		// PayBill goes through Create, which is the point of putting the guard
		// there rather than in the command.
		w := newWorld(t)
		frozen := freeze(t, w)

		// A real bill, so it is the freeze that refuses this and not a missing
		// row — the error has to name the account, not the bill.
		res, err := w.conn.Exec(
			`INSERT INTO card_bills (card_id, closes_on, due_on, total, status)
			 VALUES (?, '2026-07-15', '2026-07-22', 50000, 'closed')`, w.nucrd.ID)
		if err != nil {
			t.Fatal(err)
		}
		bill, err := res.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}

		err = w.store.PayBill(bill, frozen, 5000, "2026-08-08")
		if err == nil {
			t.Fatal("a bill was paid from a frozen account; want it refused")
		}
		if !strings.Contains(err.Error(), "frozen") {
			t.Fatalf("err = %v; want the freeze to be what refused it", err)
		}
	})

	t.Run("a credit card is unaffected — only accounts freeze", func(t *testing.T) {
		w := newWorld(t)
		freeze(t, w)

		tr := w.tx()
		tr.Account, tr.Card = Ref{}, Ref{ID: w.nucrd.ID}
		if err := w.store.Create(&tr); err != nil {
			t.Fatalf("a card charge was refused: %v", err)
		}
	})

	t.Run("an unfrozen account still takes money", func(t *testing.T) {
		w := newWorld(t)
		freeze(t, w)
		if err := w.store.Create(&[]Transaction{w.tx()}[0]); err != nil {
			t.Fatalf("INTER is not frozen and was refused: %v", err)
		}
	})

	t.Run("moving a transaction onto a frozen account is refused", func(t *testing.T) {
		w := newWorld(t)
		tr := w.create(t, w.tx())
		frozen := freeze(t, w)

		tr.Account = Ref{ID: frozen}
		if err := w.store.Update(tr, ScopeOne); err == nil {
			t.Fatal("a transaction was moved onto a frozen account; want it refused")
		}
	})

	// Freezing is not a trap. Money already on a frozen account has to be able
	// to leave it, or the freeze is a way to lose track of it.
	t.Run("moving a transaction off a frozen account is allowed", func(t *testing.T) {
		w := newWorld(t)
		tr := w.create(t, w.tx())
		if _, err := w.accounts.ToggleFreeze(w.inter.ID); err != nil {
			t.Fatal(err)
		}

		tr.Account = Ref{ID: w.cash.ID}
		if err := w.store.Update(tr, ScopeOne); err != nil {
			t.Fatalf("money could not leave a frozen account: %v", err)
		}
	})

	t.Run("deleting a transaction on a frozen account is allowed", func(t *testing.T) {
		w := newWorld(t)
		tr := w.create(t, w.tx())
		if _, err := w.accounts.ToggleFreeze(w.inter.ID); err != nil {
			t.Fatal(err)
		}
		if err := w.store.Delete(tr.ID, ScopeOne); err != nil {
			t.Fatalf("a transaction on a frozen account could not be cleaned up: %v", err)
		}
	})
}

// move is the transfer most cases start from: R$500.00 out of INTER and into
// CASH1, both in reais.
func (w *world) move() Transfer {
	return Transfer{
		Title: "Transferência", Date: "2026-08-14",
		From: Ref{ID: w.inter.ID}, To: Ref{ID: w.cash.ID},
		FromValue: 50000, ToValue: 50000,
	}
}

func TestTransferWritesBothLegs(t *testing.T) {
	t.Run("two rows, one group, and the group is the outgoing leg", func(t *testing.T) {
		w := newWorld(t)
		tr := w.move()
		if err := w.store.Transfer(&tr); err != nil {
			t.Fatal(err)
		}
		if n := w.count(t); n != 2 {
			t.Fatalf("%d rows written; want 2", n)
		}

		out, err := w.store.Get(tr.Group)
		if err != nil {
			t.Fatalf("the group is not the id of a row: %v", err)
		}
		if out.Kind != KindOutcome {
			t.Fatalf("the group names a %s row; want it to name the leg the money left", out.Kind)
		}
		if out.Account.ID != w.inter.ID {
			t.Fatalf("the group's row is on account %d; want the source %d", out.Account.ID, w.inter.ID)
		}
	})

	t.Run("each leg names the other end", func(t *testing.T) {
		w := newWorld(t)
		tr := w.move()
		if err := w.store.Transfer(&tr); err != nil {
			t.Fatal(err)
		}

		all, err := w.store.List(Filter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 2 {
			t.Fatalf("List = %d rows; want both legs", len(all))
		}
		for _, leg := range all {
			if !leg.IsTransfer() {
				t.Fatalf("leg #%d does not read as a transfer", leg.ID)
			}
			if leg.Counterpart.Ref.ID == 0 {
				t.Fatalf("leg #%d names no counterpart", leg.ID)
			}
			if leg.Counterpart.Ref.ID == leg.Account.ID {
				t.Fatalf("leg #%d names itself as its counterpart", leg.ID)
			}
			if leg.Counterpart.Value != 50000 {
				t.Errorf("leg #%d counterpart value = %d; want 50000", leg.ID, leg.Counterpart.Value)
			}
		}
	})

	t.Run("the balances move both ways and net to nothing", func(t *testing.T) {
		w := newWorld(t)
		fromBefore := w.accountBalance(t, w.inter.ID)
		toBefore := w.accountBalance(t, w.cash.ID)

		tr := w.move()
		if err := w.store.Transfer(&tr); err != nil {
			t.Fatal(err)
		}
		if got := w.accountBalance(t, w.inter.ID); got != fromBefore-50000 {
			t.Errorf("source = %d; want %d", got, fromBefore-50000)
		}
		if got := w.accountBalance(t, w.cash.ID); got != toBefore+50000 {
			t.Errorf("destination = %d; want %d", got, toBefore+50000)
		}
	})

	t.Run("a fee is the two legs differing", func(t *testing.T) {
		w := newWorld(t)
		tr := w.move()
		tr.ToValue = 49500 // a R$5.00 TED
		if err := w.store.Transfer(&tr); err != nil {
			t.Fatal(err)
		}
		if got := w.accountBalance(t, w.cash.ID); got != 15000+49500 {
			t.Fatalf("destination = %d; want only what arrived", got)
		}
	})

	t.Run("nothing is written when either leg is refused", func(t *testing.T) {
		w := newWorld(t)
		before := w.accountBalance(t, w.inter.ID)

		tr := w.move()
		tr.To = Ref{ID: 404} // no such account
		if err := w.store.Transfer(&tr); err == nil {
			t.Fatal("a transfer into a missing account succeeded; want it refused")
		}
		if n := w.count(t); n != 0 {
			t.Fatalf("%d rows left behind; want none", n)
		}
		if got := w.accountBalance(t, w.inter.ID); got != before {
			t.Fatalf("the source balance moved to %d; want %d", got, before)
		}
	})
}

func TestTransferIsRefused(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Transfer, *world)
		want string
	}{
		{"to and from the same account", func(tr *Transfer, w *world) {
			tr.To = Ref{ID: w.inter.ID}
		}, "same account"},
		{"with no source", func(tr *Transfer, _ *world) { tr.From = Ref{} }, "account"},
		{"with no destination", func(tr *Transfer, _ *world) { tr.To = Ref{} }, "account"},
		{"with nothing leaving", func(tr *Transfer, _ *world) { tr.FromValue = 0 }, "more than zero"},
		{"with nothing arriving", func(tr *Transfer, _ *world) { tr.ToValue = 0 }, "more than zero"},
		{"with no title", func(tr *Transfer, _ *world) { tr.Title = "  " }, "title"},
		{"on a date that is not one", func(tr *Transfer, _ *world) { tr.Date = "14/08/2026" }, "YYYY-MM-DD"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			tr := w.move()
			tc.edit(&tr, w)
			err := w.store.Transfer(&tr)
			if err == nil {
				t.Fatal("want it refused")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v; want an error mentioning %q", err, tc.want)
			}
			if n := w.count(t); n != 0 {
				t.Fatalf("%d rows written by a refused transfer", n)
			}
		})
	}

	t.Run("out of a frozen account", func(t *testing.T) {
		w := newWorld(t)
		if _, err := w.accounts.ToggleFreeze(w.inter.ID); err != nil {
			t.Fatal(err)
		}
		tr := w.move()
		if err := w.store.Transfer(&tr); err == nil {
			t.Fatal("money left a frozen account; want it refused")
		}
	})

	t.Run("into a frozen account", func(t *testing.T) {
		w := newWorld(t)
		if _, err := w.accounts.ToggleFreeze(w.cash.ID); err != nil {
			t.Fatal(err)
		}
		tr := w.move()
		if err := w.store.Transfer(&tr); err == nil {
			t.Fatal("money arrived in a frozen account; want it refused")
		}
	})
}

// A transfer between two currencies is two amounts and no rate: each side moves
// by its own, and nothing anywhere converts one into the other.
func TestCrossCurrencyTransfer(t *testing.T) {
	w := newWorld(t)
	usd := accounts.Account{Code: "PAYPL", Name: "PayPal", Color: "blue",
		Currency: "USD", Balance: 20000}
	if err := w.accounts.Create(&usd); err != nil {
		t.Fatal(err)
	}

	tr := w.move()
	tr.To = Ref{ID: usd.ID}
	tr.FromValue, tr.ToValue = 50000, 9200 // R$500.00 out, $92.00 in
	if err := w.store.Transfer(&tr); err != nil {
		t.Fatal(err)
	}
	if got := w.accountBalance(t, w.inter.ID); got != 100000-50000 {
		t.Errorf("source = %d; want it down by its own R$500.00", got)
	}
	if got := w.accountBalance(t, usd.ID); got != 20000+9200 {
		t.Errorf("destination = %d; want it up by its own $92.00", got)
	}
}

func TestTransferGoal(t *testing.T) {
	t.Run("the arriving leg may feed a goal", func(t *testing.T) {
		w := newWorld(t)
		var goalID int64
		res, err := w.conn.Exec(
			`INSERT INTO goals (name, target, currency, kind) VALUES ('Reserva', 500000, 'BRL', 'saving')`)
		if err != nil {
			t.Fatal(err)
		}
		if goalID, err = res.LastInsertId(); err != nil {
			t.Fatal(err)
		}

		tr := w.move()
		tr.Goal = Ref{ID: goalID}
		if err := w.store.Transfer(&tr); err != nil {
			t.Fatal(err)
		}

		var on string
		if err := w.conn.QueryRow(
			`SELECT kind FROM transactions WHERE goal_id = ?`, goalID).Scan(&on); err != nil {
			t.Fatal(err)
		}
		if on != KindIncome {
			t.Fatalf("the goal is on the %s leg; want the one the money arrives on", on)
		}
	})

	t.Run("a goal in another currency is refused", func(t *testing.T) {
		w := newWorld(t)
		res, err := w.conn.Exec(
			`INSERT INTO goals (name, target, currency, kind) VALUES ('Bitcoin', 100000000, 'BTC', 'saving')`)
		if err != nil {
			t.Fatal(err)
		}
		goalID, err := res.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}

		tr := w.move()
		tr.Goal = Ref{ID: goalID}
		if err := w.store.Transfer(&tr); err == nil {
			t.Fatal("satoshis were counted against a reais transfer; want it refused")
		}
		if n := w.count(t); n != 0 {
			t.Fatalf("%d rows written by a refused transfer", n)
		}
	})
}

func TestTransferEditAndDelete(t *testing.T) {
	t.Run("editing reaches both legs", func(t *testing.T) {
		w := newWorld(t)
		tr := w.move()
		if err := w.store.Transfer(&tr); err != nil {
			t.Fatal(err)
		}

		tr.Title, tr.Description = "Para a reserva", "sobra do mês"
		tr.FromValue, tr.ToValue = 60000, 60000
		if err := w.store.UpdateTransfer(tr); err != nil {
			t.Fatal(err)
		}

		all, err := w.store.List(Filter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, leg := range all {
			if leg.Title != "Para a reserva" || leg.Description != "sobra do mês" {
				t.Fatalf("leg #%d kept the old title: %+v", leg.ID, leg)
			}
			if leg.Value != 60000 {
				t.Fatalf("leg #%d is worth %d; want the edited 60000", leg.ID, leg.Value)
			}
		}
		// The balances have to follow the edit, both of them.
		if got := w.accountBalance(t, w.inter.ID); got != 100000-60000 {
			t.Errorf("source = %d; want it re-applied at the new amount", got)
		}
		if got := w.accountBalance(t, w.cash.ID); got != 15000+60000 {
			t.Errorf("destination = %d; want it re-applied at the new amount", got)
		}
	})

	t.Run("editing can move either end", func(t *testing.T) {
		w := newWorld(t)
		other := accounts.Account{Code: "NUBON", Name: "Nubank", Color: "violet",
			Currency: "BRL", Balance: 0}
		if err := w.accounts.Create(&other); err != nil {
			t.Fatal(err)
		}
		tr := w.move()
		if err := w.store.Transfer(&tr); err != nil {
			t.Fatal(err)
		}

		tr.To = Ref{ID: other.ID}
		if err := w.store.UpdateTransfer(tr); err != nil {
			t.Fatal(err)
		}
		if got := w.accountBalance(t, w.cash.ID); got != 15000 {
			t.Errorf("the old destination = %d; want the money given back", got)
		}
		if got := w.accountBalance(t, other.ID); got != 50000 {
			t.Errorf("the new destination = %d; want it to have arrived", got)
		}
	})

	t.Run("deleting either leg deletes both and reverses both balances", func(t *testing.T) {
		for _, which := range []string{"the outgoing leg", "the arriving leg"} {
			t.Run(which, func(t *testing.T) {
				w := newWorld(t)
				tr := w.move()
				if err := w.store.Transfer(&tr); err != nil {
					t.Fatal(err)
				}
				all, err := w.store.List(Filter{})
				if err != nil {
					t.Fatal(err)
				}
				target := all[0]
				for _, leg := range all {
					if (which == "the outgoing leg") == (leg.Kind == KindOutcome) {
						target = leg
					}
				}

				if err := w.store.Delete(target.ID, ScopeOne); err != nil {
					t.Fatal(err)
				}
				if n := w.count(t); n != 0 {
					t.Fatalf("%d rows left; want half a transfer to be impossible", n)
				}
				if got := w.accountBalance(t, w.inter.ID); got != 100000 {
					t.Errorf("source = %d; want it back where it started", got)
				}
				if got := w.accountBalance(t, w.cash.ID); got != 15000 {
					t.Errorf("destination = %d; want it back where it started", got)
				}
			})
		}
	})

	t.Run("a transfer comes back as one thing to edit", func(t *testing.T) {
		w := newWorld(t)
		tr := w.move()
		tr.ToValue = 49500
		if err := w.store.Transfer(&tr); err != nil {
			t.Fatal(err)
		}

		got, err := w.store.GetTransfer(tr.Group)
		if err != nil {
			t.Fatal(err)
		}
		if got.From.ID != w.inter.ID || got.To.ID != w.cash.ID {
			t.Fatalf("GetTransfer = %+v; want INTER → CASH1", got)
		}
		if got.FromValue != 50000 || got.ToValue != 49500 {
			t.Fatalf("amounts = %d/%d; want 50000/49500", got.FromValue, got.ToValue)
		}
		if got.Title != "Transferência" {
			t.Errorf("title = %q; want it kept", got.Title)
		}
	})
}

// An ordinary edit must not be able to turn one leg into something else and
// leave the other pointing at it.
func TestOrdinaryUpdateCannotBreakATransfer(t *testing.T) {
	w := newWorld(t)
	tr := w.move()
	if err := w.store.Transfer(&tr); err != nil {
		t.Fatal(err)
	}
	one, err := w.store.Get(tr.Group)
	if err != nil {
		t.Fatal(err)
	}

	one.Category = Ref{ID: w.food.ID}
	if err := w.store.Update(one, ScopeOne); err == nil {
		t.Fatal("a transfer leg took a category through the ordinary edit; want it refused")
	}
}

// audit is every trail row for one entity so far, oldest first.
func (w *world) audit(t *testing.T, entity string) []logs.Entry {
	t.Helper()
	es, err := logs.List(w.conn, logs.Filter{Entity: entity, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(es)-1; i < j; i, j = i+1, j-1 {
		es[i], es[j] = es[j], es[i]
	}
	return es
}

func TestAuditTrail(t *testing.T) {
	t.Run("a create leaves one row", func(t *testing.T) {
		w := newWorld(t)
		tr := w.create(t, w.tx())
		es := w.audit(t, "transaction")
		if len(es) != 1 || es[0].Action != "created" || es[0].EntityID != tr.ID {
			t.Fatalf("trail = %+v; want one created row for %d", es, tr.ID)
		}
	})

	t.Run("an installment series is one action", func(t *testing.T) {
		w := newWorld(t)
		w.create(t, w.phone())
		if es := w.audit(t, "transaction"); len(es) != 1 {
			t.Fatalf("trail has %d rows for a 6-installment purchase; want 1", len(es))
		}
	})

	t.Run("an edit across a series is one action, carrying only what moved", func(t *testing.T) {
		w := newWorld(t)
		created := w.create(t, w.phone())
		// The stored row, not the pre-split purchase — an edit starts from what
		// the form loads.
		tr, err := w.store.Get(created.ID)
		if err != nil {
			t.Fatal(err)
		}
		tr.Title = "iPhone"
		if err := w.store.Update(tr, ScopeAll); err != nil {
			t.Fatal(err)
		}
		es := w.audit(t, "transaction")
		if len(es) != 2 || es[1].Action != "edited" {
			t.Fatalf("trail = %+v; want a created then one edited row", es)
		}
		if !strings.Contains(es[1].Changes, `"title"`) || strings.Contains(es[1].Changes, `"value"`) {
			t.Errorf("changes = %s; want the title move and nothing else", es[1].Changes)
		}
	})

	t.Run("a delete across a series is one action", func(t *testing.T) {
		w := newWorld(t)
		tr := w.create(t, w.phone())
		if err := w.store.Delete(tr.ID, ScopeAll); err != nil {
			t.Fatal(err)
		}
		es := w.audit(t, "transaction")
		if len(es) != 2 || es[1].Action != "deleted" || es[1].EntityID != tr.ID {
			t.Fatalf("trail = %+v; want one deleted row", es)
		}
	})

	t.Run("a transfer is one transfer row, not two transaction rows", func(t *testing.T) {
		w := newWorld(t)
		tr := w.move()
		if err := w.store.Transfer(&tr); err != nil {
			t.Fatal(err)
		}
		es := w.audit(t, "transfer")
		if len(es) != 1 || es[0].Action != "created" || es[0].EntityID != tr.Group {
			t.Fatalf("transfer trail = %+v; want one created row under group %d", es, tr.Group)
		}
		if es := w.audit(t, "transaction"); len(es) != 0 {
			t.Fatalf("transaction trail = %+v; want the legs unlogged", es)
		}
	})

	t.Run("editing a transfer is one action", func(t *testing.T) {
		w := newWorld(t)
		tr := w.move()
		if err := w.store.Transfer(&tr); err != nil {
			t.Fatal(err)
		}
		tr.FromValue, tr.ToValue = 60000, 60000
		if err := w.store.UpdateTransfer(tr); err != nil {
			t.Fatal(err)
		}
		es := w.audit(t, "transfer")
		if len(es) != 2 || es[1].Action != "edited" {
			t.Fatalf("trail = %+v; want a created then one edited row", es)
		}
		if !strings.Contains(es[1].Changes, `"from_value"`) || strings.Contains(es[1].Changes, `"title"`) {
			t.Errorf("changes = %s; want the amount move and nothing else", es[1].Changes)
		}
	})

	t.Run("deleting a transfer from either leg is one transfer row", func(t *testing.T) {
		for _, leg := range []string{"outgoing", "incoming"} {
			w := newWorld(t)
			tr := w.move()
			if err := w.store.Transfer(&tr); err != nil {
				t.Fatal(err)
			}
			id := tr.Group
			if leg == "incoming" {
				legs, err := w.store.legsOf(tr.Group)
				if err != nil {
					t.Fatal(err)
				}
				for _, l := range legs {
					if l.ID != tr.Group {
						id = l.ID
					}
				}
			}
			if err := w.store.Delete(id, ScopeOne); err != nil {
				t.Fatal(err)
			}
			es := w.audit(t, "transfer")
			if len(es) != 2 || es[1].Action != "deleted" || es[1].EntityID != tr.Group {
				t.Fatalf("%s leg: trail = %+v; want one transfer deleted row", leg, es)
			}
			if es := w.audit(t, "transaction"); len(es) != 0 {
				t.Fatalf("%s leg: transaction trail = %+v; want none", leg, es)
			}
		}
	})

	t.Run("paying a bill is one action", func(t *testing.T) {
		w := newWorld(t)
		bill := w.bill(t, w.nucrd, "2026-08-15", "2026-08-22", 20000)
		if err := w.store.PayBill(bill, w.inter.ID, 20000, "2026-08-16"); err != nil {
			t.Fatal(err)
		}
		if es := w.audit(t, "transaction"); len(es) != 1 {
			t.Fatalf("trail has %d transaction rows for a payment; want 1", len(es))
		}
	})

	t.Run("a refused write records nothing", func(t *testing.T) {
		w := newWorld(t)
		if _, err := w.accounts.ToggleFreeze(w.inter.ID); err != nil {
			t.Fatal(err)
		}
		tr := w.tx()
		if err := w.store.Create(&tr); err == nil {
			t.Fatal("a transaction onto a frozen account was accepted")
		}
		if es := w.audit(t, "transaction"); len(es) != 0 {
			t.Fatalf("trail = %+v; want nothing for a refused write", es)
		}
	})
}

func TestAdjustments(t *testing.T) {
	adj := func(w *world, value int64) Transaction {
		return Transaction{
			Title: "Balance adjustment", Value: value, Kind: KindAdjustment,
			Date: "2026-08-27", Account: Ref{ID: w.inter.ID},
		}
	}

	t.Run("a signed adjustment moves the balance either way", func(t *testing.T) {
		w := newWorld(t)
		down := adj(w, -5000)
		if err := w.store.Create(&down); err != nil {
			t.Fatal(err)
		}
		if got := w.accountBalance(t, w.inter.ID); got != 95000 {
			t.Fatalf("balance = %d after a -5000 adjustment; want 95000", got)
		}
		up := adj(w, 30000)
		if err := w.store.Create(&up); err != nil {
			t.Fatal(err)
		}
		if got := w.accountBalance(t, w.inter.ID); got != 125000 {
			t.Fatalf("balance = %d after a +30000 adjustment; want 125000", got)
		}
	})

	t.Run("deleting an adjustment reverts it", func(t *testing.T) {
		w := newWorld(t)
		a := adj(w, -5000)
		if err := w.store.Create(&a); err != nil {
			t.Fatal(err)
		}
		if err := w.store.Delete(a.ID, ScopeOne); err != nil {
			t.Fatal(err)
		}
		if got := w.accountBalance(t, w.inter.ID); got != 100000 {
			t.Fatalf("balance = %d after deleting the adjustment; want back at 100000", got)
		}
	})

	t.Run("an adjustment is not edited", func(t *testing.T) {
		w := newWorld(t)
		a := adj(w, -5000)
		if err := w.store.Create(&a); err != nil {
			t.Fatal(err)
		}
		a.Value = -6000
		err := w.store.Update(a, ScopeOne)
		if err == nil || !strings.Contains(err.Error(), "delete it") {
			t.Fatalf("Update on an adjustment = %v; want the refusal", err)
		}
	})

	t.Run("nothing is turned into an adjustment", func(t *testing.T) {
		w := newWorld(t)
		tr := w.create(t, w.tx())
		tr.Kind, tr.Value, tr.Category = KindAdjustment, -5000, Ref{}
		if err := w.store.Update(tr, ScopeOne); err == nil {
			t.Fatal("Update turned a transaction into an adjustment; want a refusal")
		}
	})

	t.Run("a frozen account refuses an adjustment", func(t *testing.T) {
		w := newWorld(t)
		if _, err := w.accounts.ToggleFreeze(w.inter.ID); err != nil {
			t.Fatal(err)
		}
		a := adj(w, -5000)
		if err := w.store.Create(&a); err == nil {
			t.Fatal("an adjustment landed on a frozen account; want the freeze to refuse it")
		}
	})
}

// openBill plants a still-open bill row, the shape Ensure would have made.
func (w *world) openBill(t *testing.T, card cards.Card, closesOn, dueOn string) int64 {
	t.Helper()
	res, err := w.conn.Exec(
		`INSERT INTO card_bills (card_id, closes_on, due_on) VALUES (?, ?, ?)`,
		card.ID, closesOn, dueOn)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (w *world) billTotal(t *testing.T, id int64) int64 {
	t.Helper()
	var total int64
	if err := w.conn.QueryRow(`SELECT total FROM card_bills WHERE id = ?`, id).Scan(&total); err != nil {
		t.Fatal(err)
	}
	return total
}

func TestChargesRefreshTheirBill(t *testing.T) {
	// NUCRD closes on the 15th: the 2026-09-15 bill covers 08-16 through 09-15.
	t.Run("a charge lands on its bill at write time", func(t *testing.T) {
		w := newWorld(t)
		bill := w.openBill(t, w.nucrd, "2026-09-15", "2026-09-22")

		tr := w.tx()
		tr.Account, tr.Card, tr.Date = Ref{}, Ref{ID: w.nucrd.ID}, "2026-08-20"
		tr = w.create(t, tr)
		if got := w.billTotal(t, bill); got != tr.Value {
			t.Fatalf("bill total = %d after the charge, before any read; want %d", got, tr.Value)
		}

		if err := w.store.Delete(tr.ID, ScopeOne); err != nil {
			t.Fatal(err)
		}
		if got := w.billTotal(t, bill); got != 0 {
			t.Fatalf("bill total = %d after the delete; want it given back", got)
		}
	})

	t.Run("an edit moving the date moves the totals with it", func(t *testing.T) {
		w := newWorld(t)
		aug := w.openBill(t, w.nucrd, "2026-08-15", "2026-08-22")
		sep := w.openBill(t, w.nucrd, "2026-09-15", "2026-09-22")

		tr := w.tx()
		tr.Account, tr.Card, tr.Date = Ref{}, Ref{ID: w.nucrd.ID}, "2026-08-10"
		tr = w.create(t, tr)
		if got := w.billTotal(t, aug); got != tr.Value {
			t.Fatalf("august total = %d; want the charge on it", got)
		}

		loaded, err := w.store.Get(tr.ID)
		if err != nil {
			t.Fatal(err)
		}
		loaded.Date = "2026-08-20"
		if err := w.store.Update(loaded, ScopeOne); err != nil {
			t.Fatal(err)
		}
		if got := w.billTotal(t, aug); got != 0 {
			t.Fatalf("august total = %d after the move; want 0", got)
		}
		if got := w.billTotal(t, sep); got != tr.Value {
			t.Fatalf("september total = %d after the move; want the charge", got)
		}
	})

	t.Run("a charge on the closing day belongs to that bill", func(t *testing.T) {
		w := newWorld(t)
		bill := w.openBill(t, w.nucrd, "2026-09-15", "2026-09-22")
		tr := w.tx()
		tr.Account, tr.Card, tr.Date = Ref{}, Ref{ID: w.nucrd.ID}, "2026-09-15"
		tr = w.create(t, tr)
		if got := w.billTotal(t, bill); got != tr.Value {
			t.Fatalf("bill total = %d for a closing-day charge; want %d", got, tr.Value)
		}
	})

	t.Run("a bill that does not exist is not created", func(t *testing.T) {
		w := newWorld(t)
		tr := w.phone() // 6 installments on the card, reaching months ahead
		w.create(t, tr)
		var n int
		if err := w.conn.QueryRow(`SELECT count(*) FROM card_bills`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%d bill rows appeared from a write; want generation left to Ensure", n)
		}
	})

	t.Run("a closed bill's total stays frozen", func(t *testing.T) {
		w := newWorld(t)
		frozen := w.bill(t, w.nucrd, "2026-08-15", "2026-08-22", 20000)
		tr := w.tx()
		tr.Account, tr.Card, tr.Date = Ref{}, Ref{ID: w.nucrd.ID}, "2026-08-10"
		w.create(t, tr)
		if got := w.billTotal(t, frozen); got != 20000 {
			t.Fatalf("closed total = %d after a backdated charge; want it frozen at 20000", got)
		}
	})
}
