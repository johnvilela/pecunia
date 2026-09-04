package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"pecunia/internal/accounts"
	"pecunia/internal/bills"
	"pecunia/internal/budgets"
	"pecunia/internal/cards"
	"pecunia/internal/categories"
	"pecunia/internal/core"
	"pecunia/internal/goals"
	"pecunia/internal/recurring"
	"pecunia/internal/summary"
	"pecunia/internal/transactions"
)

// This file is pecunia's Omni face (github.com/johnvilela/omni): omni-manifest
// and omni-skills are the plugin contract Omni calls at install time, and
// `pecunia omni <sub>` is what its Telegram commands run. Telegram shows
// stdout as plain proportional text, so nothing here may emit ANSI, tables or
// alignment — every renderer is lines.

// omniManifest is what `pecunia omni-manifest` prints: how Omni wires pecunia
// in — the MCP server, the skills and the Telegram commands.
type omniManifest struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	MCP         omniMCP   `json:"mcp"`
	Skills      bool      `json:"skills"`
	Commands    []omniCmd `json:"commands"`
}

type omniMCP struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type omniCmd struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Argv        []string `json:"argv"`
}

func manifest() omniManifest {
	return omniManifest{
		Name:        "pecunia",
		Version:     version,
		Description: "Personal finance, on your machine: accounts, credit cards, bills, budgets and goals in a local SQLite ledger.",
		MCP:         omniMCP{Command: "pecunia", Args: []string{"mcp"}},
		Skills:      true,
		Commands: []omniCmd{
			{"pecunia_resume", "Where you stand: balances, money in and out, and any alerts. Add a period: today, yesterday, week, last week, month, last month, YYYY-MM or YYYY-MM-DD.", []string{"pecunia", "omni", "resume"}},
			{"pecunia_goals", "Goals and how far along each one is.", []string{"pecunia", "omni", "goals"}},
			{"pecunia_bills", "Recurring bills and where this cycle stands: upcoming, open, overdue or paid.", []string{"pecunia", "omni", "bills"}},
			{"pecunia_cc", "Credit cards: limit, balance, available and the current statement.", []string{"pecunia", "omni", "cc"}},
			{"pecunia_alerts", "Only problems: overdue bills, budgets over cap, cards near their limit. Prints nothing when all is well.", []string{"pecunia", "omni", "alerts"}},
			{"pecunia_budget", "This month's budgets against what was actually spent.", []string{"pecunia", "omni", "budget"}},
			{"pecunia_add", "Quick expense: amount then title, e.g. 12.50 lunch. @CODE picks the account, #CODE the category.", []string{"pecunia", "omni", "add"}},
		},
	}
}

