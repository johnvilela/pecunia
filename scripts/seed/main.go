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
	"kakei/internal/budgets"
	"kakei/internal/cards"
	"kakei/internal/categories"
	"kakei/internal/db"
	"kakei/internal/goals"
	"kakei/internal/recurring"
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

// goalFixtures are the sample goals — both kinds, one past its target so the
// bar has its clamp to render, one in Bitcoin with nothing filed against it so
// the empty bar and the eight decimal places both show up, and one without a
// description.
var goalFixtures = []goals.Goal{
	{Name: "Notebook novo", Description: "trocar o notebook do trabalho",
		Target: 300000, Currency: "BRL", Kind: goals.KindSaving},
	{Name: "Quitar o Itaú", Description: "cartão estourado",
		Target: 412000, Currency: "BRL", Kind: goals.KindPaying},
	{Name: "Fone de ouvido", Target: 20000, Currency: "BRL", Kind: goals.KindSaving},
	{Name: "Um bitcoin inteiro", Description: "sem pressa",
		Target: 100000000, Currency: "BTC", Kind: goals.KindSaving},
}

// seedGoals inserts the fixtures that are not there yet. A goal has no code to
// skip on, so the name is what stands in for one here — it is what the
// transaction fixtures name them by too.
func seedGoals(conn *sql.DB) (int, error) {
	s := goals.NewStore(conn)
	existing, err := s.List()
	if err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	for _, g := range existing {
		seen[g.Name] = true
	}

	n := 0
	for _, f := range goalFixtures {
		if seen[f.Name] {
			continue
		}
		if err := s.Create(&f); err != nil {
			return n, fmt.Errorf("%s: %w", f.Name, err)
		}
		n++
	}
	return n, nil
}

// budgetFixture is one sample budget, written against a category code rather
// than an id because the category only gets one when it is seeded.
//
// Archived and Moved are what give the dev database something in every branch
// of the renderers: a budget nobody is tracking, and a cap with a history.
type budgetFixture struct {
	Budget   budgets.Budget
	Category string // category code
	Archived bool
	Moved    int64  // the cap it used to be, 0 when it has never moved
	Why      string // why it moved
}

// budgetFixtures cover a spread the summary and the table have to render: one
// comfortably inside its cap, one running ahead of the month, one that a real
// August blows straight through, one in another currency, and one put away.
var budgetFixtures = []budgetFixture{
	{
		Budget: budgets.Budget{Code: "FOODB", Name: "Comida", Description: "mercado e feira",
			Amount: 90000, Currency: "BRL", Color: "lime"},
		Category: "FOOD1",
		Moved:    80000,
		Why:      "arroz e carne subiram",
	},
	{
		Budget: budgets.Budget{Code: "RESTB", Name: "Restaurantes", Description: "comer fora e delivery",
			Amount: 40000, Currency: "BRL", Color: "orange"},
		Category: "RESTA",
	},
	{
		Budget: budgets.Budget{Code: "TRANB", Name: "Transporte", Description: "combustível e aplicativos",
			Amount: 35000, Currency: "BRL", Color: "teal"},
		Category: "TRANS",
	},
	{
		Budget: budgets.Budget{Code: "LSURB", Name: "Lazer", Description: "passeios e viagens",
			Amount: 60000, Currency: "BRL", Color: "violet"},
		Category: "LSURE",
	},
	{
		Budget: budgets.Budget{Code: "HOBBB", Name: "Hobbies em dólar", Description: "assinaturas gringas",
			Amount: 5000, Currency: "USD", Color: "yellow"},
		Category: "HOBBY",
	},
	{
		Budget: budgets.Budget{Code: "CAREB", Name: "Cuidados", Description: "cancelado, ficou no histórico",
			Amount: 25000, Currency: "BRL", Color: "pink"},
		Category: "CARE1",
		Archived: true,
	},
}

