package budgets

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"kakei/internal/transactions"
)

func newTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	conn := newTestDB(t)
	return NewStore(conn), conn
}

// account and card are the two things a transaction can be filed against, and
// the only place a transaction's currency comes from. Raw SQL, so a case builds
// exactly the row it means to.
func account(t *testing.T, conn *sql.DB, code, currency string) int64 {
	t.Helper()
	res, err := conn.Exec(
		`INSERT INTO accounts (code, name, color, balance, currency) VALUES (?, ?, 'orange', 0, ?)`,
		code, code, currency)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func card(t *testing.T, conn *sql.DB, code, currency string) int64 {
	t.Helper()
	res, err := conn.Exec(
		`INSERT INTO credit_cards (code, name, color, credit_limit, balance, currency, closing_day, due_day)
		 VALUES (?, ?, 'violet', 1000000, 0, ?, 20, 28)`, code, code, currency)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// fileOn writes one transaction against an account. categoryID may be 0, which
// is a transaction no budget can ever see.
func fileOn(t *testing.T, conn *sql.DB, accountID, categoryID int64, kind string, value int64, date string) {
	t.Helper()
	var cat any
	if categoryID != 0 {
		cat = categoryID
	}
	if _, err := conn.Exec(
		`INSERT INTO transactions (title, account_id, category_id, value, kind, date)
		 VALUES ('Something', ?, ?, ?, ?, ?)`, accountID, cat, value, kind, date); err != nil {
		t.Fatal(err)
	}
}

// fileCard is fileOn against a credit card instead, which is what proves a
// charge counts on the day it was charged rather than the day the bill is paid.
func fileCard(t *testing.T, conn *sql.DB, cardID, categoryID int64, kind string, value int64, date string) {
	t.Helper()
	if _, err := conn.Exec(
		`INSERT INTO transactions (title, card_id, category_id, value, kind, date)
		 VALUES ('Something', ?, ?, ?, ?, ?)`, cardID, categoryID, value, kind, date); err != nil {
		t.Fatal(err)
	}
}

// seed is the budget every case starts from, already in the database.
func seed(t *testing.T, s *Store, conn *sql.DB) (Budget, int64) {
	t.Helper()
	cat := category(t, conn, "FOODC")
	b := Budget{
		Code: "FOOD1", Name: "Food", Description: "groceries and eating out",
		Amount: 80000, Currency: "BRL", Color: "green",
		Category: refTo(cat),
	}
	if err := s.Create(&b); err != nil {
		t.Fatalf("create: %v", err)
	}
	return b, cat
}

func TestCreateAndGet(t *testing.T) {
	t.Run("a budget comes back as it was written", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, cat := seed(t, s, conn)
		if b.ID == 0 {
			t.Fatal("Create left the id at zero")
		}

		got, err := s.Get(b.ID, "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		if got.Code != "FOOD1" || got.Name != "Food" || got.Amount != 80000 || got.Currency != "BRL" {
			t.Fatalf("Get = %+v; want the budget that was written", got)
		}
		if got.Category.ID != cat || got.Category.Code != "FOODC" {
			t.Fatalf("category = %+v; want the one it caps, joined in", got.Category)
		}
		if !got.Active {
			t.Error("a new budget came back archived; want it active")
		}
		if got.Cycle != "2026-08" {
			t.Errorf("Cycle = %q; want the one it was read for", got.Cycle)
		}
	})

	t.Run("a budget with nothing under it is at zero", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, _ := seed(t, s, conn)
		got, err := s.Get(b.ID, "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		if got.Spent != 0 {
			t.Fatalf("Spent = %d; want zero", got.Spent)
		}
	})

	t.Run("a lowercase code is stored uppercase", func(t *testing.T) {
		s, conn := newTestStore(t)
		cat := category(t, conn, "FOODC")
		b := Budget{Code: "food1", Name: "Food", Amount: 80000, Currency: "BRL",
			Color: "green", Category: refTo(cat)}
		if err := s.Create(&b); err != nil {
			t.Fatal(err)
		}
		got, err := s.ByCode("FOOD1", "2026-08")
		if err != nil {
			t.Fatalf("ByCode after a lowercase create: %v", err)
		}
		if got.Code != "FOOD1" {
			t.Fatalf("code = %q; want FOOD1", got.Code)
		}
	})

	t.Run("an unknown id is not found", func(t *testing.T) {
		s, _ := newTestStore(t)
		if _, err := s.Get(404, "2026-08"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get(404) = %v; want ErrNotFound", err)
		}
	})

	t.Run("a broken budget never reaches the table", func(t *testing.T) {
		s, conn := newTestStore(t)
		cat := category(t, conn, "FOODC")
		b := Budget{Code: "FOOD1", Name: "Food", Amount: 0, Currency: "BRL",
			Color: "green", Category: refTo(cat)}
		if err := s.Create(&b); err == nil {
			t.Fatal("Create with a zero amount succeeded; want Validate to refuse it")
		}
		all, err := s.List("2026-08", true)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 0 {
			t.Fatalf("List = %d budgets; want none written", len(all))
		}
	})

	t.Run("a second budget over the same category reads as a sentence", func(t *testing.T) {
		s, conn := newTestStore(t)
		_, cat := seed(t, s, conn)
		dup := Budget{Code: "FOOD2", Name: "Food again", Amount: 50000, Currency: "BRL",
			Color: "red", Category: refTo(cat)}
		err := s.Create(&dup)
		if err == nil {
			t.Fatal("a second BRL budget over the category was written; want it refused")
		}
		if strings.Contains(err.Error(), "UNIQUE") {
			t.Fatalf("err = %v; want the constraint said in words", err)
		}
		if !strings.Contains(err.Error(), "already") {
			t.Fatalf("err = %v; want it to say the category is already capped", err)
		}
	})

	t.Run("an unreadable cycle is refused rather than read as nothing", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, _ := seed(t, s, conn)
		if _, err := s.Get(b.ID, "August"); err == nil {
			t.Fatal("Get with a broken cycle succeeded; want it refused")
		}
	})
}

