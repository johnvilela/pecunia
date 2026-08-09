package transactions

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"kakei/internal/accounts"
	"kakei/internal/cards"
	"kakei/internal/categories"
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
		if err := cs.Create(c); err != nil {
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
		if err := w.store.Update(made); err == nil {
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
		if err := w.store.Update(made); err != nil {
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
		if err := w.store.Update(made); err != nil {
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
		if err := w.store.Update(made); err != nil {
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
		if err := w.store.Update(made); err != nil {
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
		if err := w.store.Update(made); err != nil {
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
		if err := w.store.Update(made); err != nil {
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
		if err := w.store.Update(tr); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update(999) = %v; want ErrNotFound", err)
		}
	})

	t.Run("a broken edit leaves both the row and the balance alone", func(t *testing.T) {
		w := newWorld(t)
		made := w.create(t, w.tx())
		made.Title = ""
		if err := w.store.Update(made); err == nil {
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
		if err := w.store.Delete(made.ID); err != nil {
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
		if err := w.store.Delete(made.ID); err != nil {
			t.Fatal(err)
		}
		if got := w.cardBalance(t, w.nucrd.ID); got != 0 {
			t.Fatalf("card balance = %d; want it back at 0", got)
		}
	})

	t.Run("an unknown id is not found", func(t *testing.T) {
		w := newWorld(t)
		if err := w.store.Delete(999); !errors.Is(err, ErrNotFound) {
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