// seedBudgets inserts the fixtures that are not there yet, skipping on the code
// so it stays re-runnable, and then puts one of them away and moves another's
// cap — both so the dev database has one of each to look at.
func seedBudgets(conn *sql.DB) (int, error) {
	s := budgets.NewStore(conn)

	n := 0
	for _, f := range budgetFixtures {
		taken, err := s.CodeTaken(f.Budget.Code)
		if err != nil {
			return n, err
		}
		if taken {
			continue
		}
		cat, err := categoryID(conn, f.Category)
		if err != nil {
			return n, fmt.Errorf("%s: %w", f.Budget.Code, err)
		}

		b := f.Budget
		b.Category = transactions.Ref{ID: cat}
		// The cap it used to have goes in first, so the update below is what
		// writes the log entry — there is no other way to put one there, and
		// that is the point of the log.
		if f.Moved != 0 {
			b.Amount = f.Moved
		}
		if err := s.Create(&b); err != nil {
			return n, fmt.Errorf("%s: %w", b.Code, err)
		}
		if f.Moved != 0 {
			b.Amount = f.Budget.Amount
			if err := s.Update(b, f.Why); err != nil {
				return n, fmt.Errorf("%s: %w", b.Code, err)
			}
		}
		if f.Archived {
			if err := s.SetActive(b.ID, false); err != nil {
				return n, fmt.Errorf("%s: %w", b.Code, err)
			}
		}
		n++
	}
	return n, nil
}

// recurringFixture is one sample recurring bill, written against codes rather
// than ids because the account, card and category it points at only get theirs
// when they are seeded.
type recurringFixture struct {
	Bill     recurring.Bill
	Account  string // exactly one of these two
	Card     string
	Category string // category code, empty for none
	Archived bool
	// PaidCycles is how many months back to file a payment for, newest first —
	// 1 is last month only. The current month is deliberately left alone on most
	// of them, so the board has something to owe.
	PaidCycles int
	// PaidNow settles the month the seeder runs in as well, so the board has a
	// bill that is already done for this cycle.
	PaidNow bool
	// Vary is how much each seeded payment moves off the expected amount, in
	// minor units, so an energy bill's average is not its expected amount.
	Vary int64
}

// recurringFixtures cover every state the board renders: one overdue, one still
// upcoming, one settled, one on a credit card and one archived.
//
// The two days are picked around the 1st and the 28th rather than today, so the
// same fixtures land in different states depending on when the seeder is run —
// which is the point of looking at a dev database.
var recurringFixtures = []recurringFixture{
	{
		Bill: recurring.Bill{Code: "ENERG", Name: "Energia", Description: "Neoenergia",
			Color: "amber", Expected: 21490, OpenDay: 5, DueDay: 15, Tags: []string{"casa", "fixo"}},
		Account: "INTER", Category: "UTILS", PaidCycles: 3, Vary: 1830,
	},
	{
		Bill: recurring.Bill{Code: "ALUGL", Name: "Aluguel", Color: "red",
			Expected: 180000, OpenDay: 1, DueDay: 10, Tags: []string{"casa"}},
		Account: "NUBON", Category: "HOME1", PaidCycles: 2,
	},
	{
		// On the card, and cheap enough that it never troubles NUCRD's limit.
		Bill: recurring.Bill{Code: "NFLIX", Name: "Netflix", Description: "plano família",
			Color: "violet", Expected: 5590, OpenDay: 22, DueDay: 28, Tags: []string{"lazer"}},
		Card: "NUCRD", Category: "ENTER", PaidCycles: 4,
	},
	{
		// Already settled for the month the seeder runs in, so the board always
		// has a paid row — and, with the ones above it, every state at once.
		Bill: recurring.Bill{Code: "INTNT", Name: "Internet", Description: "Vivo Fibra",
			Color: "blue", Expected: 12990, OpenDay: 1, DueDay: 10, Tags: []string{"casa", "fixo"}},
		Account: "INTER", Category: "UTILS", PaidCycles: 5, PaidNow: true,
	},
	{
		// No expected amount: a bill nobody has seen a number for yet.
		Bill: recurring.Bill{Code: "WATER", Name: "Água", Color: "cyan",
			OpenDay: 12, DueDay: 20},
		Account: "INTER", Category: "UTILS",
	},
	{
		Bill: recurring.Bill{Code: "GYMXX", Name: "Academia", Description: "cancelada",
			Color: "lime", Expected: 12990, OpenDay: 5, DueDay: 10},
		Account: "INTER", Category: "CARE1", Archived: true, PaidCycles: 2,
	},
}