// What a budget is at is the ledger's business, and these are every way the
// ledger can be misread.
func TestSpend(t *testing.T) {
	t.Run("outcome in the cycle counts", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, cat := seed(t, s, conn)
		acc := account(t, conn, "WLLT1", "BRL")
		fileOn(t, conn, acc, cat, "outcome", 12000, "2026-08-03")
		fileOn(t, conn, acc, cat, "outcome", 42000, "2026-08-19")

		got, err := s.Get(b.ID, "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		if got.Spent != 54000 {
			t.Fatalf("Spent = %d; want 54000", got.Spent)
		}
	})

	t.Run("a refund gives the budget its money back", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, cat := seed(t, s, conn)
		acc := account(t, conn, "WLLT1", "BRL")
		fileOn(t, conn, acc, cat, "outcome", 20000, "2026-08-03")
		fileOn(t, conn, acc, cat, "income", 4000, "2026-08-05")

		got, err := s.Get(b.ID, "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		if got.Spent != 16000 {
			t.Fatalf("Spent = %d; want 16000, the R$40.00 refund netted off", got.Spent)
		}
	})

	t.Run("another month is another budget's business", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, cat := seed(t, s, conn)
		acc := account(t, conn, "WLLT1", "BRL")
		fileOn(t, conn, acc, cat, "outcome", 12000, "2026-07-31")
		fileOn(t, conn, acc, cat, "outcome", 30000, "2026-08-01")
		fileOn(t, conn, acc, cat, "outcome", 15000, "2026-09-01")

		got, err := s.Get(b.ID, "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		if got.Spent != 30000 {
			t.Fatalf("Spent = %d; want only August's 30000", got.Spent)
		}
	})

	t.Run("another category is not counted", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, cat := seed(t, s, conn)
		other := category(t, conn, "FUELC")
		acc := account(t, conn, "WLLT1", "BRL")
		fileOn(t, conn, acc, cat, "outcome", 20000, "2026-08-03")
		fileOn(t, conn, acc, other, "outcome", 50000, "2026-08-04")

		got, err := s.Get(b.ID, "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		if got.Spent != 20000 {
			t.Fatalf("Spent = %d; want only the food 20000", got.Spent)
		}
	})

	t.Run("an uncategorised transaction is caught by no budget", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, _ := seed(t, s, conn)
		acc := account(t, conn, "WLLT1", "BRL")
		fileOn(t, conn, acc, 0, "outcome", 20000, "2026-08-03")

		got, err := s.Get(b.ID, "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		if got.Spent != 0 {
			t.Fatalf("Spent = %d; want zero — it has no category to be caught by", got.Spent)
		}
	})

	t.Run("another currency is not counted", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, cat := seed(t, s, conn)
		brl := account(t, conn, "WLLT1", "BRL")
		btc := account(t, conn, "COLD1", "BTC")
		fileOn(t, conn, brl, cat, "outcome", 20000, "2026-08-03")
		// The same integer means eight decimal places over here. Adding them
		// would be the one sum kakei never makes.
		fileOn(t, conn, btc, cat, "outcome", 20000, "2026-08-04")

		got, err := s.Get(b.ID, "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		if got.Spent != 20000 {
			t.Fatalf("Spent = %d; want only the reais 20000", got.Spent)
		}
	})

	t.Run("a card charge counts on the day it was charged", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, cat := seed(t, s, conn)
		cc := card(t, conn, "NUBNK", "BRL")
		// Charged in August, settled in September. The budget is about what was
		// consumed, so this is August's.
		fileCard(t, conn, cc, cat, "outcome", 20000, "2026-08-15")

		got, err := s.Get(b.ID, "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		if got.Spent != 20000 {
			t.Fatalf("Spent = %d; want the card charge counted in August", got.Spent)
		}
	})

	t.Run("a card in another currency is not counted", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, cat := seed(t, s, conn)
		cc := card(t, conn, "USDCC", "USD")
		fileCard(t, conn, cc, cat, "outcome", 20000, "2026-08-15")

		got, err := s.Get(b.ID, "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		if got.Spent != 0 {
			t.Fatalf("Spent = %d; want zero — the card is in dollars", got.Spent)
		}
	})

	t.Run("one installment counts, not the whole purchase", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, cat := seed(t, s, conn)
		cc := card(t, conn, "NUBNK", "BRL")
		// A R$1200.00 purchase over six bills is six rows on six dates, so each
		// month sees its own sixth with no case for it anywhere.
		for i, date := range []string{
			"2026-08-15", "2026-09-15", "2026-10-15",
			"2026-11-15", "2026-12-15", "2027-01-15",
		} {
			_ = i
			fileCard(t, conn, cc, cat, "outcome", 20000, date)
		}

		got, err := s.Get(b.ID, "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		if got.Spent != 20000 {
			t.Fatalf("Spent = %d; want one installment's 20000, not the whole 120000", got.Spent)
		}
	})
}

