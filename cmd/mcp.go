// pecunia mcp serves the modules to an AI agent over the Model Context
// Protocol, one tool per module, on stdio. Reads answer from the same stores
// the CLI uses; writes go through them too, so every rule — frozen accounts,
// card limits, currency freezes — holds, and every write lands in the audit
// trail as source "ai".
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"pecunia/internal/accounts"
	"pecunia/internal/bills"
	"pecunia/internal/budgets"
	"pecunia/internal/cards"
	"pecunia/internal/categories"
	"pecunia/internal/core"
	"pecunia/internal/goals"
	"pecunia/internal/logs"
	"pecunia/internal/recurring"
	"pecunia/internal/summary"
	"pecunia/internal/transactions"
)

const mcpHelp = `Serve pecunia to an AI agent over the Model Context Protocol.

Usage:
  pecunia mcp
  pecunia mcp install [AGENT]

Speaks MCP on stdin/stdout — point an MCP client (Claude Code, Claude
Desktop, …) at "pecunia mcp" and it gets one tool per module: accounts, credit
cards, categories, transactions, goals, recurring bills, budgets, summary and
logs. Reads and writes go through the same stores the CLI uses, and every
write is logged with source "ai" so "pecunia logs --source ai" shows exactly
what an agent did.

"install" registers this binary with an agent so nothing has to be
hand-edited. AGENT is one of ` + agentList + `; leaving it out opens a
picker.

Amounts everywhere are integers in minor units (cents; satoshis for BTC).
`

func runMCP(args []string) error {
	if len(args) > 0 && isHelpFlag(args[0]) {
		fmt.Fprint(out, mcpHelp)
		return nil
	}
	if len(args) > 0 && args[0] == "install" {
		return runMCPInstall(args[1:])
	}
	logs.Actor = logs.AI
	return withConn(func(conn *sql.DB) error {
		return mcpServer(conn).Run(context.Background(), &mcp.StdioTransport{})
	})
}

const moneyNote = "Amounts are integers in minor units (cents; satoshis for BTC)."

func mcpServer(conn *sql.DB) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "pecunia", Title: "pecunia — personal finance", Version: version}, nil)
	tool(s, conn, "pecunia_accounts",
		"Manage money accounts: list, get, create, update, delete, toggle_freeze. Changing the balance on update files a balance-adjustment transaction on the ledger. "+moneyNote,
		accountsDo)
	tool(s, conn, "pecunia_credit_cards",
		"Manage credit cards and their bills: list, get, create, update, delete, plus bills (the card's statements), bill_charges (one statement's lines) and pay_bill (settle a statement from an account). "+moneyNote,
		cardsDo)
	tool(s, conn, "pecunia_categories",
		"Manage the categories transactions are filed under: list, get, create, update, delete.",
		categoriesDo)
	tool(s, conn, "pecunia_transactions",
		"The ledger. list (with filters), get, create, update, delete, transfer. A transaction is income or outcome on exactly one account or credit card; a card purchase may be split into installments; paying a recurring bill is a transaction carrying recurring and cycle. "+moneyNote,
		transactionsDo)
	tool(s, conn, "pecunia_goals",
		"Manage savings/payoff goals: list, get, create, update, delete, target_log. Progress is summed from the transactions linked to the goal. "+moneyNote,
		goalsDo)
	tool(s, conn, "pecunia_recurring_bills",
		"Manage recurring bills (rent, energy, subscriptions): list, get, create, update, set_active, delete, payments. To pay one, create a transaction with recurring and cycle set. "+moneyNote,
		recurringDo)
	tool(s, conn, "pecunia_budgets",
		"Manage monthly caps per category: list, get, create, update, set_active, delete, history. "+moneyNote,
		budgetsDo)
	tool(s, conn, "pecunia_summary",
		"Where the user stands: totals in and out, what is due or coming up, account and card balances, goals and budgets — for one day or a whole month. "+moneyNote,
		summaryDo)
	tool(s, conn, "pecunia_logs",
		"The audit trail, newest first: who (user, system or ai) did what to which row, with field-level diffs on edits.",
		logsDo)
	return s
}

// tool registers one module handler, adapting it to the SDK's shape.
func tool[T any](s *mcp.Server, conn *sql.DB, name, desc string, f func(*sql.DB, T) (any, error)) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: desc},
		func(_ context.Context, _ *mcp.CallToolRequest, in T) (*mcp.CallToolResult, any, error) {
			out, err := f(conn, in)
			return nil, out, err
		})
}

