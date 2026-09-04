package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"pecunia/internal/accounts"
	"pecunia/internal/budgets"
	"pecunia/internal/cards"
	"pecunia/internal/goals"
	"pecunia/internal/recurring"
	"pecunia/internal/summary"
)

// 2026-09-04 is a Friday, so its week runs 2026-08-31 through 2026-09-06.
var omniNow = time.Date(2026, 9, 4, 15, 0, 0, 0, time.UTC)

func TestParsePeriod(t *testing.T) {
	cases := []struct {
		phrase   string
		from, to string
	}{
		{"", "2026-09-01", "2026-09-30"},
		{"month", "2026-09-01", "2026-09-30"},
		{"this month", "2026-09-01", "2026-09-30"},
		{"last month", "2026-08-01", "2026-08-31"},
		{"today", "2026-09-04", "2026-09-04"},
		{"yesterday", "2026-09-03", "2026-09-03"},
		{"week", "2026-08-31", "2026-09-06"},
		{"this week", "2026-08-31", "2026-09-06"},
		{"last week", "2026-08-24", "2026-08-30"},
		{"2026-07", "2026-07-01", "2026-07-31"},
		{"2026-07-15", "2026-07-15", "2026-07-15"},
	}
	for _, c := range cases {
		t.Run("phrase "+c.phrase, func(t *testing.T) {
			var args []string
			if c.phrase != "" {
				args = strings.Fields(c.phrase)
			}
			p, err := parsePeriod(args, omniNow)
			if err != nil {
				t.Fatal(err)
			}
			if p.From != c.from || p.To != c.to {
				t.Errorf("got %s..%s; want %s..%s", p.From, p.To, c.from, c.to)
			}
		})
	}

	// A Sunday still belongs to the week that started the Monday before.
	sunday := time.Date(2026, 9, 6, 8, 0, 0, 0, time.UTC)
	p, err := parsePeriod([]string{"week"}, sunday)
	if err != nil {
		t.Fatal(err)
	}
	if p.From != "2026-08-31" || p.To != "2026-09-06" {
		t.Errorf("sunday's week is %s..%s; want 2026-08-31..2026-09-06", p.From, p.To)
	}

	if _, err := parsePeriod([]string{"fortnight"}, omniNow); err == nil {
		t.Error("garbage phrase did not error")
	}
}

func TestParseAdd(t *testing.T) {
	amount, title, acct, cat, err := parseAdd([]string{"12.50", "lunch", "at", "the", "corner", "@NUBNK", "#FOOD1"})
	if err != nil {
		t.Fatal(err)
	}
	if amount != "12.50" || title != "lunch at the corner" || acct != "NUBNK" || cat != "FOOD1" {
		t.Errorf("got %q %q %q %q", amount, title, acct, cat)
	}

	// Tokens work from anywhere in the message.
	_, title, acct, _, err = parseAdd([]string{"@NUBNK", "9", "coffee"})
	if err != nil {
		t.Fatal(err)
	}
	if title != "coffee" || acct != "NUBNK" {
		t.Errorf("got title %q acct %q", title, acct)
	}

	if _, _, _, _, err := parseAdd([]string{"12.50"}); err == nil {
		t.Error("amount without a title did not error")
	}
	if _, _, _, _, err := parseAdd(nil); err == nil {
		t.Error("no words did not error")
	}
}

func omniBill(name string, expected int64, payments map[string]recurring.Tally) recurring.Bill {
	return recurring.Bill{
		Name: name, Expected: expected, OpenDay: 1, DueDay: 10, Active: true,
		Currency: "BRL", Payments: payments, CreatedAt: "2026-08-15 00:00:00",
	}
}