func TestList(t *testing.T) {
	t.Run("budgets come back by name with their spend", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, cat := seed(t, s, conn)
		fuel := category(t, conn, "FUELC")
		if err := s.Create(&Budget{Code: "FUEL1", Name: "Fuel", Amount: 30000,
			Currency: "BRL", Color: "amber", Category: refTo(fuel)}); err != nil {
			t.Fatal(err)
		}
		acc := account(t, conn, "WLLT1", "BRL")
		fileOn(t, conn, acc, cat, "outcome", 54000, "2026-08-10")

		all, err := s.List("2026-08", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 2 {
			t.Fatalf("List = %d budgets; want 2", len(all))
		}
		if all[0].Name != "Food" || all[1].Name != "Fuel" {
			t.Fatalf("order = %s, %s; want them by name", all[0].Name, all[1].Name)
		}
		if all[0].Spent != 54000 || all[1].Spent != 0 {
			t.Fatalf("spend = %d, %d; want 54000 and 0", all[0].Spent, all[1].Spent)
		}
		if all[0].ID != b.ID {
			t.Errorf("first id = %d; want the food budget %d", all[0].ID, b.ID)
		}
	})

	t.Run("an archived budget is left out unless asked for", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, _ := seed(t, s, conn)
		if err := s.SetActive(b.ID, false); err != nil {
			t.Fatal(err)
		}

		live, err := s.List("2026-08", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(live) != 0 {
			t.Fatalf("List(archived=false) = %d; want the archived one left out", len(live))
		}

		all, err := s.List("2026-08", true)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 1 || all[0].Active {
			t.Fatalf("List(archived=true) = %+v; want the archived budget, marked archived", all)
		}
	})
}