// set overwrites dst only when the field was sent, which is what lets an
// update patch one field and leave the rest be.
func set[T any](dst *T, src *T) {
	if src != nil {
		*dst = *src
	}
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// mcpCurrency validates an input currency, defaulting to the first one.
// CurrencyByCode's silent fallback is right for rendering a hand-edited row,
// and wrong for accepting new data.
func mcpCurrency(p *string) (string, error) {
	if p == nil || *p == "" {
		return core.Currencies[0].Code, nil
	}
	code := strings.ToUpper(strings.TrimSpace(*p))
	var known []string
	for _, c := range core.Currencies {
		if c.Code == code {
			return code, nil
		}
		known = append(known, c.Code)
	}
	return "", fmt.Errorf("unknown currency %q — one of %s", *p, strings.Join(known, ", "))
}

func mcpColor(p *string) (string, error) {
	if p == nil || *p == "" {
		return core.Palette[0].Name, nil
	}
	name := strings.ToLower(strings.TrimSpace(*p))
	var known []string
	for _, c := range core.Palette {
		if c.Name == name {
			return name, nil
		}
		known = append(known, c.Name)
	}
	return "", fmt.Errorf("unknown color %q — one of %s", *p, strings.Join(known, ", "))
}

func dateOrToday(s string) (string, error) {
	if s == "" {
		return transactions.Today(), nil
	}
	return transactions.ParseDate(s)
}

type accountsIn struct {
	Action      string  `json:"action" jsonschema:"one of: list, get, create, update, delete, toggle_freeze"`
	Ref         string  `json:"ref,omitempty" jsonschema:"account id or code, for everything but list and create"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Color       *string `json:"color,omitempty" jsonschema:"palette name: red, orange, amber, yellow, lime, green, teal, cyan, blue, indigo, violet, pink"`
	Currency    *string `json:"currency,omitempty" jsonschema:"USD, EUR, BRL or BTC (default USD); frozen once transactions are filed"`
	Balance     *int64  `json:"balance,omitempty" jsonschema:"minor units. On update the change is filed as a balance-adjustment transaction, so the ledger keeps explaining the balance"`
	Note        *string `json:"note,omitempty" jsonschema:"why, when update changes the balance"`
}

func accountsDo(conn *sql.DB, in accountsIn) (any, error) {
	s := accounts.NewStore(conn)
	switch in.Action {
	case "list":
		return s.List()
	case "get":
		return s.Resolve(in.Ref)
	case "create":
		if str(in.Name) == "" {
			return nil, errors.New("create needs a name")
		}
		cur, err := mcpCurrency(in.Currency)
		if err != nil {
			return nil, err
		}
		col, err := mcpColor(in.Color)
		if err != nil {
			return nil, err
		}
		code, err := s.SuggestCode()
		if err != nil {
			return nil, err
		}
		a := accounts.Account{Code: code, Name: *in.Name, Description: str(in.Description), Color: col, Currency: cur}
		set(&a.Balance, in.Balance)
		if err := s.Create(&a); err != nil {
			return nil, err
		}
		return a, nil
	case "update":
		a, err := s.Resolve(in.Ref)
		if err != nil {
			return nil, err
		}
		old := a.Balance
		set(&a.Name, in.Name)
		set(&a.Description, in.Description)
		if in.Color != nil {
			if a.Color, err = mcpColor(in.Color); err != nil {
				return nil, err
			}
		}
		if in.Currency != nil {
			if a.Currency, err = mcpCurrency(in.Currency); err != nil {
				return nil, err
			}
		}
		set(&a.Balance, in.Balance)
		// A changed balance is filed as an adjustment before the rest of the
		// edit, exactly as `pecunia ac edit` files it, and for the same reason:
		// a frozen account's refusal aborts with nothing written.
		if delta := a.Balance - old; delta != 0 {
			adj := transactions.Transaction{
				Title: "Balance adjustment", Description: str(in.Note),
				Value: delta, Kind: transactions.KindAdjustment,
				Date:    transactions.Today(),
				Account: transactions.Ref{ID: a.ID}, Currency: a.Currency,
			}
			if err := transactions.NewStore(conn).Create(&adj); err != nil {
				return nil, err
			}
		}
		if err := s.Update(a); err != nil {
			return nil, err
		}
		return s.Get(a.ID)
	case "delete":
		a, err := s.Resolve(in.Ref)
		if err != nil {
			return nil, err
		}
		return map[string]string{"deleted": a.Code}, s.Delete(a.ID)
	case "toggle_freeze":
		a, err := s.Resolve(in.Ref)
		if err != nil {
			return nil, err
		}
		if _, err := s.ToggleFreeze(a.ID); err != nil {
			return nil, err
		}
		return s.Get(a.ID)
	}
	return nil, fmt.Errorf("unknown action %q — one of list, get, create, update, delete, toggle_freeze", in.Action)
}

type cardsIn struct {
	Action           string  `json:"action" jsonschema:"one of: list, get, create, update, delete, bills, bill_charges, pay_bill"`
	Ref              string  `json:"ref,omitempty" jsonschema:"card id or code, for everything but list and create (optional on bills: without it, every card's bills)"`
	Name             *string `json:"name,omitempty"`
	Description      *string `json:"description,omitempty"`
	Color            *string `json:"color,omitempty" jsonschema:"palette name, as on accounts"`
	Currency         *string `json:"currency,omitempty" jsonschema:"USD, EUR, BRL or BTC (default USD)"`
	Limit            *int64  `json:"limit,omitempty" jsonschema:"spending limit, minor units"`
	ClosingDay       *int64  `json:"closing_day,omitempty" jsonschema:"day of month the statement closes, 1-31"`
	DueDay           *int64  `json:"due_day,omitempty" jsonschema:"day of month the statement is due, 1-31"`
	OverLimitAllowed *bool   `json:"over_limit_allowed,omitempty" jsonschema:"whether the issuer lets spending pass the limit"`
	ClosesOn         string  `json:"closes_on,omitempty" jsonschema:"a bill's closing date YYYY-MM-DD from the bills action (bill_charges; pay_bill, default the oldest unpaid bill)"`
	Account          string  `json:"account,omitempty" jsonschema:"account id or code the payment comes from (pay_bill)"`
	Value            *int64  `json:"value,omitempty" jsonschema:"payment amount in minor units (pay_bill, default what the bill still owes)"`
	Date             string  `json:"date,omitempty" jsonschema:"payment date YYYY-MM-DD (pay_bill, default today)"`
}

func cardsDo(conn *sql.DB, in cardsIn) (any, error) {
	s := cards.NewStore(conn)
	bs := bills.NewStore(conn)
	switch in.Action {
	case "list":
		return s.List()
	case "get":
		return s.Resolve(in.Ref)
	case "create":
		if str(in.Name) == "" {
			return nil, errors.New("create needs a name")
		}
		cur, err := mcpCurrency(in.Currency)
		if err != nil {
			return nil, err
		}
		col, err := mcpColor(in.Color)
		if err != nil {
			return nil, err
		}
		code, err := s.SuggestCode()
		if err != nil {
			return nil, err
		}
		c := cards.Card{Code: code, Name: *in.Name, Description: str(in.Description), Color: col, Currency: cur}
		set(&c.Limit, in.Limit)
		set(&c.OverLimitAllowed, in.OverLimitAllowed)
		if in.ClosingDay != nil {
			c.ClosingDay = int(*in.ClosingDay)
		}
		if in.DueDay != nil {
			c.DueDay = int(*in.DueDay)
		}
		if err := s.Create(&c); err != nil {
			return nil, err
		}
		return c, nil
	case "update":
		c, err := s.Resolve(in.Ref)
		if err != nil {
			return nil, err
		}
		set(&c.Name, in.Name)
		set(&c.Description, in.Description)
		if in.Color != nil {
			if c.Color, err = mcpColor(in.Color); err != nil {
				return nil, err
			}
		}
		if in.Currency != nil {
			if c.Currency, err = mcpCurrency(in.Currency); err != nil {
				return nil, err
			}
		}
		set(&c.Limit, in.Limit)
		set(&c.OverLimitAllowed, in.OverLimitAllowed)
		if in.ClosingDay != nil {
			c.ClosingDay = int(*in.ClosingDay)
		}
		if in.DueDay != nil {
			c.DueDay = int(*in.DueDay)
		}
		if err := s.Update(c); err != nil {
			return nil, err
		}
		return s.Get(c.ID)
	case "delete":
		c, err := s.Resolve(in.Ref)
		if err != nil {
			return nil, err
		}
		return map[string]string{"deleted": c.Code}, s.Delete(c.ID)
	case "bills":
		if in.Ref == "" {
			all, err := s.List()
			if err != nil {
				return nil, err
			}
			var found []bills.Bill
			for _, c := range all {
				of, err := bs.List(c)
				if err != nil {
					return nil, err
				}
				found = append(found, of...)
			}
			return found, nil
		}
		c, err := s.Resolve(in.Ref)
		if err != nil {
			return nil, err
		}
		return bs.List(c)
	case "bill_charges":
		c, err := s.Resolve(in.Ref)
		if err != nil {
			return nil, err
		}
		b, err := bs.Get(c, in.ClosesOn)
		if err != nil {
			return nil, err
		}
		charges, err := bs.Charges(b)
		if err != nil {
			return nil, err
		}
		return map[string]any{"bill": b, "charges": charges}, nil
	case "pay_bill":
		c, err := s.Resolve(in.Ref)
		if err != nil {
			return nil, err
		}
		var b bills.Bill
		if in.ClosesOn == "" {
			b, err = bs.OldestUnpaid(c)
		} else {
			b, err = bs.Get(c, in.ClosesOn)
		}
		if err != nil {
			return nil, err
		}
		a, err := accounts.NewStore(conn).Resolve(in.Account)
		if err != nil {
			return nil, err
		}
		value := b.Total - b.Paid
		set(&value, in.Value)
		if value <= 0 {
			return nil, errors.New("nothing to pay")
		}
		date, err := dateOrToday(in.Date)
		if err != nil {
			return nil, err
		}
		if err := transactions.NewStore(conn).PayBill(b.ID, a.ID, value, date); err != nil {
			return nil, err
		}
		return bs.Get(c, b.ClosesOn)
	}
	return nil, fmt.Errorf("unknown action %q — one of list, get, create, update, delete, bills, bill_charges, pay_bill", in.Action)
}

type categoriesIn struct {
	Action      string  `json:"action" jsonschema:"one of: list, get, create, update, delete"`
	Ref         string  `json:"ref,omitempty" jsonschema:"category id or code, for everything but list and create"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Color       *string `json:"color,omitempty" jsonschema:"palette name, as on accounts"`
}

func categoriesDo(conn *sql.DB, in categoriesIn) (any, error) {
	s := categories.NewStore(conn)
	switch in.Action {
	case "list":
		return s.List()
	case "get":
		return s.Resolve(in.Ref)
	case "create":
		if str(in.Name) == "" {
			return nil, errors.New("create needs a name")
		}
		col, err := mcpColor(in.Color)
		if err != nil {
			return nil, err
		}
		code, err := s.SuggestCode()
		if err != nil {
			return nil, err
		}
		c := categories.Category{Code: code, Name: *in.Name, Description: str(in.Description), Color: col}
		if err := s.Create(&c, logs.Actor); err != nil {
			return nil, err
		}
		return c, nil
	case "update":
		c, err := s.Resolve(in.Ref)
		if err != nil {
			return nil, err
		}
		set(&c.Name, in.Name)
		set(&c.Description, in.Description)
		if in.Color != nil {
			if c.Color, err = mcpColor(in.Color); err != nil {
				return nil, err
			}
		}
		if err := s.Update(c); err != nil {
			return nil, err
		}
		return s.Get(c.ID)
	case "delete":
		c, err := s.Resolve(in.Ref)
		if err != nil {
			return nil, err
		}
		return map[string]string{"deleted": c.Code}, s.Delete(c.ID)
	}
	return nil, fmt.Errorf("unknown action %q — one of list, get, create, update, delete", in.Action)
}

type transactionsIn struct {
	Action string `json:"action" jsonschema:"one of: list, get, create, update, delete, transfer"`
	ID     int64  `json:"id,omitempty" jsonschema:"transaction id (get, update, delete)"`
	// Shared by the list filter and create.
	From      string `json:"from,omitempty" jsonschema:"list: date YYYY-MM-DD, inclusive"`
	To        string `json:"to,omitempty" jsonschema:"list: date YYYY-MM-DD, inclusive"`
	Tag       string `json:"tag,omitempty" jsonschema:"list: only transactions carrying this tag"`
	Search    string `json:"search,omitempty" jsonschema:"list: substring of the title or description"`
	Transfers bool   `json:"transfers,omitempty" jsonschema:"list: only the legs of transfers"`
	Category  string `json:"category,omitempty" jsonschema:"category id or code (list filter; create/update)"`
	Account   string `json:"account,omitempty" jsonschema:"account id or code (list filter; create: exactly one of account and card; transfer: where the money leaves)"`
	Card      string `json:"card,omitempty" jsonschema:"card id or code (list filter; create)"`
	GoalID    int64  `json:"goal_id,omitempty" jsonschema:"goal id this feeds (list filter; create). The goal's currency must match"`
	Recurring string `json:"recurring,omitempty" jsonschema:"recurring bill id or code this pays (list filter; create — requires cycle)"`

	Title        string   `json:"title,omitempty" jsonschema:"create (required), update"`
	Description  string   `json:"description,omitempty"`
	Value        int64    `json:"value,omitempty" jsonschema:"minor units, positive (create, update, transfer)"`
	Kind         string   `json:"kind,omitempty" jsonschema:"income or outcome (create)"`
	Date         string   `json:"date,omitempty" jsonschema:"YYYY-MM-DD, default today (create, transfer)"`
	Tags         []string `json:"tags,omitempty" jsonschema:"up to 5 (create, update)"`
	Cycle        string   `json:"cycle,omitempty" jsonschema:"YYYY-MM the payment is for, required with recurring"`
	Installments int64    `json:"installments,omitempty" jsonschema:"create on a card: split the purchase over this many monthly bills"`

	Scope string `json:"scope,omitempty" jsonschema:"how far an update or delete reaches through an installment series: one (default), forward, all"`

	ToAccount string `json:"to_account,omitempty" jsonschema:"transfer: account id or code the money reaches"`
	ToValue   int64  `json:"to_value,omitempty" jsonschema:"transfer: what arrives, in the destination's minor units — default value; differs on currency exchange or a fee"`
}

func mcpScope(s string) (transactions.Scope, error) {
	switch s {
	case "", "one":
		return transactions.ScopeOne, nil
	case "forward":
		return transactions.ScopeForward, nil
	case "all":
		return transactions.ScopeAll, nil
	}
	return 0, fmt.Errorf("unknown scope %q — one of one, forward, all", s)
}

func recurringResolve(s *recurring.Store, ref string) (recurring.Bill, error) {
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		return s.Get(id)
	}
	return s.ByCode(ref)
}