func runOmniManifest() error {
	data, err := json.MarshalIndent(manifest(), "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(out, string(data))
	return nil
}

// runOmniSkills writes every embedded skill into the directory Omni hands
// over, one <name>/SKILL.md each. Unlike setup --skills it ships pecunia-omni
// too: the Telegram commands that skill teaches only exist under Omni.
func runOmniSkills(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: pecunia omni-skills <dir>")
	}
	entries, err := skillFS.ReadDir("skills")
	if err != nil {
		return err
	}
	for _, e := range entries {
		data, err := skillFS.ReadFile("skills/" + e.Name())
		if err != nil {
			return err
		}
		dir := filepath.Join(args[0], strings.TrimSuffix(e.Name(), ".md"))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func runOmni(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: pecunia omni resume|goals|bills|cc|alerts|budget|add")
	}
	// Telegram appends whatever the user typed after the command; only resume
	// and add have a use for it, the rest ignore it.
	switch args[0] {
	case "resume":
		return runOmniResume(args[1:])
	case "goals":
		return runOmniGoals()
	case "bills":
		return runOmniBills()
	case "cc":
		return runOmniCC()
	case "alerts":
		return runOmniAlerts()
	case "budget":
		return runOmniBudget()
	case "add":
		return runOmniAdd(args[1:])
	default:
		return fmt.Errorf("unknown omni subcommand %q — resume, goals, bills, cc, alerts, budget or add", args[0])
	}
}

// money is one amount with its currency symbol, sign first: "-R$360.00".
func money(v int64, cur core.Currency) string {
	f := core.FormatAmount(v, cur)
	if strings.HasPrefix(f, "-") {
		return "-" + cur.Symbol + f[1:]
	}
	return cur.Symbol + f
}

// moneyByCur is every non-zero currency on one line, sorted — the plain cousin
// of core.MoneyLine, which styles its separator with ANSI.
func moneyByCur(by map[string]int64) string {
	var codes []string
	for c, v := range by {
		if v != 0 {
			codes = append(codes, c)
		}
	}
	slices.Sort(codes)
	parts := make([]string, len(codes))
	for i, c := range codes {
		parts[i] = money(by[c], core.CurrencyByCode(c))
	}
	return strings.Join(parts, " · ")
}

func days(n int) string {
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}

// parsePeriod maps a Telegram phrase onto a summary window. Nothing means the
// current month. Weeks run Monday through Sunday.
func parsePeriod(args []string, now time.Time) (summary.Period, error) {
	day := func(t time.Time) string { return t.Format(transactions.DateLayout) }
	monday := now.AddDate(0, 0, -((int(now.Weekday()) + 6) % 7))

	phrase := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	switch phrase {
	case "", "month", "this month":
		return monthPeriod(transactions.CycleOf(day(now)))
	case "today":
		return summary.Period{From: day(now), To: day(now)}, nil
	case "yesterday":
		y := day(now.AddDate(0, 0, -1))
		return summary.Period{From: y, To: y}, nil
	case "week", "this week":
		return summary.Period{From: day(monday), To: day(monday.AddDate(0, 0, 6))}, nil
	case "last week":
		monday = monday.AddDate(0, 0, -7)
		return summary.Period{From: day(monday), To: day(monday.AddDate(0, 0, 6))}, nil
	case "last month":
		first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return monthPeriod(first.AddDate(0, -1, 0).Format(transactions.CycleLayout))
	}
	if cycle, err := transactions.ParseCycle(phrase); err == nil {
		return monthPeriod(cycle)
	}
	if d, err := transactions.ParseDate(phrase); err == nil {
		return summary.Period{From: d, To: d}, nil
	}
	return summary.Period{}, fmt.Errorf("cannot read %q as a period — try today, yesterday, week, last week, month, last month, YYYY-MM or YYYY-MM-DD", phrase)
}

func monthPeriod(cycle string) (summary.Period, error) {
	from, to, err := monthRange(cycle)
	if err != nil {
		return summary.Period{}, err
	}
	return summary.Period{From: from, To: to}, nil
}

// periodLabel is the window's name. Collect's own Title only knows days and
// months, so a week window is spelled out as its two ends.
func periodLabel(s summary.Summary) string {
	p := s.Period
	if p.Day() {
		return s.Title
	}
	if from, to, err := monthRange(transactions.CycleOf(p.From)); err == nil && from == p.From && to == p.To {
		return s.Title
	}
	return p.From + " to " + p.To
}

func runOmniResume(args []string) error {
	period, err := parsePeriod(args, time.Now())
	if err != nil {
		return err
	}
	return withConn(func(conn *sql.DB) error {
		s, err := summary.Collect(conn, period, time.Now())
		if err != nil {
			return err
		}
		alerts, err := collectAlerts(conn)
		if err != nil {
			return err
		}
		fmt.Fprint(out, plainSummary(s, alerts))
		return nil
	})
}

func plainSummary(s summary.Summary, alerts []string) string {
	sections := []string{"📊 " + periodLabel(s)}

	var flow []string
	if in := moneyByCur(s.In); in != "" {
		flow = append(flow, "In: "+in)
	}
	if outgo := moneyByCur(s.Out); outgo != "" {
		flow = append(flow, "Out: "+outgo)
	}
	if len(flow) == 0 {
		flow = []string{"Nothing moved in this window."}
	}
	sections = append(sections, strings.Join(flow, "\n"))

	if len(s.Accounts) > 0 {
		lines := []string{"Accounts:"}
		for _, a := range s.Accounts {
			lines = append(lines, fmt.Sprintf("• %s (%s): %s", a.Name, a.Code, money(a.Balance, a.Cur())))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	if len(s.Cards) > 0 {
		lines := []string{"Cards:"}
		for _, c := range s.Cards {
			lines = append(lines, fmt.Sprintf("• %s (%s): %s owed · %s available",
				c.Name, c.Code, money(c.Balance, c.Cur()), money(c.Available(), c.Cur())))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	if len(alerts) > 0 {
		sections = append(sections, "Alerts:\n"+strings.Join(alerts, "\n"))
	}
	return strings.Join(sections, "\n\n") + "\n"
}

// collectAlerts reads what the alerts need and judges it against now — even on
// a resume of last month, what is overdue is a fact about today.
func collectAlerts(conn *sql.DB) ([]string, error) {
	today := time.Now()
	rbs, err := recurring.NewStore(conn).List(false)
	if err != nil {
		return nil, err
	}
	buds, err := budgets.NewStore(conn).List(transactions.CycleOf(transactions.Today()), false)
	if err != nil {
		return nil, err
	}
	cs, err := cards.NewStore(conn).List()
	if err != nil {
		return nil, err
	}
	return alertLines(rbs, buds, cs, today), nil
}

// alertLines is every problem worth a ping: overdue recurring bills, budgets
// past their cap, cards at or near their limit. Empty when all is well, which
// is what lets `omni alerts` stay silent.
func alertLines(rbs []recurring.Bill, buds []budgets.Budget, cs []cards.Card, today time.Time) []string {
	var lines []string
	for _, b := range rbs {
		occ := b.Current(today)
		if occ.Status != recurring.StatusOverdue {
			continue
		}
		name := b.Name
		if b.Expected > 0 {
			name += " (" + money(b.Expected, b.Cur()) + ")"
		}
		lines = append(lines, fmt.Sprintf("⚠️ %s is %s overdue — was due %s", name, days(occ.Late), occ.DueOn))
	}
	for _, b := range buds {
		if b.Status(today) != budgets.StatusOver {
			continue
		}
		lines = append(lines, fmt.Sprintf("⚠️ %s budget is over its cap: %s of %s (%d%%)",
			b.Name, money(b.Spent, b.Cur()), money(b.Amount, b.Cur()), b.Pct()))
	}
	for _, c := range cs {
		// ponytail: fixed 90% threshold; make it a flag if anyone ever asks.
		if c.Limit <= 0 || c.Balance*10 < c.Limit*9 {
			continue
		}
		lines = append(lines, fmt.Sprintf("⚠️ %s is at %d%% of its limit: %s of %s",
			c.Name, c.Balance*100/c.Limit, money(c.Balance, c.Cur()), money(c.Limit, c.Cur())))
	}
	return lines
}

func runOmniAlerts() error {
	return withConn(func(conn *sql.DB) error {
		lines, err := collectAlerts(conn)
		if err != nil {
			return err
		}
		// Silence is the contract: an Omni scheduled task running this only
		// pings when something is wrong.
		if len(lines) > 0 {
			fmt.Fprintln(out, strings.Join(lines, "\n"))
		}
		return nil
	})
}

func runOmniGoals() error {
	return withConn(func(conn *sql.DB) error {
		gs, err := goals.NewStore(conn).List()
		if err != nil {
			return err
		}
		fmt.Fprint(out, plainGoals(gs))
		return nil
	})
}

func plainGoals(gs []goals.Goal) string {
	if len(gs) == 0 {
		return "No goals yet — create one with: pecunia goals new\n"
	}
	lines := []string{"🎯 Goals"}
	for _, g := range gs {
		var pct int64
		if g.Target > 0 {
			pct = g.Progress() * 100 / g.Target
		}
		line := fmt.Sprintf("• %s: %s %s of %s (%d%%)",
			g.Name, money(g.Progress(), g.Cur()), g.Verb(), money(g.Target, g.Cur()), pct)
		if g.Reached() {
			line += " ✅"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n") + "\n"
}

func runOmniBills() error {
	return withConn(func(conn *sql.DB) error {
		bs, err := recurring.NewStore(conn).List(false)
		if err != nil {
			return err
		}
		fmt.Fprint(out, plainBills(bs, time.Now()))
		return nil
	})
}

func plainBills(bs []recurring.Bill, today time.Time) string {
	if len(bs) == 0 {
		return "No recurring bills yet — create one with: pecunia bill new\n"
	}
	lines := []string{"🧾 Bills"}
	for _, b := range bs {
		occ := b.Current(today)
		name := b.Name
		if b.Expected > 0 {
			name += " (" + money(b.Expected, b.Cur()) + ")"
		}
		var state string
		switch occ.Status {
		case recurring.StatusPaid:
			state = "✅ paid " + money(occ.Paid, b.Cur())
		case recurring.StatusOverdue:
			state = fmt.Sprintf("⚠️ %s overdue — was due %s", days(occ.Late), occ.DueOn)
		case recurring.StatusOpen:
			state = "open, due " + occ.DueOn
		default:
			state = "upcoming, opens " + occ.OpenOn
		}
		lines = append(lines, fmt.Sprintf("• %s [%s]: %s", name, occ.Cycle, state))
	}
	return strings.Join(lines, "\n") + "\n"
}

func runOmniBudget() error {
	return withConn(func(conn *sql.DB) error {
		bs, err := budgets.NewStore(conn).List(transactions.CycleOf(transactions.Today()), false)
		if err != nil {
			return err
		}
		fmt.Fprint(out, plainBudgets(bs, time.Now()))
		return nil
	})
}

func plainBudgets(bs []budgets.Budget, today time.Time) string {
	if len(bs) == 0 {
		return "No budgets yet — create one with: pecunia budget new\n"
	}
	lines := []string{"📉 Budgets — " + bs[0].Cycle}
	for _, b := range bs {
		status := b.Status(today)
		prefix := "• "
		if status == budgets.StatusOver {
			prefix = "⚠️ "
		}
		lines = append(lines, fmt.Sprintf("%s%s: %s of %s (%d%%, %s)",
			prefix, b.Name, money(b.Spent, b.Cur()), money(b.Amount, b.Cur()), b.Pct(), status))
	}
	return strings.Join(lines, "\n") + "\n"
}

// ccInfo is one card with its statements already read — what plainCC renders.
type ccInfo struct {
	Card cards.Card
	Open *bills.Bill // the statement still taking charges, nil when none is
	Live int64       // what that open statement holds so far
	Owed int64       // what closed statements still owe
}

func runOmniCC() error {
	return withConn(func(conn *sql.DB) error {
		cs, err := cards.NewStore(conn).List()
		if err != nil {
			return err
		}
		store := bills.NewStore(conn)
		infos := make([]ccInfo, len(cs))
		for i, c := range cs {
			infos[i].Card = c
			open, err := store.Open(c)
			switch {
			case err == nil:
				if infos[i].Live, err = store.LiveTotal(open); err != nil {
					return err
				}
				infos[i].Open = &open
			case !errors.Is(err, bills.ErrNotFound):
				return err
			}
			unpaid, err := store.Unpaid(c)
			if err != nil {
				return err
			}
			for _, b := range unpaid {
				infos[i].Owed += b.Owed()
			}
		}
		fmt.Fprint(out, plainCC(infos))
		return nil
	})
}

func plainCC(infos []ccInfo) string {
	if len(infos) == 0 {
		return "No credit cards yet — create one with: pecunia cc new\n"
	}
	lines := []string{"💳 Cards"}
	for _, i := range infos {
		c := i.Card
		lines = append(lines, fmt.Sprintf("• %s (%s): %s used of %s · %s available",
			c.Name, c.Code, money(c.Balance, c.Cur()), money(c.Limit, c.Cur()), money(c.Available(), c.Cur())))
		if i.Open != nil {
			lines = append(lines, fmt.Sprintf("  open statement: %s, closes %s, due %s",
				money(i.Live, c.Cur()), i.Open.ClosesOn, i.Open.DueOn))
		}
		if i.Owed > 0 {
			lines = append(lines, fmt.Sprintf("  ⚠️ closed statements still owe %s", money(i.Owed, c.Cur())))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// parseAdd splits a quick expense: @CODE and #CODE pick the account and
// category from anywhere in the words, the first remaining token is the
// amount, and the rest is the title.
func parseAdd(args []string) (amount, title, acct, cat string, err error) {
	var words []string
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "@") && len(a) > 1:
			acct = a[1:]
		case strings.HasPrefix(a, "#") && len(a) > 1:
			cat = a[1:]
		default:
			words = append(words, a)
		}
	}
	if len(words) < 2 {
		return "", "", "", "", errors.New("usage: /pecunia-add AMOUNT TITLE... [@ACCOUNT] [#CATEGORY]")
	}
	return words[0], strings.Join(words[1:], " "), acct, cat, nil
}

func runOmniAdd(args []string) error {
	amount, title, acctCode, catCode, err := parseAdd(args)
	if err != nil {
		return err
	}
	return withConn(func(conn *sql.DB) error {
		acct, err := pickAccount(conn, acctCode)
		if err != nil {
			return err
		}
		value, err := core.ParseAmount(amount, acct.Cur())
		if err != nil {
			return err
		}
		t := transactions.Transaction{
			Title:    title,
			Value:    value,
			Kind:     transactions.KindOutcome,
			Date:     transactions.Today(),
			Account:  transactions.Ref{ID: acct.ID},
			Currency: acct.Currency,
		}
		if catCode != "" {
			c, err := categories.NewStore(conn).ByCode(catCode)
			if err != nil {
				return fmt.Errorf("#%s: %w", catCode, err)
			}
			t.Category = transactions.Ref{ID: c.ID}
		}
		if err := transactions.NewStore(conn).Create(&t); err != nil {
			return err
		}
		fmt.Fprintf(out, "Added: %s — %s from %s (%s)\n", title, money(value, acct.Cur()), acct.Name, acct.Code)
		return nil
	})
}

// pickAccount resolves where the money leaves from. Without an @CODE it takes
// the only unfrozen account there is — and refuses to choose between two,
// because guessing where money goes is worse than asking.
func pickAccount(conn *sql.DB, code string) (accounts.Account, error) {
	s := accounts.NewStore(conn)
	if code != "" {
		a, err := s.ByCode(code)
		if err != nil {
			return a, fmt.Errorf("@%s: %w", code, err)
		}
		return a, nil
	}
	all, err := s.List()
	if err != nil {
		return accounts.Account{}, err
	}
	var open []accounts.Account
	for _, a := range all {
		if !a.IsFrozen {
			open = append(open, a)
		}
	}
	switch len(open) {
	case 0:
		return accounts.Account{}, errors.New("no accounts yet — create one with: pecunia accounts new")
	case 1:
		return open[0], nil
	}
	codes := make([]string, len(open))
	for i, a := range open {
		codes[i] = "@" + a.Code
	}
	return accounts.Account{}, fmt.Errorf("more than one account — say which with %s", strings.Join(codes, ", "))
}