func TestUpdate(t *testing.T) {
	t.Run("a moved amount is logged with its reason", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, _ := seed(t, s, conn)
		b.Amount = 95000
		if err := s.Update(b, "rice went up"); err != nil {
			t.Fatal(err)
		}

		log, err := s.AmountLog(b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(log) != 1 {
			t.Fatalf("AmountLog = %d entries; want 1", len(log))
		}
		if log[0].Previous != 80000 || log[0].Amount != 95000 || log[0].Note != "rice went up" {
			t.Fatalf("entry = %+v; want 80000 → 95000 because rice went up", log[0])
		}
	})

	t.Run("an amount that did not move has nothing to explain", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, _ := seed(t, s, conn)
		b.Name = "Food and drink"
		if err := s.Update(b, "a note nobody asked for"); err != nil {
			t.Fatal(err)
		}

		log, err := s.AmountLog(b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(log) != 0 {
			t.Fatalf("AmountLog = %d entries; want none — the amount stayed put", len(log))
		}
		got, err := s.Get(b.ID, "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "Food and drink" {
			t.Fatalf("name = %q; want the edit kept", got.Name)
		}
	})

	t.Run("the log reads newest first", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, _ := seed(t, s, conn)
		b.Amount = 85000
		if err := s.Update(b, "first move"); err != nil {
			t.Fatal(err)
		}
		b.Amount = 95000
		if err := s.Update(b, "second move"); err != nil {
			t.Fatal(err)
		}

		log, err := s.AmountLog(b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(log) != 2 {
			t.Fatalf("AmountLog = %d entries; want 2", len(log))
		}
		if log[0].Note != "second move" || log[1].Note != "first move" {
			t.Fatalf("order = %q, %q; want the newest first", log[0].Note, log[1].Note)
		}
	})

	t.Run("the currency is frozen once anything has been counted", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, cat := seed(t, s, conn)
		acc := account(t, conn, "WLLT1", "BRL")
		fileOn(t, conn, acc, cat, "outcome", 20000, "2026-08-03")

		b.Currency = "BTC"
		err := s.Update(b, "")
		if err == nil {
			t.Fatal("the currency was changed under counted transactions; want it refused")
		}
		if !strings.Contains(err.Error(), "BRL") {
			t.Fatalf("err = %v; want it to name the currency that is already counted", err)
		}
	})

	t.Run("the currency moves freely while nothing has been counted", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, _ := seed(t, s, conn)
		b.Currency = "USD"
		if err := s.Update(b, ""); err != nil {
			t.Fatalf("changing the currency of an empty budget: %v", err)
		}
		got, err := s.Get(b.ID, "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		if got.Currency != "USD" {
			t.Fatalf("currency = %q; want USD", got.Currency)
		}
	})

	t.Run("a transaction in another currency does not freeze it", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, cat := seed(t, s, conn)
		btc := account(t, conn, "COLD1", "BTC")
		// The budget counts reais; this row was never its business.
		fileOn(t, conn, btc, cat, "outcome", 20000, "2026-08-03")

		b.Currency = "USD"
		if err := s.Update(b, ""); err != nil {
			t.Fatalf("a satoshi row froze a reais budget: %v", err)
		}
	})

	t.Run("moving onto a category already capped reads as a sentence", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, _ := seed(t, s, conn)
		fuel := category(t, conn, "FUELC")
		other := Budget{Code: "FUEL1", Name: "Fuel", Amount: 30000, Currency: "BRL",
			Color: "amber", Category: refTo(fuel)}
		if err := s.Create(&other); err != nil {
			t.Fatal(err)
		}

		b.Category = refTo(fuel)
		err := s.Update(b, "")
		if err == nil {
			t.Fatal("two budgets landed on one category; want it refused")
		}
		if strings.Contains(err.Error(), "UNIQUE") {
			t.Fatalf("err = %v; want the constraint said in words", err)
		}
	})

	t.Run("an unknown id is not found", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, _ := seed(t, s, conn)
		b.ID = 404
		if err := s.Update(b, ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update(404) = %v; want ErrNotFound", err)
		}
	})
}