func transactionsDo(conn *sql.DB, in transactionsIn) (any, error) {
	s := transactions.NewStore(conn)
	// The three refs the filter and create share, resolved once.
	var category categories.Category
	var account accounts.Account
	var card cards.Card
	var recur recurring.Bill
	var err error
	if in.Category != "" {
		if category, err = categories.NewStore(conn).Resolve(in.Category); err != nil {
			return nil, err
		}
	}
	if in.Account != "" {
		if account, err = accounts.NewStore(conn).Resolve(in.Account); err != nil {
			return nil, err
		}
	}
	if in.Card != "" {
		if card, err = cards.NewStore(conn).Resolve(in.Card); err != nil {
			return nil, err
		}
	}
	if in.Recurring != "" {
		if recur, err = recurringResolve(recurring.NewStore(conn), in.Recurring); err != nil {
			return nil, err
		}
	}

	switch in.Action {
	case "list":
		return s.List(transactions.Filter{
			From: in.From, To: in.To, Tag: in.Tag, Search: in.Search,
			CategoryID: category.ID, AccountID: account.ID, CardID: card.ID,
			GoalID: in.GoalID, RecurringID: recur.ID, Transfers: in.Transfers,
		})
	case "get":
		return s.Get(in.ID)
	case "create":
		date, err := dateOrToday(in.Date)
		if err != nil {
			return nil, err
		}
		t := transactions.Transaction{
			Title: in.Title, Description: in.Description,
			Value: in.Value, Kind: in.Kind, Date: date, Tags: in.Tags,
			Category:  transactions.Ref{ID: category.ID},
			Account:   transactions.Ref{ID: account.ID},
			Card:      transactions.Ref{ID: card.ID},
			Goal:      transactions.Ref{ID: in.GoalID},
			Recurring: transactions.Ref{ID: recur.ID}, Cycle: in.Cycle,
			Installment: transactions.Installment{Count: in.Installments},
		}
		if err := s.Create(&t); err != nil {
			return nil, err
		}
		return s.Get(t.ID)
	case "update":
		t, err := s.Get(in.ID)
		if err != nil {
			return nil, err
		}
		scope, err := mcpScope(in.Scope)
		if err != nil {
			return nil, err
		}
		if in.Title != "" {
			t.Title = in.Title
		}
		if in.Description != "" {
			t.Description = in.Description
		}
		if in.Value != 0 {
			t.Value = in.Value
		}
		if in.Kind != "" {
			t.Kind = in.Kind
		}
		if in.Date != "" {
			if t.Date, err = transactions.ParseDate(in.Date); err != nil {
				return nil, err
			}
		}
		if len(in.Tags) > 0 {
			t.Tags = in.Tags
		}
		if in.Category != "" {
			t.Category = transactions.Ref{ID: category.ID}
		}
		if in.Cycle != "" {
			t.Cycle = in.Cycle
		}
		if err := s.Update(t, scope); err != nil {
			return nil, err
		}
		return s.Get(t.ID)
	case "delete":
		scope, err := mcpScope(in.Scope)
		if err != nil {
			return nil, err
		}
		return map[string]int64{"deleted": in.ID}, s.Delete(in.ID, scope)
	case "transfer":
		date, err := dateOrToday(in.Date)
		if err != nil {
			return nil, err
		}
		to, err := accounts.NewStore(conn).Resolve(in.ToAccount)
		if err != nil {
			return nil, err
		}
		toValue := in.Value
		if in.ToValue != 0 {
			toValue = in.ToValue
		}
		tr := transactions.Transfer{
			Title: in.Title, Description: in.Description, Date: date, Tags: in.Tags,
			From: transactions.Ref{ID: account.ID}, To: transactions.Ref{ID: to.ID},
			FromValue: in.Value, ToValue: toValue,
			Goal: transactions.Ref{ID: in.GoalID},
		}
		if err := s.Transfer(&tr); err != nil {
			return nil, err
		}
		return tr, nil
	}
	return nil, fmt.Errorf("unknown action %q — one of list, get, create, update, delete, transfer", in.Action)
}

