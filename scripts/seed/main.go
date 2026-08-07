// Command seed fills the database at $KAKEI_DB with sample accounts and credit
// cards. It is a dev-only tool: scripts/dev.sh runs it against the database at
// the repo root.
package main

import (
	"fmt"
	"os"

	"kakei/internal/accounts"
	"kakei/internal/cards"
	"kakei/internal/db"
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
// nothing owed, one without a description, a spread of currencies and days at
// both ends of the range, so every branch of the table, the usage bar and the
// details card has something to render.
var cardFixtures = []cards.Card{
	{Code: "NUCRD", Name: "Nubank Ultravioleta", Description: "cartão principal", Color: "violet",
		Currency: "BRL", Limit: 500000, Balance: 123850, ClosingDay: 15, DueDay: 22},
	{Code: "ITAU1", Name: "Itaú Click", Description: "estourado", Color: "orange",
		Currency: "BRL", Limit: 300000, Balance: 412000, ClosingDay: 1, DueDay: 8},
	{Code: "CAIXA", Name: "Caixa Elo", Color: "blue",
		Currency: "BRL", Limit: 150000, Balance: 0, ClosingDay: 28, DueDay: 5},
	{Code: "AMEX2", Name: "Amex Green", Description: "compras internacionais", Color: "green",
		Currency: "USD", Limit: 800000, Balance: 215075, ClosingDay: 10, DueDay: 31},
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
	path, _ := db.Path()
	fmt.Printf("seeded %d of %d accounts and %d of %d credit cards into %s\n",
		n, len(fixtures), c, len(cardFixtures), path)
}