func TestDelete(t *testing.T) {
	t.Run("deleting a budget takes no transactions with it", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, cat := seed(t, s, conn)
		acc := account(t, conn, "WLLT1", "BRL")
		fileOn(t, conn, acc, cat, "outcome", 20000, "2026-08-03")

		if err := s.Delete(b.ID); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := conn.QueryRow(`SELECT count(*) FROM transactions`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("%d transactions left; want the one that really moved to stay", n)
		}
		// Nothing ever named the budget, so the category is untouched too.
		if err := conn.QueryRow(`SELECT count(*) FROM categories`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("%d categories left; want the category to outlive its budget", n)
		}
	})

	t.Run("an unknown id is not found", func(t *testing.T) {
		s, _ := newTestStore(t)
		if err := s.Delete(404); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Delete(404) = %v; want ErrNotFound", err)
		}
	})
}

func TestResolveAndCodes(t *testing.T) {
	t.Run("a code resolves, in any case", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, _ := seed(t, s, conn)
		for _, ref := range []string{"FOOD1", "food1", " FOOD1 "} {
			got, err := s.Resolve(ref, "2026-08")
			if err != nil {
				t.Fatalf("Resolve(%q): %v", ref, err)
			}
			if got.ID != b.ID {
				t.Fatalf("Resolve(%q) = %d; want %d", ref, got.ID, b.ID)
			}
		}
	})

	t.Run("an id resolves too", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, _ := seed(t, s, conn)
		got, err := s.Resolve("1", "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != b.ID {
			t.Fatalf("Resolve(\"1\") = %d; want %d", got.ID, b.ID)
		}
	})

	t.Run("a taken code is reported taken", func(t *testing.T) {
		s, conn := newTestStore(t)
		seed(t, s, conn)
		taken, err := s.CodeTaken("food1")
		if err != nil {
			t.Fatal(err)
		}
		if !taken {
			t.Fatal("CodeTaken(food1) = false; want it taken whatever the case")
		}
		if taken, err = s.CodeTaken("XXXXX"); err != nil || taken {
			t.Fatalf("CodeTaken(XXXXX) = %v, %v; want free", taken, err)
		}
	})

	t.Run("a suggested code is free and the right length", func(t *testing.T) {
		s, conn := newTestStore(t)
		seed(t, s, conn)
		code, err := s.SuggestCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 5 {
			t.Fatalf("SuggestCode() = %q; want 5 characters", code)
		}
		taken, err := s.CodeTaken(code)
		if err != nil {
			t.Fatal(err)
		}
		if taken {
			t.Fatalf("SuggestCode() = %q, which is already taken", code)
		}
	})
}

