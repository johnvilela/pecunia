// Command seed fills the database at $KAKEI_DB with sample accounts and credit
// cards. It is a dev-only tool: scripts/dev.sh runs it against the database at
// the repo root.
package main

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"kakei/internal/accounts"
	"kakei/internal/cards"
	"kakei/internal/categories"
	"kakei/internal/db"
	"kakei/internal/transactions"
)

// fixtures are the sample accounts — a spread of currencies, one frozen, one
// overdrawn, one without a description, so every branch of the table and the
// details view has something to render.
var fixtures = []accounts.Account{
	{Code: "INTER", Name: "Banco Inter", Description: "conta corrente", Color: "orange", Currency: "BRL", Balance: 482350},
	{Code: "NUBON", Name: "Nubank", Description: "conta principal", Color: "violet", Currency: "BRL", Balance: 1275090},
	{Code: "CASH1", Name: "Carteira", Color: "green", Currency: "BRL", Balance: 15000},
	{Code: "CRED1", Name: "Cartão de crédito", Description: "fatura aberta", Color: "red", Currency: "BRL", Balance: -238745},
	{Code: "PAYPL", Name: "PayPal", Description: "recebimentos", Color: "blue", Currency: "USD", Balance: 92050},
	{Code: "EURSV", Name: "Poupança EUR", Color: "teal", Currency: "EUR", Balance: 500000},
	{Code: "BTC01", Name: "Cold wallet", Description: "hardware wallet", Color: "amber", Currency: "BTC", Balance: 42500000},
	{Code: "OLDAC", Name: "Conta antiga", Description: "encerrada em 2024", Color: "pink", Currency: "BRL", Balance: 0, IsFrozen: true},
}

// seed inserts the fixtures that are not there yet and reports how many it
// added. Skipping codes that already exist keeps it re-runnable and never
// clobbers an account you edited by hand while testing.
func seed(s *accounts.Store) (int, error) {
	n := 0
	for _, f := range fixtures {
		taken, err := s.CodeTaken(f.Code)
		if err != nil {
			return n, err
		}
		if taken {
			continue
		}
		if err := s.Create(&f); err != nil {
			return n, err
		}
		// Create ignores is_frozen, which only Update writes.
		if f.IsFrozen {
			if err := s.Update(f); err != nil {
				return n, err
			}
		}
		n++
	}
	return n, nil
}

// cardFixtures are the sample credit cards — one over its limit, one with
// nothing owed, one without a description, both settings of the over-limit
// allowance, a spread of currencies and days at both ends of the range, so
// every branch of the table, the usage bar and the details card has something
// to render.
var cardFixtures = []cards.Card{
	{Code: "NUCRD", Name: "Nubank Ultravioleta", Description: "cartão principal", Color: "violet",
		Currency: "BRL", Limit: 500000, Balance: 123850, ClosingDay: 15, DueDay: 22},
	// Over its limit, which only a card allowed over it can be.
	{Code: "ITAU1", Name: "Itaú Click", Description: "estourado", Color: "orange",
		Currency: "BRL", Limit: 300000, Balance: 412000, ClosingDay: 1, DueDay: 8,
		OverLimitAllowed: true},
	{Code: "CAIXA", Name: "Caixa Elo", Color: "blue",
		Currency: "BRL", Limit: 150000, Balance: 0, ClosingDay: 28, DueDay: 5},
	// Allowed over its limit but nowhere near it: the mark shows, the amount
	// stays uncolored.
	{Code: "AMEX2", Name: "Amex Green", Description: "compras internacionais", Color: "green",
		Currency: "USD", Limit: 800000, Balance: 215075, ClosingDay: 10, DueDay: 31,
		OverLimitAllowed: true},
	{Code: "WISE3", Name: "Wise", Description: "euros", Color: "teal",
		Currency: "EUR", Limit: 200000, Balance: 45000, ClosingDay: 20, DueDay: 30},
	{Code: "BTCRD", Name: "Crypto card", Description: "limite em bitcoin", Color: "amber",
		Currency: "BTC", Limit: 100000000, Balance: 12345678, ClosingDay: 31, DueDay: 1},
}