// seedRecurring inserts the bills that are not there yet, skipping on the code
// so a reseed never clobbers one edited by hand.
func seedRecurring(conn *sql.DB) (int, error) {
	s := recurring.NewStore(conn)
	n := 0
	for _, f := range recurringFixtures {
		taken, err := s.CodeTaken(f.Bill.Code)
		if err != nil {
			return n, err
		}
		if taken {
			continue
		}
		b := f.Bill
		if b.Account, b.Card, err = sourceRefs(conn, f.Account, f.Card); err != nil {
			return n, fmt.Errorf("%s: %w", b.Code, err)
		}
		if f.Category != "" {
			id, err := categoryID(conn, f.Category)
			if err != nil {
				return n, fmt.Errorf("%s: %w", b.Code, err)
			}
			b.Category = transactions.Ref{ID: id}
		}
		if err := s.Create(&b); err != nil {
			return n, fmt.Errorf("%s: %w", b.Code, err)
		}
		if f.Archived {
			if err := s.SetActive(b.ID, false); err != nil {
				return n, err
			}
		}
		n++
	}
	return n, nil
}

// sourceRefs resolves a fixture's account or card code into the ref a bill is
// written with. Exactly one of the two codes is set, which is what the schema
// asks for anyway.
func sourceRefs(conn *sql.DB, account, card string) (transactions.Ref, transactions.Ref, error) {
	if account != "" {
		a, err := accounts.NewStore(conn).ByCode(account)
		if err != nil {
			return transactions.Ref{}, transactions.Ref{}, fmt.Errorf("account %s: %w", account, err)
		}
		return transactions.Ref{ID: a.ID}, transactions.Ref{}, nil
	}
	c, err := cards.NewStore(conn).ByCode(card)
	if err != nil {
		return transactions.Ref{}, transactions.Ref{}, fmt.Errorf("card %s: %w", card, err)
	}
	return transactions.Ref{}, transactions.Ref{ID: c.ID}, nil
}

func categoryID(conn *sql.DB, code string) (int64, error) {
	c, err := categories.NewStore(conn).ByCode(code)
	if err != nil {
		return 0, fmt.Errorf("category %s: %w", code, err)
	}
	return c.ID, nil
}

// seedRecurringPayments files the past months each fixture asks for, so the dev
// database has averages to show and a board that is not all one colour.
//
// The payment is dated a couple of days after its cycle opens, and carries the
// cycle it was for — which is what the whole module turns on.
func seedRecurringPayments(conn *sql.DB) (int, error) {
	s := recurring.NewStore(conn)
	ts := transactions.NewStore(conn)
	now := time.Now()

	n := 0
	for _, f := range recurringFixtures {
		if f.PaidCycles == 0 {
			continue
		}
		b, err := s.ByCode(f.Bill.Code)
		if err != nil {
			return n, err
		}
		// From the current month when the fixture asks for it, from last month
		// otherwise.
		first := 1
		if f.PaidNow {
			first = 0
		}
		for i := first; i <= f.PaidCycles; i++ {
			cycle := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).
				AddDate(0, -i, 0)
			occ := b.Occurrence(cycle.Format(recurring.CycleLayout), now)
			if _, done := b.Payments[occ.Cycle]; done {
				continue
			}
			paidOn, err := time.Parse(recurring.DateLayout, occ.OpenOn)
			if err != nil {
				return n, err
			}
			// Two days after it opened, or today if that has not come round yet:
			// a bill can be paid the month it is still in, but no transaction in
			// the dev database is ever dated into the future.
			paidOn = paidOn.AddDate(0, 0, 2)
			if paidOn.After(now) {
				paidOn = now
			}
			// Alternating either side of the expected amount, so the average is a
			// number nothing else on the card already shows.
			value := b.Expected + f.Vary*int64(i%3-1)
			if value <= 0 {
				value = b.Expected
			}
			t := transactions.Transaction{
				Title: b.Name, Value: value, Kind: transactions.KindOutcome,
				Date:     paidOn.AddDate(0, 0, 2).Format(recurring.DateLayout),
				Category: b.Category, Account: b.Account, Card: b.Card,
				Tags: b.Tags, Recurring: transactions.Ref{ID: b.ID}, Cycle: occ.Cycle,
			}
			if err := ts.Create(&t); err != nil {
				return n, fmt.Errorf("%s %s: %w", b.Code, occ.Cycle, err)
			}
			n++
		}
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
	// Goal is the name of the goal this feeds, empty for none. The name rather
	// than a code, because a goal has neither.
	Goal string
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

	// Both dated today, and both against LSURE, so the Lazer budget is over its
	// R$600.00 cap in whatever month the seeder runs in — the one budget state
	// the fixtures above never reach, since every other cap sits comfortably
	// inside its month.
	//
	// Days 0 and not a few days back: a fixture dated the 2nd of a month falls
	// into the month before when the seeder runs on the 1st, and then the month
	// being looked at has nothing over in it after all.
	{Title: "Passagem para Salvador", Description: "feriado prolongado",
		Kind: transactions.KindOutcome, Value: 52000, Days: 0, Category: "LSURE",
		Account: "NUBON", Tags: []string{"lazer"}},
	{Title: "Pousada", Kind: transactions.KindOutcome,
		Value: 23500, Days: 0, Category: "LSURE", Card: "NUCRD", Tags: []string{"lazer"}},

	// Filed against goals, so the dev database has bars in three states to look
	// at: part way, past the target, and a goal with nothing against it at all.
	{Title: "Guardar para o notebook", Kind: transactions.KindIncome,
		Value: 120000, Days: 6, Category: "INVST", Account: "NUBON", Goal: "Notebook novo"},
	{Title: "Guardar para o notebook", Kind: transactions.KindIncome,
		Value: 90000, Days: 34, Category: "INVST", Account: "NUBON", Goal: "Notebook novo"},
	{Title: "Amortização do Itaú", Description: "adiantando a fatura", Kind: transactions.KindOutcome,
		Value: 150000, Days: 20, Category: "DEBT1", Account: "INTER", Goal: "Quitar o Itaú",
		Tags: []string{"fixo"}},
	// More than the goal asked for, which is what the clamped bar is there for.
	{Title: "Venda do fone antigo", Kind: transactions.KindIncome,
		Value: 25000, Days: 9, Category: "HOBBY", Account: "CASH1", Goal: "Fone de ouvido"},

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
		if f.Goal != "" {
			g, err := goalByName(conn, f.Goal)
			if err != nil {
				return n, fmt.Errorf("%s: goal %s: %w", f.Title, f.Goal, err)
			}
			t.Goal = transactions.Ref{ID: g.ID}
		}
		if err := s.Create(&t); err != nil {
			return n, fmt.Errorf("%s: %w", f.Title, err)
		}
		n += max(1, f.Installments)
	}
	return n, nil
}