// History is what makes a budget more than this month's number: a cap missed
// every month is a cap, not a spending problem.
func TestHistory(t *testing.T) {
	t.Run("spend comes back a month at a time, oldest first", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, cat := seed(t, s, conn)
		acc := account(t, conn, "WLLT1", "BRL")
		fileOn(t, conn, acc, cat, "outcome", 70000, "2026-06-10")
		fileOn(t, conn, acc, cat, "outcome", 82000, "2026-07-11")
		fileOn(t, conn, acc, cat, "outcome", 54000, "2026-08-12")

		got, err := s.Get(b.ID, "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		months, err := s.History(got, 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(months) != 3 {
			t.Fatalf("History = %d months; want 3", len(months))
		}
		want := []CycleSpend{{"2026-06", 70000}, {"2026-07", 82000}, {"2026-08", 54000}}
		for i, w := range want {
			if months[i] != w {
				t.Fatalf("month %d = %+v; want %+v", i, months[i], w)
			}
		}
	})

	t.Run("a month with nothing in it still shows up", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, cat := seed(t, s, conn)
		acc := account(t, conn, "WLLT1", "BRL")
		fileOn(t, conn, acc, cat, "outcome", 54000, "2026-08-12")

		got, err := s.Get(b.ID, "2026-08")
		if err != nil {
			t.Fatal(err)
		}
		months, err := s.History(got, 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(months) != 3 {
			t.Fatalf("History = %d months; want 3, quiet ones included", len(months))
		}
		if months[0].Spent != 0 || months[1].Spent != 0 {
			t.Fatalf("quiet months = %d, %d; want zero, not missing", months[0].Spent, months[1].Spent)
		}
	})
}

// Counted is what stands between a budget and a currency change, so it has to
// count exactly what the budget itself would.
func TestCounted(t *testing.T) {
	s, conn := newTestStore(t)
	_, cat := seed(t, s, conn)
	brl := account(t, conn, "WLLT1", "BRL")
	btc := account(t, conn, "COLD1", "BTC")
	other := category(t, conn, "FUELC")

	fileOn(t, conn, brl, cat, "outcome", 20000, "2026-08-03")
	fileOn(t, conn, brl, cat, "income", 4000, "2026-08-04")
	fileOn(t, conn, btc, cat, "outcome", 20000, "2026-08-05")
	fileOn(t, conn, brl, other, "outcome", 20000, "2026-08-06")

	n, err := s.Counted(cat, "BRL")
	if err != nil {
		t.Fatal(err)
	}
	// Both directions count: an income row is as much this budget's business as
	// an outcome one, and it is filed in reais either way.
	if n != 2 {
		t.Fatalf("Counted = %d; want the 2 reais rows in this category", n)
	}
}

// refTo is the category reference a budget carries. Only the id is needed to
// write one; the rest is joined back in on read.
func refTo(id int64) transactions.Ref { return transactions.Ref{ID: id} }

// Deleting a category takes its budgets with it, by the cascade in the schema.
// This is what lets the confirmation say so before it happens.
func TestCountForCategory(t *testing.T) {
	t.Run("counts the budgets over one category, whatever their currency", func(t *testing.T) {
		s, conn := newTestStore(t)
		_, cat := seed(t, s, conn)
		if err := s.Create(&Budget{Code: "FOOD2", Name: "Food in satoshis", Amount: 500000,
			Currency: "BTC", Color: "amber", Category: refTo(cat)}); err != nil {
			t.Fatal(err)
		}
		other := category(t, conn, "FUELC")
		if err := s.Create(&Budget{Code: "FUEL1", Name: "Fuel", Amount: 30000,
			Currency: "BRL", Color: "teal", Category: refTo(other)}); err != nil {
			t.Fatal(err)
		}

		n, err := s.CountForCategory(cat)
		if err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("CountForCategory = %d; want the 2 over that category", n)
		}
	})

	t.Run("an archived budget still goes with the category", func(t *testing.T) {
		s, conn := newTestStore(t)
		b, cat := seed(t, s, conn)
		if err := s.SetActive(b.ID, false); err != nil {
			t.Fatal(err)
		}
		n, err := s.CountForCategory(cat)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("CountForCategory = %d; want the archived one counted too", n)
		}
	})

	t.Run("a category nothing caps counts zero", func(t *testing.T) {
		s, conn := newTestStore(t)
		seed(t, s, conn)
		n, err := s.CountForCategory(category(t, conn, "FUELC"))
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("CountForCategory = %d; want 0", n)
		}
	})
}