// seedCards is seed's twin for credit cards: skip what is already there, so it
// is re-runnable and never clobbers a card you edited by hand while testing.
func seedCards(s *cards.Store) (int, error) {
	n := 0
	for _, f := range cardFixtures {
		taken, err := s.CodeTaken(f.Code)
		if err != nil {
			return n, err
		}
		if taken {
			continue
		}
		if err := s.Create(&f); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// txFixture is one sample transaction, written against codes rather than ids
// because the rows it points at only get their ids when they are seeded.
//
// Month is 0 for the current month and -1 for the one before, so the dev
// database always has something in the default list and something outside it,
// whenever it is seeded.
type txFixture struct {
	Title       string
	Description string
	Kind        string
	Value       int64
	Month, Day  int
	Category    string // category code, empty for none
	Account     string // exactly one of these two
	Card        string
	Tags        []string
}

// date turns the fixture's month-and-day into a real one. A day the month is
// too short for lands on its last, and a day still ahead in the current month
// lands on today — dev data dated into the future reads as a bug.
func (f txFixture) date(now time.Time) string {
	m := time.Date(now.Year(), now.Month()+time.Month(f.Month), 1, 0, 0, 0, 0, time.UTC)
	// Day 0 of the next month is the last day of this one.
	day := min(f.Day, time.Date(m.Year(), m.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day())
	if f.Month == 0 {
		day = min(day, now.Day())
	}
	return time.Date(m.Year(), m.Month(), day, 0, 0, 0, 0, time.UTC).
		Format(transactions.DateLayout)
}

// txFixtures spread over two months, four accounts, two cards, both kinds and a
// handful of tags, so every filter and every branch of the table has something
// to work on. The card amounts stay well inside NUCRD's limit — it is the one
// card that declines at it.
var txFixtures = []txFixture{
	{Title: "Salário", Description: "pagamento mensal", Kind: transactions.KindIncome,
		Value: 850000, Month: -1, Day: 5, Category: "SLRY1", Account: "INTER", Tags: []string{"fixo"}},
	{Title: "Aluguel", Kind: transactions.KindOutcome,
		Value: 220000, Month: -1, Day: 6, Category: "HOME1", Account: "INTER", Tags: []string{"fixo", "casa"}},
	{Title: "Conta de luz", Kind: transactions.KindOutcome,
		Value: 18450, Month: -1, Day: 10, Category: "UTILS", Account: "INTER", Tags: []string{"fixo", "casa"}},
	{Title: "Supermercado", Description: "compra do mês", Kind: transactions.KindOutcome,
		Value: 63200, Month: -1, Day: 12, Category: "FOOD1", Account: "NUBON", Tags: []string{"mercado"}},
	{Title: "Cinema", Kind: transactions.KindOutcome,
		Value: 9000, Month: -1, Day: 18, Category: "ENTER", Card: "NUCRD", Tags: []string{"lazer"}},
	{Title: "Uber para o aeroporto", Kind: transactions.KindOutcome,
		Value: 7350, Month: -1, Day: 22, Category: "TRANS", Account: "CASH1"},
	{Title: "Freelance", Description: "landing page", Kind: transactions.KindIncome,
		Value: 120000, Month: -1, Day: 28, Category: "WORK1", Account: "PAYPL", Tags: []string{"extra"}},

	{Title: "Salário", Description: "pagamento mensal", Kind: transactions.KindIncome,
		Value: 850000, Month: 0, Day: 5, Category: "SLRY1", Account: "INTER", Tags: []string{"fixo"}},
	{Title: "Aluguel", Kind: transactions.KindOutcome,
		Value: 220000, Month: 0, Day: 6, Category: "HOME1", Account: "INTER", Tags: []string{"fixo", "casa"}},
	{Title: "Padaria", Kind: transactions.KindOutcome,
		Value: 2450, Month: 0, Day: 7, Category: "FOOD1", Account: "CASH1", Tags: []string{"mercado"}},
	{Title: "Almoço no japonês", Kind: transactions.KindOutcome,
		Value: 11800, Month: 0, Day: 9, Category: "RESTA", Card: "NUCRD", Tags: []string{"lazer"}},
	{Title: "Assinatura de streaming", Kind: transactions.KindOutcome,
		Value: 5590, Month: 0, Day: 10, Category: "ENTER", Card: "NUCRD", Tags: []string{"fixo"}},
	// No category, so the table's blank-category branch has something to render.
	{Title: "Transferência recebida", Kind: transactions.KindIncome,
		Value: 30000, Month: 0, Day: 11, Account: "NUBON"},
	{Title: "Ração do gato", Kind: transactions.KindOutcome,
		Value: 15900, Month: 0, Day: 14, Category: "PETS1", Account: "NUBON", Tags: []string{"casa"}},
	// On the USD card, so a listed amount is not always in reais.
	{Title: "Domain renewal", Description: "annual", Kind: transactions.KindOutcome,
		Value: 4800, Month: 0, Day: 15, Category: "WORK1", Card: "AMEX2", Tags: []string{"extra"}},
	// An income on a card is a payment against the invoice, which lowers it.
	{Title: "Pagamento da fatura", Kind: transactions.KindIncome,
		Value: 100000, Month: 0, Day: 20, Category: "DEBT1", Card: "NUCRD", Tags: []string{"fixo"}},
}

// seedTransactions files every fixture. Its idempotency is the whole table
// rather than a per-row check: a transaction has no code to skip on, and a dev
// database half-seeded is not a state worth supporting.
func seedTransactions(conn *sql.DB) (int, error) {
	s := transactions.NewStore(conn)
	existing, err := s.List(transactions.Filter{})
	if err != nil || len(existing) > 0 {
		return 0, err
	}

	accountStore, cardStore := accounts.NewStore(conn), cards.NewStore(conn)
	categoryStore := categories.NewStore(conn)
	now := time.Now()

	n := 0
	for _, f := range txFixtures {
		t := transactions.Transaction{
			Title: f.Title, Description: f.Description, Kind: f.Kind,
			Value: f.Value, Date: f.date(now), Tags: f.Tags,
		}
		if f.Category != "" {
			c, err := categoryStore.ByCode(f.Category)
			if err != nil {
				return n, fmt.Errorf("%s: category %s: %w", f.Title, f.Category, err)
			}
			t.Category = transactions.Ref{ID: c.ID}
		}
		if f.Account != "" {
			a, err := accountStore.ByCode(f.Account)
			if err != nil {
				return n, fmt.Errorf("%s: account %s: %w", f.Title, f.Account, err)
			}
			t.Account = transactions.Ref{ID: a.ID}
		} else {
			c, err := cardStore.ByCode(f.Card)
			if err != nil {
				return n, fmt.Errorf("%s: card %s: %w", f.Title, f.Card, err)
			}
			t.Card = transactions.Ref{ID: c.ID}
		}
		if err := s.Create(&t); err != nil {
			return n, fmt.Errorf("%s: %w", f.Title, err)
		}
		n++
	}
	return n, nil
}

func main() {
	conn, err := db.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	defer conn.Close()

	n, err := seed(accounts.NewStore(conn))
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	c, err := seedCards(cards.NewStore(conn))
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	// Categories have no dev fixtures of their own — the dev DB gets the same
	// starter set a real one does, because that is what needs looking at.
	ct, err := categories.Seed(categories.NewStore(conn))
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	// Last, because a transaction points at an account, a card and a category,
	// and moves the balance of whichever of the first two it names.
	tx, err := seedTransactions(conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	path, _ := db.Path()
	fmt.Printf("seeded %d of %d accounts, %d of %d credit cards, %d of %d categories and %d of %d transactions into %s\n",
		n, len(fixtures), c, len(cardFixtures), ct, len(categories.Starter), tx, len(txFixtures), path)
}