// seedTargetChange cuts one goal's target, so the dev database has a target
// history to look at — the case the log exists for: a bill that settles for
// less than it said it would.
func seedTargetChange(conn *sql.DB) (int, error) {
	g, err := goalByName(conn, "Quitar o Itaú")
	if err != nil {
		// The fixture is not there, which a hand-edited dev database is allowed
		// to be.
		return 0, nil
	}
	log, err := goals.NewStore(conn).TargetLog(g.ID)
	if err != nil || len(log) > 0 {
		return 0, err
	}
	g.Target = 350000
	return 1, goals.NewStore(conn).Update(g, "consegui um desconto à vista")
}

// goalByName is the lookup the transaction fixtures need, since a goal has no
// code for them to name it by.
func goalByName(conn *sql.DB, name string) (goals.Goal, error) {
	all, err := goals.NewStore(conn).List()
	if err != nil {
		return goals.Goal{}, err
	}
	for _, g := range all {
		if g.Name == name {
			return g, nil
		}
	}
	return goals.Goal{}, errors.New("no such goal")
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
	// Before the transactions, because a transaction may name the goal it feeds.
	g, err := seedGoals(conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	// Last, because a transaction points at an account, a card, a category and
	// maybe a goal, and moves the balance of whichever of the first two it names.
	tx, err := seedTransactions(conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	// After the goals exist and before the tally, so the dev database shows a
	// target that has moved.
	moved, err := seedTargetChange(conn)
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
	// After the categories, which is all a budget points at.
	bg, err := seedBudgets(conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	// The recurring bills come after the accounts, cards and categories they
	// point at, and their payments after the bills themselves.
	rb, err := seedRecurring(conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	rp, err := seedRecurringPayments(conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	path, _ := db.Path()
	fmt.Printf("seeded %d of %d accounts, %d of %d credit cards, %d of %d categories, %d of %d goals, %d of %d transactions, %d target change(s), %d bill payment(s), %d of %d recurring bills, %d recurring payment(s) and %d of %d budgets into %s\n",
		n, len(fixtures), c, len(cardFixtures), ct, len(categories.Starter), g, len(goalFixtures), tx, txRows(), moved, paid, rb, len(recurringFixtures), rp, bg, len(budgetFixtures), path)
}