type goalsIn struct {
	Action      string  `json:"action" jsonschema:"one of: list, get, create, update, delete, target_log"`
	ID          int64   `json:"id,omitempty" jsonschema:"goal id, for everything but list and create"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Target      *int64  `json:"target,omitempty" jsonschema:"minor units, positive"`
	Currency    *string `json:"currency,omitempty" jsonschema:"USD, EUR, BRL or BTC (default USD); only transactions in it may be linked"`
	Kind        *string `json:"kind,omitempty" jsonschema:"saving (progress climbs on income, default) or paying (climbs on outcome)"`
	Note        *string `json:"note,omitempty" jsonschema:"why, when update changes the target"`
}

func goalsDo(conn *sql.DB, in goalsIn) (any, error) {
	s := goals.NewStore(conn)
	switch in.Action {
	case "list":
		return s.List()
	case "get":
		return s.Get(in.ID)
	case "create":
		cur, err := mcpCurrency(in.Currency)
		if err != nil {
			return nil, err
		}
		g := goals.Goal{Name: str(in.Name), Description: str(in.Description), Currency: cur, Kind: goals.KindSaving}
		set(&g.Target, in.Target)
		set(&g.Kind, in.Kind)
		if err := s.Create(&g); err != nil {
			return nil, err
		}
		return g, nil
	case "update":
		g, err := s.Get(in.ID)
		if err != nil {
			return nil, err
		}
		set(&g.Name, in.Name)
		set(&g.Description, in.Description)
		set(&g.Target, in.Target)
		set(&g.Kind, in.Kind)
		if in.Currency != nil {
			if g.Currency, err = mcpCurrency(in.Currency); err != nil {
				return nil, err
			}
		}
		if err := s.Update(g, str(in.Note)); err != nil {
			return nil, err
		}
		return s.Get(g.ID)
	case "delete":
		return map[string]int64{"deleted": in.ID}, s.Delete(in.ID)
	case "target_log":
		return s.TargetLog(in.ID)
	}
	return nil, fmt.Errorf("unknown action %q — one of list, get, create, update, delete, target_log", in.Action)
}

type recurringIn struct {
	Action      string   `json:"action" jsonschema:"one of: list, get, create, update, set_active, delete, payments"`
	Ref         string   `json:"ref,omitempty" jsonschema:"recurring bill id or code, for everything but list and create"`
	Archived    bool     `json:"archived,omitempty" jsonschema:"list: show the archived ones instead"`
	Name        *string  `json:"name,omitempty"`
	Description *string  `json:"description,omitempty"`
	Color       *string  `json:"color,omitempty" jsonschema:"palette name, as on accounts"`
	Expected    *int64   `json:"expected,omitempty" jsonschema:"what it usually costs, minor units; 0 = unknown"`
	OpenDay     *int64   `json:"open_day,omitempty" jsonschema:"day of month it can be paid from"`
	DueDay      *int64   `json:"due_day,omitempty" jsonschema:"day of month it is late after"`
	Tags        []string `json:"tags,omitempty"`
	Category    *string  `json:"category,omitempty" jsonschema:"category id or code"`
	Account     *string  `json:"account,omitempty" jsonschema:"account id or code it is paid from (exactly one of account and card)"`
	Card        *string  `json:"card,omitempty" jsonschema:"card id or code it is paid from"`
	Active      *bool    `json:"active,omitempty" jsonschema:"set_active: the state to set — omitted flips it"`
}

func recurringDo(conn *sql.DB, in recurringIn) (any, error) {
	s := recurring.NewStore(conn)
	sourceRefs := func(b *recurring.Bill) error {
		if in.Category != nil {
			c, err := categories.NewStore(conn).Resolve(*in.Category)
			if err != nil {
				return err
			}
			b.Category = transactions.Ref{ID: c.ID}
		}
		if in.Account != nil {
			a, err := accounts.NewStore(conn).Resolve(*in.Account)
			if err != nil {
				return err
			}
			b.Account = transactions.Ref{ID: a.ID}
		}
		if in.Card != nil {
			c, err := cards.NewStore(conn).Resolve(*in.Card)
			if err != nil {
				return err
			}
			b.Card = transactions.Ref{ID: c.ID}
		}
		return nil
	}
	switch in.Action {
	case "list":
		return s.List(in.Archived)
	case "get":
		return recurringResolve(s, in.Ref)
	case "create":
		col, err := mcpColor(in.Color)
		if err != nil {
			return nil, err
		}
		code, err := core.SuggestCode(s.CodeTaken)
		if err != nil {
			return nil, err
		}
		b := recurring.Bill{Code: code, Name: str(in.Name), Description: str(in.Description), Color: col, Tags: in.Tags}
		set(&b.Expected, in.Expected)
		if in.OpenDay != nil {
			b.OpenDay = int(*in.OpenDay)
		}
		if in.DueDay != nil {
			b.DueDay = int(*in.DueDay)
		}
		if err := sourceRefs(&b); err != nil {
			return nil, err
		}
		if err := s.Create(&b); err != nil {
			return nil, err
		}
		return s.Get(b.ID)
	case "update":
		b, err := recurringResolve(s, in.Ref)
		if err != nil {
			return nil, err
		}
		set(&b.Name, in.Name)
		set(&b.Description, in.Description)
		if in.Color != nil {
			if b.Color, err = mcpColor(in.Color); err != nil {
				return nil, err
			}
		}
		set(&b.Expected, in.Expected)
		if in.OpenDay != nil {
			b.OpenDay = int(*in.OpenDay)
		}
		if in.DueDay != nil {
			b.DueDay = int(*in.DueDay)
		}
		if len(in.Tags) > 0 {
			b.Tags = in.Tags
		}
		if err := sourceRefs(&b); err != nil {
			return nil, err
		}
		if err := s.Update(b); err != nil {
			return nil, err
		}
		return s.Get(b.ID)
	case "set_active":
		b, err := recurringResolve(s, in.Ref)
		if err != nil {
			return nil, err
		}
		active := !b.Active
		set(&active, in.Active)
		if err := s.SetActive(b.ID, active); err != nil {
			return nil, err
		}
		return s.Get(b.ID)
	case "delete":
		b, err := recurringResolve(s, in.Ref)
		if err != nil {
			return nil, err
		}
		return map[string]string{"deleted": b.Code}, s.Delete(b.ID)
	case "payments":
		b, err := recurringResolve(s, in.Ref)
		if err != nil {
			return nil, err
		}
		return s.Payments(b.ID)
	}
	return nil, fmt.Errorf("unknown action %q — one of list, get, create, update, set_active, delete, payments", in.Action)
}

type budgetsIn struct {
	Action      string  `json:"action" jsonschema:"one of: list, get, create, update, set_active, delete, history"`
	Ref         string  `json:"ref,omitempty" jsonschema:"budget id or code, for everything but list and create"`
	Cycle       string  `json:"cycle,omitempty" jsonschema:"YYYY-MM the spend is read for (list, get; default the current month)"`
	Archived    bool    `json:"archived,omitempty" jsonschema:"list: show the archived ones instead"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Color       *string `json:"color,omitempty" jsonschema:"palette name, as on accounts"`
	Amount      *int64  `json:"amount,omitempty" jsonschema:"the monthly cap, minor units"`
	Currency    *string `json:"currency,omitempty" jsonschema:"USD, EUR, BRL or BTC (default USD); only transactions in it are counted"`
	Category    *string `json:"category,omitempty" jsonschema:"category id or code the cap is on"`
	Note        *string `json:"note,omitempty" jsonschema:"why, when update changes the amount"`
	Active      *bool   `json:"active,omitempty" jsonschema:"set_active: the state to set — omitted flips it"`
	Months      int     `json:"months,omitempty" jsonschema:"history: how many months back, default 6"`
}

