package summary

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"pecunia/internal/accounts"
	"pecunia/internal/bills"
	"pecunia/internal/budgets"
	"pecunia/internal/cards"
	"pecunia/internal/core"
	"pecunia/internal/goals"
	"pecunia/internal/recurring"
	"pecunia/internal/transactions"
)

// labelWidth lines the totals up under each other. "month" is the longest of
// them, and one space past it keeps the figures off the words.
const labelWidth = 6

// nothingYet is what an untouched database gets instead of six empty sections.
const nothingYet = "no accounts yet — create one with: pecunia ac n"

var titleStyle = lipgloss.NewStyle().Bold(true)

// net is what the window left behind, each currency on its own. It walks both
// maps because a day can earn in one currency and spend in another, and
// iterating either alone would drop the other.
func net(s Summary) map[string]int64 {
	out := make(map[string]int64, len(s.In)+len(s.Out))
	for c, v := range s.In {
		out[c] += v
	}
	for c, v := range s.Out {
		out[c] -= v
	}
	return out
}

// total is one line of the block at the top: the word, then the figures.
func total(label, figures string) string {
	if figures == "" {
		return ""
	}
	return core.DimStyle.Render(fmt.Sprintf("%-*s", labelWidth, label)) + figures + "\n"
}

// board renders one window's bills and statements. They are two different types
// from two different modules, so they stay two tables — and both are the
// renderers their own commands already use.
func board(b Board, today time.Time) string {
	var parts []string
	if len(b.Bills) > 0 {
		// recurring.Board on an empty slice says "no bills yet", which is the
		// wrong news here: there may be twelve bills and none of them due.
		parts = append(parts, strings.TrimRight(recurring.Board(b.Bills, today), "\n"))
	}
	if len(b.Statements) > 0 {
		// ponytail: bills.Table is seven columns for one fact. One line per
		// card if it ever reads too heavy.
		parts = append(parts, bills.Table(b.Statements))
	}
	return strings.Join(parts, "\n")
}

// section is a heading with something under it, or nothing at all. An empty
// section is left out entirely rather than printed as a heading over a blank —
// except where the caller passes an empty-state line, because "nothing due" is
// news worth reading.
func section(title, body string) string {
	if body == "" {
		return ""
	}
	return "\n" + core.HeaderStyle.Render(title) + "\n" + strings.TrimRight(body, "\n") + "\n"
}

// Render is the whole screen. It is a plain print with no alt screen, so
// `pecunia summary | head` works like every other list in pecunia.
func Render(s Summary) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(s.Title) + "\n")

	if s.empty() {
		b.WriteString("\n" + core.DimStyle.Render(nothingYet) + "\n")
		return b.String()
	}

	totals := total("in", core.MoneyLine(s.In)) + total("out", core.MoneyLine(s.Out)) +
		total("net", core.MoneyLine(net(s))) +
		total("month", withSuffix(core.MoneyLine(s.MTD), " out"))
	if totals != "" {
		b.WriteString("\n" + totals)
	}

	// A clear day says so; a window that ended says nothing, because nothing
	// was read for it.
	due := board(s.Due, s.Today)
	if due == "" && s.live() {
		due = core.DimStyle.Render("nothing due")
	}
	b.WriteString(section("DUE", due))

	ledger := core.DimStyle.Render("no transactions")
	if len(s.Ledger) > 0 {
		// Table on an empty slice is a header with nothing under it, which
		// reads as a broken table rather than as a quiet day.
		ledger = transactions.Table(s.Ledger)
	}
	b.WriteString(section("TRANSACTIONS", ledger))

	b.WriteString(section("NEXT 7 DAYS", board(s.Soon, s.Today)))

	var balances []string
	if len(s.Accounts) > 0 {
		balances = append(balances, accounts.Table(s.Accounts))
	}
	if len(s.Cards) > 0 {
		// A card's balance is a debt, so it never joins the accounts in one
		// figure — two tables, and no total under them.
		balances = append(balances, cards.Table(s.Cards))
	}
	b.WriteString(section("BALANCES", strings.Join(balances, "\n")))

	if len(s.Goals) > 0 {
		b.WriteString(section("GOALS", goals.Table(s.Goals)))
	}
	// Last, and after the balances: a budget is about a month rather than about
	// a moment, so it is what to read once the question of what is in the
	// account has been answered.
	b.WriteString(section("BUDGETS", budgets.Table(s.Budgets, s.Today)))
	return b.String()
}

// withSuffix says what the month figure is, since "month" alone does not say
// whether it came in or went out.
func withSuffix(figures, suffix string) string {
	if figures == "" {
		return ""
	}
	return figures + core.DimStyle.Render(suffix)
}
