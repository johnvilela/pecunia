// Command seed fills the database at $KAKEI_DB with sample accounts and credit
// cards. It is a dev-only tool: scripts/dev.sh runs it against the database at
// the repo root.
package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"kakei/internal/accounts"
	"kakei/internal/bills"
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
// Days is how long ago it happened, counting back from the day the seeder runs.
// Days rather than a fixed date so the dev database is never stale, and never
// dated into the future either.
type txFixture struct {
	Title       string
	Description string
	Kind        string
	Value       int64
	Days        int
	Category    string // category code, empty for none
	Account     string // exactly one of these two
	Card        string
	Tags        []string
	// Installments spreads a card purchase over that many bills. 0 and 1 both
	// mean one ordinary charge.
	Installments int
}

func (f txFixture) date(now time.Time) string {
	return now.AddDate(0, 0, -f.Days).Format(transactions.DateLayout)
}

// txFixtures spread over five weeks, four accounts, two cards, both kinds and a
// handful of tags, so every filter and every branch of the table has something
// to work on. The first several are recent enough to always land in the current
// month, and the last several far enough back to always fall outside it.
//
// The card amounts stay well inside NUCRD's limit — it is the one card here that
// declines at it.
var txFixtures = []txFixture{
	{Title: "Padaria", Kind: transactions.KindOutcome,
		Value: 2450, Days: 0, Category: "FOOD1", Account: "CASH1", Tags: []string{"mercado"}},
	{Title: "Almoço no japonês", Kind: transactions.KindOutcome,
		Value: 11800, Days: 1, Category: "RESTA", Card: "NUCRD", Tags: []string{"lazer"}},
	// No category, so the table's blank-category branch has something to render.
	{Title: "Transferência recebida", Kind: transactions.KindIncome,
		Value: 30000, Days: 2, Account: "NUBON"},
	// On the USD card, so a listed amount is not always in reais.
	{Title: "Domain renewal", Description: "annual", Kind: transactions.KindOutcome,
		Value: 4800, Days: 3, Category: "WORK1", Card: "AMEX2", Tags: []string{"extra"}},
	{Title: "Ração do gato", Kind: transactions.KindOutcome,
		Value: 15900, Days: 5, Category: "PETS1", Account: "NUBON", Tags: []string{"casa"}},
	{Title: "Assinatura de streaming", Kind: transactions.KindOutcome,
		Value: 5590, Days: 8, Category: "ENTER", Card: "NUCRD", Tags: []string{"fixo"}},
	// An income on a card is a credit against the bill it falls in — a refund,
	// not a payment. Paying a bill is `kakei cc pay`, which files the money as an
	// outcome on the account it came from.
	{Title: "Estorno de compra", Kind: transactions.KindIncome,
		Value: 100000, Days: 11, Category: "HOBBY", Card: "NUCRD", Tags: []string{"extra"}},
	{Title: "Supermercado", Description: "compra da semana", Kind: transactions.KindOutcome,
		Value: 63200, Days: 14, Category: "FOOD1", Account: "NUBON", Tags: []string{"mercado"}},
	{Title: "Uber para o aeroporto", Kind: transactions.KindOutcome,
		Value: 7350, Days: 19, Category: "TRANS", Account: "CASH1"},
	{Title: "Conta de luz", Kind: transactions.KindOutcome,
		Value: 18450, Days: 24, Category: "UTILS", Account: "INTER", Tags: []string{"fixo", "casa"}},

	{Title: "Salário", Description: "pagamento mensal", Kind: transactions.KindIncome,
		Value: 850000, Days: 32, Category: "SLRY1", Account: "INTER", Tags: []string{"fixo"}},
	{Title: "Aluguel", Kind: transactions.KindOutcome,
		Value: 220000, Days: 33, Category: "HOME1", Account: "INTER", Tags: []string{"fixo", "casa"}},
	{Title: "Cinema", Kind: transactions.KindOutcome,
		Value: 9000, Days: 36, Category: "ENTER", Card: "NUCRD", Tags: []string{"lazer"}},
	{Title: "Freelance", Description: "landing page", Kind: transactions.KindIncome,
		Value: 120000, Days: 40, Category: "WORK1", Account: "PAYPL", Tags: []string{"extra"}},
	{Title: "Presente de aniversário", Kind: transactions.KindOutcome,
		Value: 24000, Days: 44, Category: "GIFTS", Account: "NUBON"},
	{Title: "Farmácia", Kind: transactions.KindOutcome,
		Value: 8790, Days: 47, Category: "HLTH1", Account: "INTER", Tags: []string{"casa"}},

	// A split purchase, so the dev database has a series to look at: five rows a
	// month apart, the first two already behind and the rest still to come. The
	// value is well inside NUCRD's limit, which the whole purchase takes at once.
	{Title: "Celular novo", Description: "5x sem juros", Kind: transactions.KindOutcome,
		Value: 100000, Days: 40, Category: "HOBBY", Card: "NUCRD", Tags: []string{"extra"},
		Installments: 5},
}

// txRows is how many transaction rows the fixtures come to, which is more than
// there are fixtures once one of them is a series.
func txRows() int {
	n := 0
	for _, f := range txFixtures {
		n += max(1, f.Installments)
	}
	return n
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
			Installment: transactions.Installment{Count: int64(f.Installments)},
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
		n += max(1, f.Installments)
	}
	return n, nil
}

// seedBillPayment leaves NUCRD's oldest unpaid bill partly settled, so the dev
// database has a bill in every state to look at: open, closed, partial and paid.
// Bills themselves need no fixture — asking for them is what creates them.
func seedBillPayment(conn *sql.DB) (int, error) {
	card, err := cards.NewStore(conn).ByCode("NUCRD")
	if err != nil {
		return 0, err
	}
	bill, err := bills.NewStore(conn).OldestUnpaid(card)
	if errors.Is(err, bills.ErrNotFound) {
		// Nothing owed, which is a fine state for the seeder to leave alone.
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	// Already settled by hand, or by a second run of the seeder.
	if bill.Paid > 0 {
		return 0, nil
	}

	inter, err := accounts.NewStore(conn).ByCode("INTER")
	if err != nil {
		return 0, err
	}
	// Two fifths of it, so the bill lands on "partial" rather than "paid".
	return 1, transactions.NewStore(conn).PayBill(
		bill.ID, inter.ID, bill.Remaining()*2/5, bill.DueOn)
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
	// Last of all: a bill only exists once something asks for it, and only a
	// transaction can pay one.
	paid, err := seedBillPayment(conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	path, _ := db.Path()
	fmt.Printf("seeded %d of %d accounts, %d of %d credit cards, %d of %d categories, %d of %d transactions and %d bill payment(s) into %s\n",
		n, len(fixtures), c, len(cardFixtures), ct, len(categories.Starter), tx, txRows(), paid, path)
}