func budgetsDo(conn *sql.DB, in budgetsIn) (any, error) {
	s := budgets.NewStore(conn)
	cycle := in.Cycle
	if cycle == "" {
		cycle = time.Now().Format(transactions.CycleLayout)
	}
	switch in.Action {
	case "list":
		return s.List(cycle, in.Archived)
	case "get":
		return s.Resolve(in.Ref, cycle)
	case "create":
		if in.Category == nil {
			return nil, errors.New("create needs the category the cap is on")
		}
		c, err := categories.NewStore(conn).Resolve(*in.Category)
		if err != nil {
			return nil, err
		}
		cur, err := mcpCurrency(in.Currency)
		if err != nil {
			return nil, err
		}
		col, err := mcpColor(in.Color)
		if err != nil {
			return nil, err
		}
		code, err := s.SuggestCode()
		if err != nil {
			return nil, err
		}
		b := budgets.Budget{Code: code, Name: str(in.Name), Description: str(in.Description), Color: col,
			Currency: cur, Category: transactions.Ref{ID: c.ID}}
		set(&b.Amount, in.Amount)
		if err := s.Create(&b); err != nil {
			return nil, err
		}
		return s.Get(b.ID, cycle)
	case "update":
		b, err := s.Resolve(in.Ref, cycle)
		if err != nil {
			return nil, err
		}
		set(&b.Name, in.Name)
		set(&b.Description, in.Description)
		if in.Color != nil {
			if b.Color, err = mcpColor(in.Color); err != nil {
				return nil, err
			}
		}
		if in.Currency != nil {
			if b.Currency, err = mcpCurrency(in.Currency); err != nil {
				return nil, err
			}
		}
		set(&b.Amount, in.Amount)
		if in.Category != nil {
			c, err := categories.NewStore(conn).Resolve(*in.Category)
			if err != nil {
				return nil, err
			}
			b.Category = transactions.Ref{ID: c.ID}
		}
		if err := s.Update(b, str(in.Note)); err != nil {
			return nil, err
		}
		return s.Get(b.ID, cycle)
	case "set_active":
		b, err := s.Resolve(in.Ref, cycle)
		if err != nil {
			return nil, err
		}
		active := !b.Active
		set(&active, in.Active)
		if err := s.SetActive(b.ID, active); err != nil {
			return nil, err
		}
		return s.Get(b.ID, cycle)
	case "delete":
		b, err := s.Resolve(in.Ref, cycle)
		if err != nil {
			return nil, err
		}
		return map[string]string{"deleted": b.Code}, s.Delete(b.ID)
	case "history":
		b, err := s.Resolve(in.Ref, cycle)
		if err != nil {
			return nil, err
		}
		months := in.Months
		if months == 0 {
			months = 6
		}
		return s.History(b, months)
	}
	return nil, fmt.Errorf("unknown action %q — one of list, get, create, update, set_active, delete, history", in.Action)
}

