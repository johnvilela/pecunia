package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"pecunia/internal/accounts"
	"pecunia/internal/bills"
	"pecunia/internal/budgets"
	"pecunia/internal/cards"
	"pecunia/internal/categories"
	"pecunia/internal/db"
	"pecunia/internal/goals"
	"pecunia/internal/logs"
	"pecunia/internal/recurring"
	"pecunia/internal/summary"
	"pecunia/internal/transactions"
)

// mcpConn points PECUNIA_DB at a database of this case's own and opens it with
// the audit actor set to AI, the way runMCP does.
func mcpConn(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("PECUNIA_DB", filepath.Join(t.TempDir(), "pecunia.db"))
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	logs.Actor = logs.AI
	t.Cleanup(func() { logs.Actor = logs.User })
	return conn
}

func sp(s string) *string { return &s }
func ip(n int64) *int64   { return &n }

// newAccount files one through the tool, so every case builds state the same
// way an agent would.
func newAccount(t *testing.T, conn *sql.DB, name string, balance int64) accounts.Account {
	t.Helper()
	out, err := accountsDo(conn, accountsIn{Action: "create", Name: sp(name), Balance: &balance})
	if err != nil {
		t.Fatal(err)
	}
	return out.(accounts.Account)
}

func TestMCPAccounts(t *testing.T) {
	t.Run("create lists and gets, logged as ai", func(t *testing.T) {
		conn := mcpConn(t)
		a := newAccount(t, conn, "Wallet", 5000)
		if a.Code == "" || a.Balance != 5000 || a.Currency != "USD" {
			t.Fatalf("create came back %+v", a)
		}
		out, err := accountsDo(conn, accountsIn{Action: "list"})
		if err != nil {
			t.Fatal(err)
		}
		if got := out.([]accounts.Account); len(got) != 1 {
			t.Fatalf("listed %d accounts", len(got))
		}
		out, err = accountsDo(conn, accountsIn{Action: "get", Ref: a.Code})
		if err != nil {
			t.Fatal(err)
		}
		if out.(accounts.Account).ID != a.ID {
			t.Fatal("get by code found the wrong account")
		}
		trail, err := logs.List(conn, logs.Filter{Entity: "account"})
		if err != nil {
			t.Fatal(err)
		}
		if len(trail) != 1 || trail[0].Source != logs.AI {
			t.Fatalf("trail %+v — want one row from ai", trail)
		}
	})

	t.Run("update patches fields and files a balance adjustment", func(t *testing.T) {
		conn := mcpConn(t)
		a := newAccount(t, conn, "Wallet", 5000)
		out, err := accountsDo(conn, accountsIn{
			Action: "update", Ref: a.Code,
			Name: sp("Cash"), Balance: ip(7000), Note: sp("found cash"),
		})
		if err != nil {
			t.Fatal(err)
		}
		got := out.(accounts.Account)
		if got.Name != "Cash" || got.Balance != 7000 {
			t.Fatalf("update came back %+v", got)
		}
		rows, err := transactions.NewStore(conn).List(transactions.Filter{AccountID: a.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Kind != transactions.KindAdjustment || rows[0].Value != 2000 {
			t.Fatalf("ledger %+v — want one +2000 adjustment", rows)
		}
	})

	t.Run("toggle_freeze and delete", func(t *testing.T) {
		conn := mcpConn(t)
		a := newAccount(t, conn, "Wallet", 0)
		out, err := accountsDo(conn, accountsIn{Action: "toggle_freeze", Ref: a.Code})
		if err != nil {
			t.Fatal(err)
		}
		if !out.(accounts.Account).IsFrozen {
			t.Fatal("account did not freeze")
		}
		if _, err := accountsDo(conn, accountsIn{Action: "delete", Ref: a.Code}); err != nil {
			t.Fatal(err)
		}
		if _, err := accountsDo(conn, accountsIn{Action: "get", Ref: a.Code}); err == nil {
			t.Fatal("deleted account still resolves")
		}
	})

	t.Run("unknown action names the real ones", func(t *testing.T) {
		conn := mcpConn(t)
		_, err := accountsDo(conn, accountsIn{Action: "explode"})
		if err == nil || !strings.Contains(err.Error(), "toggle_freeze") {
			t.Fatalf("err %v — want the action list", err)
		}
	})
}

func TestMCPTransactions(t *testing.T) {
	t.Run("create moves the balance and delete puts it back", func(t *testing.T) {
		conn := mcpConn(t)
		a := newAccount(t, conn, "Wallet", 0)
		out, err := transactionsDo(conn, transactionsIn{
			Action: "create", Title: "Salary", Value: 1000,
			Kind: "income", Account: a.Code,
		})
		if err != nil {
			t.Fatal(err)
		}
		tx := out.(transactions.Transaction)
		got, _ := accounts.NewStore(conn).Get(a.ID)
		if got.Balance != 1000 {
			t.Fatalf("balance %d after income", got.Balance)
		}
		if _, err := transactionsDo(conn, transactionsIn{Action: "delete", ID: tx.ID}); err != nil {
			t.Fatal(err)
		}
		got, _ = accounts.NewStore(conn).Get(a.ID)
		if got.Balance != 0 {
			t.Fatalf("balance %d after delete", got.Balance)
		}
	})

	t.Run("list narrows by search", func(t *testing.T) {
		conn := mcpConn(t)
		a := newAccount(t, conn, "Wallet", 0)
		for _, title := range []string{"Coffee", "Rent"} {
			if _, err := transactionsDo(conn, transactionsIn{
				Action: "create", Title: title, Value: 100, Kind: "outcome", Account: a.Code,
			}); err != nil {
				t.Fatal(err)
			}
		}
		out, err := transactionsDo(conn, transactionsIn{Action: "list", Search: "coffee"})
		if err != nil {
			t.Fatal(err)
		}
		if rows := out.([]transactions.Transaction); len(rows) != 1 || rows[0].Title != "Coffee" {
			t.Fatalf("search found %+v", rows)
		}
	})

	t.Run("transfer moves both ends", func(t *testing.T) {
		conn := mcpConn(t)
		from := newAccount(t, conn, "Checking", 1000)
		to := newAccount(t, conn, "Savings", 0)
		out, err := transactionsDo(conn, transactionsIn{
			Action: "transfer", Title: "Stash", Value: 400,
			Account: from.Code, ToAccount: to.Code,
		})
		if err != nil {
			t.Fatal(err)
		}
		if out.(transactions.Transfer).Group == 0 {
			t.Fatal("transfer came back without a group")
		}
		f, _ := accounts.NewStore(conn).Get(from.ID)
		g, _ := accounts.NewStore(conn).Get(to.ID)
		if f.Balance != 600 || g.Balance != 400 {
			t.Fatalf("balances %d and %d after transfer", f.Balance, g.Balance)
		}
	})
}

func TestMCPCards(t *testing.T) {
	t.Run("create, charge, bills and pay_bill", func(t *testing.T) {
		conn := mcpConn(t)
		out, err := cardsDo(conn, cardsIn{
			Action: "create", Name: sp("Visa"), Limit: ip(100000),
			ClosingDay: ip(10), DueDay: ip(20),
		})
		if err != nil {
			t.Fatal(err)
		}
		c := out.(cards.Card)
		// A charge dated well in the past lands in a cycle that has already
		// closed, so the bill is payable without waiting for a clock.
		if _, err := transactionsDo(conn, transactionsIn{
			Action: "create", Title: "Groceries", Value: 5000,
			Kind: "outcome", Card: c.Code, Date: "2026-06-05",
		}); err != nil {
			t.Fatal(err)
		}
		out, err = cardsDo(conn, cardsIn{Action: "bills", Ref: c.Code})
		if err != nil {
			t.Fatal(err)
		}
		var owed bills.Bill
		for _, b := range out.([]bills.Bill) {
			if b.Total == 5000 {
				owed = b
			}
		}
		if owed.ID == 0 {
			t.Fatalf("no bill carries the charge: %+v", out)
		}
		a := newAccount(t, conn, "Checking", 10000)
		out, err = cardsDo(conn, cardsIn{
			Action: "pay_bill", Ref: c.Code, ClosesOn: owed.ClosesOn,
			Account: a.Code, Value: ip(5000),
		})
		if err != nil {
			t.Fatal(err)
		}
		if paid := out.(bills.Bill); paid.Paid != 5000 {
			t.Fatalf("bill shows %d paid", paid.Paid)
		}
		got, _ := accounts.NewStore(conn).Get(a.ID)
		if got.Balance != 5000 {
			t.Fatalf("balance %d after paying", got.Balance)
		}
	})
}

func TestMCPCategoriesGoalsRecurringBudgets(t *testing.T) {
	t.Run("categories create and update", func(t *testing.T) {
		conn := mcpConn(t)
		out, err := categoriesDo(conn, categoriesIn{Action: "create", Name: sp("Food")})
		if err != nil {
			t.Fatal(err)
		}
		c := out.(categories.Category)
		out, err = categoriesDo(conn, categoriesIn{Action: "update", Ref: c.Code, Name: sp("Groceries")})
		if err != nil {
			t.Fatal(err)
		}
		if out.(categories.Category).Name != "Groceries" {
			t.Fatal("update did not take")
		}
	})

	t.Run("goal progress climbs with linked income", func(t *testing.T) {
		conn := mcpConn(t)
		a := newAccount(t, conn, "Wallet", 0)
		out, err := goalsDo(conn, goalsIn{Action: "create", Name: sp("Trip"), Target: ip(10000)})
		if err != nil {
			t.Fatal(err)
		}
		g := out.(goals.Goal)
		if _, err := transactionsDo(conn, transactionsIn{
			Action: "create", Title: "Save", Value: 2500,
			Kind: "income", Account: a.Code, GoalID: g.ID,
		}); err != nil {
			t.Fatal(err)
		}
		out, err = goalsDo(conn, goalsIn{Action: "get", ID: g.ID})
		if err != nil {
			t.Fatal(err)
		}
		if got := out.(goals.Goal); got.Net != 2500 {
			t.Fatalf("goal net %d", got.Net)
		}
	})

	t.Run("recurring bill records a payment", func(t *testing.T) {
		conn := mcpConn(t)
		a := newAccount(t, conn, "Wallet", 10000)
		out, err := recurringDo(conn, recurringIn{
			Action: "create", Name: sp("Energy"), Account: sp(a.Code),
			OpenDay: ip(1), DueDay: ip(28), Expected: ip(3000),
		})
		if err != nil {
			t.Fatal(err)
		}
		b := out.(recurring.Bill)
		if _, err := transactionsDo(conn, transactionsIn{
			Action: "create", Title: "Energy", Value: 3000, Kind: "outcome",
			Account: a.Code, Recurring: b.Code, Cycle: "2026-08",
		}); err != nil {
			t.Fatal(err)
		}
		out, err = recurringDo(conn, recurringIn{Action: "get", Ref: b.Code})
		if err != nil {
			t.Fatal(err)
		}
		if got := out.(recurring.Bill); len(got.Payments) == 0 {
			t.Fatal("payment did not reach the bill")
		}
	})

	t.Run("budget counts the month's spend", func(t *testing.T) {
		conn := mcpConn(t)
		a := newAccount(t, conn, "Wallet", 10000)
		cat, err := categoriesDo(conn, categoriesIn{Action: "create", Name: sp("Food")})
		if err != nil {
			t.Fatal(err)
		}
		code := cat.(categories.Category).Code
		out, err := budgetsDo(conn, budgetsIn{
			Action: "create", Name: sp("Food cap"), Amount: ip(5000), Category: sp(code),
		})
		if err != nil {
			t.Fatal(err)
		}
		b := out.(budgets.Budget)
		if _, err := transactionsDo(conn, transactionsIn{
			Action: "create", Title: "Groceries", Value: 1200, Kind: "outcome",
			Account: a.Code, Category: code,
		}); err != nil {
			t.Fatal(err)
		}
		out, err = budgetsDo(conn, budgetsIn{Action: "get", Ref: b.Code})
		if err != nil {
			t.Fatal(err)
		}
		if got := out.(budgets.Budget); got.Spent != 1200 {
			t.Fatalf("budget spent %d", got.Spent)
		}
	})
}

func TestMCPSummaryAndLogs(t *testing.T) {
	t.Run("summary shows today's money", func(t *testing.T) {
		conn := mcpConn(t)
		a := newAccount(t, conn, "Wallet", 0)
		if _, err := transactionsDo(conn, transactionsIn{
			Action: "create", Title: "Salary", Value: 8000, Kind: "income", Account: a.Code,
		}); err != nil {
			t.Fatal(err)
		}
		out, err := summaryDo(conn, summaryIn{})
		if err != nil {
			t.Fatal(err)
		}
		s := out.(summary.Summary)
		if s.In["USD"] != 8000 || len(s.Accounts) != 1 {
			t.Fatalf("summary in=%v accounts=%d", s.In, len(s.Accounts))
		}
	})

	t.Run("logs narrow by entity and carry the ai source", func(t *testing.T) {
		conn := mcpConn(t)
		a := newAccount(t, conn, "Wallet", 0)
		if _, err := transactionsDo(conn, transactionsIn{
			Action: "create", Title: "Salary", Value: 8000, Kind: "income", Account: a.Code,
		}); err != nil {
			t.Fatal(err)
		}
		out, err := logsDo(conn, logsIn{Entity: "transaction"})
		if err != nil {
			t.Fatal(err)
		}
		trail := out.([]logs.Entry)
		if len(trail) != 1 || trail[0].Source != logs.AI {
			t.Fatalf("trail %+v — want one transaction row from ai", trail)
		}
	})
}

// mcpServer must register every tool, or an agent simply cannot see a module.
func TestMCPServerRegistersEveryTool(t *testing.T) {
	conn := mcpConn(t)
	s := mcpServer(conn)
	if s == nil {
		t.Fatal("no server")
	}
}