func TestAlertLines(t *testing.T) {
	overdue := omniBill("Internet", 12000, nil) // 2026-08 unpaid, due 2026-08-10
	paid := omniBill("Rent", 0, map[string]recurring.Tally{
		"2026-08": {Value: 90000, Count: 1}, "2026-09": {Value: 90000, Count: 1},
	})
	over := budgets.Budget{Name: "Restaurants", Amount: 100000, Spent: 150000,
		Currency: "BRL", Cycle: "2026-09", Active: true}
	onTrack := budgets.Budget{Name: "Groceries", Amount: 100000, Spent: 10000,
		Currency: "BRL", Cycle: "2026-09", Active: true}
	nearLimit := cards.Card{Name: "Nubank", Limit: 10000, Balance: 9000, Currency: "BRL"}
	fine := cards.Card{Name: "Chase", Limit: 10000, Balance: 8900, Currency: "USD"}
	noLimit := cards.Card{Name: "Odd", Limit: 0, Balance: 0, Currency: "BRL"}

	lines := alertLines(
		[]recurring.Bill{overdue, paid},
		[]budgets.Budget{over, onTrack},
		[]cards.Card{nearLimit, fine, noLimit},
		omniNow,
	)
	if len(lines) != 3 {
		t.Fatalf("got %d alerts; want 3:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if want := "Internet (R$120.00) is 25 days overdue — was due 2026-08-10"; !strings.Contains(lines[0], want) {
		t.Errorf("bill alert %q does not contain %q", lines[0], want)
	}
	if want := "Restaurants budget is over its cap: R$1500.00 of R$1000.00 (150%)"; !strings.Contains(lines[1], want) {
		t.Errorf("budget alert %q does not contain %q", lines[1], want)
	}
	if want := "Nubank is at 90% of its limit"; !strings.Contains(lines[2], want) {
		t.Errorf("card alert %q does not contain %q", lines[2], want)
	}

	if quiet := alertLines([]recurring.Bill{paid}, []budgets.Budget{onTrack}, []cards.Card{fine}, omniNow); len(quiet) != 0 {
		t.Errorf("all-well fixtures raised alerts: %v", quiet)
	}
}

func TestPlainSummary(t *testing.T) {
	s := summary.Summary{
		Period: summary.Period{From: "2026-09-01", To: "2026-09-30"},
		Title:  "September 2026",
		In:     map[string]int64{"BRL": 850000},
		Out:    map[string]int64{"BRL": 523012, "USD": 100},
		Accounts: []accounts.Account{
			{Name: "Nubank", Code: "NUBNK", Balance: 432055, Currency: "BRL"},
		},
		Cards: []cards.Card{
			{Name: "Nubank Card", Code: "NUCRD", Limit: 500000, Balance: 120000, Currency: "BRL"},
		},
	}
	got := plainSummary(s, []string{"⚠️ Internet is 3 days overdue — was due 2026-09-01"})

	for _, want := range []string{
		"📊 September 2026",
		"In: R$8500.00",
		"Out: R$5230.12 · $1.00",
		"• Nubank (NUBNK): R$4320.55",
		"• Nubank Card (NUCRD): R$1200.00 owed · R$3800.00 available",
		"Alerts:\n⚠️ Internet",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b") {
		t.Error("summary contains ANSI escapes")
	}

	// A week is neither a day nor a month, so its label is its two ends.
	s.Period = summary.Period{From: "2026-08-31", To: "2026-09-06"}
	if got := plainSummary(s, nil); !strings.Contains(got, "📊 2026-08-31 to 2026-09-06") {
		t.Errorf("week window label missing:\n%s", got)
	}

	empty := summary.Summary{Period: summary.Period{From: "2026-09-04", To: "2026-09-04"}, Title: "Friday, 4 September 2026"}
	if got := plainSummary(empty, nil); !strings.Contains(got, "Nothing moved in this window.") {
		t.Errorf("empty window says nothing:\n%s", got)
	}
}

func TestPlainGoals(t *testing.T) {
	gs := []goals.Goal{
		{Name: "Trip to Japan", Target: 1000000, Net: 620000, Currency: "BRL", Kind: goals.KindSaving},
		{Name: "Car loan", Target: 500000, Net: -500000, Currency: "BRL", Kind: goals.KindPaying},
	}
	got := plainGoals(gs)
	if want := "• Trip to Japan: R$6200.00 saved of R$10000.00 (62%)"; !strings.Contains(got, want) {
		t.Errorf("goals do not contain %q:\n%s", want, got)
	}
	if want := "• Car loan: R$5000.00 paid off of R$5000.00 (100%) ✅"; !strings.Contains(got, want) {
		t.Errorf("goals do not contain %q:\n%s", want, got)
	}
	if strings.Contains(got, "\x1b") {
		t.Error("goals contain ANSI escapes")
	}
	if got := plainGoals(nil); !strings.Contains(got, "No goals yet") {
		t.Errorf("empty list says %q", got)
	}
}

func TestPlainBills(t *testing.T) {
	bs := []recurring.Bill{
		omniBill("Internet", 12000, nil),
		omniBill("Rent", 0, map[string]recurring.Tally{
			"2026-08": {Value: 90000, Count: 1}, "2026-09": {Value: 90000, Count: 1},
		}),
	}
	got := plainBills(bs, omniNow)
	if want := "• Internet (R$120.00) [2026-08]: ⚠️ 25 days overdue — was due 2026-08-10"; !strings.Contains(got, want) {
		t.Errorf("bills do not contain %q:\n%s", want, got)
	}
	if want := "• Rent [2026-09]: ✅ paid R$900.00"; !strings.Contains(got, want) {
		t.Errorf("bills do not contain %q:\n%s", want, got)
	}
	if got := plainBills(nil, omniNow); !strings.Contains(got, "No recurring bills yet") {
		t.Errorf("empty list says %q", got)
	}
}

var cmdName = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

func TestManifest(t *testing.T) {
	m := manifest()
	if m.Name != "pecunia" {
		t.Errorf("name is %q", m.Name)
	}
	if m.Version != version {
		t.Errorf("manifest version %q; binary is %q", m.Version, version)
	}
	if !m.Skills {
		t.Error("skills flag is off")
	}
	if m.MCP.Command != "pecunia" || len(m.MCP.Args) != 1 || m.MCP.Args[0] != "mcp" {
		t.Errorf("mcp entry is %+v", m.MCP)
	}
	if len(m.Commands) != 8 {
		t.Fatalf("got %d commands; want 8", len(m.Commands))
	}
	for _, c := range m.Commands {
		if !cmdName.MatchString(c.Name) {
			t.Errorf("command name %q breaks Telegram's rule", c.Name)
		}
		if !strings.HasPrefix(c.Name, "pecunia_") {
			t.Errorf("command name %q is not prefixed with the plugin name", c.Name)
		}
		// BotFather caps a command description at 256 characters.
		if len(c.Description) == 0 || len(c.Description) > 256 {
			t.Errorf("%s description is %d chars; want 1-256", c.Name, len(c.Description))
		}
		// Omni's contract: exactly one of argv and prompt.
		if c.Prompt != "" {
			if len(c.Argv) != 0 {
				t.Errorf("%s declares both argv and prompt", c.Name)
			}
			continue
		}
		if len(c.Argv) < 2 || c.Argv[0] != "pecunia" || c.Argv[1] != "omni" {
			t.Errorf("%s argv is %v; want pecunia omni ...", c.Name, c.Argv)
		}
	}
	coach := m.Commands[len(m.Commands)-1]
	if coach.Name != "pecunia_coach" || coach.Prompt != coachPrompt {
		t.Errorf("last command is %q with prompt %d chars; want pecunia_coach carrying coachPrompt", coach.Name, len(coach.Prompt))
	}
}

func TestRunOmniSkills(t *testing.T) {
	dir := t.TempDir()
	if err := runOmniSkills([]string{dir}); err != nil {
		t.Fatal(err)
	}
	entries, err := skillFS.ReadDir("skills")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatalf("embedded %d skills; want 5", len(entries))
	}
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".md")
		got, err := os.ReadFile(filepath.Join(dir, name, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		want, err := skillFS.ReadFile("skills/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s differs from the embedded copy", name)
		}
	}

	if err := runOmniSkills(nil); err == nil {
		t.Error("missing directory did not error")
	}
}