type summaryIn struct {
	Date  string `json:"date,omitempty" jsonschema:"the day to summarise, YYYY-MM-DD, default today"`
	Month bool   `json:"month,omitempty" jsonschema:"widen to the whole month that day falls in"`
}

func summaryDo(conn *sql.DB, in summaryIn) (any, error) {
	day := transactions.Today()
	if in.Date != "" {
		parsed, err := transactions.ParseDate(in.Date)
		if err != nil {
			return nil, err
		}
		day = parsed
	}
	period := summary.Period{From: day, To: day}
	if in.Month {
		start, end, err := monthRange(transactions.CycleOf(day))
		if err != nil {
			return nil, err
		}
		period = summary.Period{From: start, To: end}
	}
	return summary.Collect(conn, period, time.Now())
}

type logsIn struct {
	Entity   string `json:"entity,omitempty" jsonschema:"account, card, category, transaction, transfer, goal, recurring, budget, card_bill"`
	EntityID int64  `json:"entity_id,omitempty"`
	Action   string `json:"log_action,omitempty" jsonschema:"created, edited or deleted"`
	Source   string `json:"source,omitempty" jsonschema:"user, system or ai"`
	From     string `json:"from,omitempty" jsonschema:"date YYYY-MM-DD, inclusive"`
	To       string `json:"to,omitempty" jsonschema:"date YYYY-MM-DD, inclusive"`
	Limit    int    `json:"limit,omitempty" jsonschema:"default 10"`
}

func logsDo(conn *sql.DB, in logsIn) (any, error) {
	return logs.List(conn, logs.Filter{
		Entity: in.Entity, EntityID: in.EntityID, Action: in.Action,
		Source: in.Source, From: in.From, To: in.To, Limit: in.Limit,
	})
}
