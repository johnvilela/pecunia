// Command seed fills the database at $KAKEI_DB with sample accounts. It is a
// dev-only tool: scripts/dev.sh runs it against the database at the repo root.
package main

import (
	"fmt"
	"os"

	"kakei/internal/accounts"
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
	path, _ := db.Path()
	fmt.Printf("seeded %d of %d accounts into %s\n", n, len(fixtures), path)
}
