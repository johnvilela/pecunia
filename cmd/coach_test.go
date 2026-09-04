package main

import (
	"strings"
	"testing"

	"pecunia/internal/accounts"
	"pecunia/internal/bills"
	"pecunia/internal/budgets"
	"pecunia/internal/cards"
	"pecunia/internal/goals"
	"pecunia/internal/recurring"
	"pecunia/internal/summary"
)

func TestPlainSituation(t *testing.T) {
	s := summary.Summary{
		Period: summary.Period{From: "2026-09-04", To: "2026-09-04"},
		Title:  "Friday, 4 September 2026",
		In:     map[string]int64{"BRL": 50000},
		Out:    map[string]int64{"BRL": 12000},
		MTD:    map[string]int64{"BRL": 523012},
		Accounts: []accounts.Account{
			{Name: "Nubank", Code: "NUBNK", Balance: 432055, Currency: "BRL"},
		},
		Goals: []goals.Goal{
			{Name: "Trip", Target: 1000000, Net: 620000, Currency: "BRL", Kind: goals.KindSaving},
		},
		Budgets: []budgets.Budget{
			{Name: "Groceries", Amount: 100000, Spent: 50000, Currency: "BRL", Cycle: "2026-09", Active: true},
		},
	}
	bs := []recurring.Bill{omniBill("Internet", 12000, nil)}
	open := bills.Bill{ClosesOn: "2026-09-28", DueOn: "2026-10-05"}
	infos := []ccInfo{{
		Card: cards.Card{Name: "Nubank Card", Code: "NUCRD", Limit: 500000, Balance: 120000, Currency: "BRL"},
		Open: &open, Live: 45000, Owed: 30000,
	}}
	alerts := []string{"⚠️ Internet is 25 days overdue — was due 2026-08-10"}

	got := plainSituation(s, bs, infos, alerts, omniNow)
	for _, want := range []string{
		"📊 Friday, 4 September 2026",
		"Month to date out: R$5230.12",
		"• Internet (R$120.00) [2026-08]: ⚠️ 25 days overdue — was due 2026-08-10",
		"open statement: R$450.00, closes 2026-09-28, due 2026-10-05",
		"⚠️ closed statements still owe R$300.00",
		"• Trip: R$6200.00 saved of R$10000.00 (62%)",
		"• Groceries: R$500.00 of R$1000.00 (50%",
		"Alerts:\n⚠️ Internet",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("situation does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b") {
		t.Error("situation contains ANSI escapes")
	}

	// Every section renders even when empty — the fallback lines tell the
	// coach which modules the owner is not using yet.
	empty := summary.Summary{Period: s.Period, Title: s.Title}
	got = plainSituation(empty, nil, nil, nil, omniNow)
	for _, want := range []string{
		"No recurring bills yet", "No credit cards yet", "No goals yet", "No budgets yet",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("empty situation does not contain %q:\n%s", want, got)
		}
	}
}

// TestCoachPrompt pins the prompt's load-bearing tokens, the way
// skills_test.go pins the skills' hard rules.
func TestCoachPrompt(t *testing.T) {
	for _, want := range []string{
		"pecunia_situation", // the snapshot tool the coach must call first
		"pecunia-coach.md",  // the fixed plan slug — the single-plan invariant
		"[pecunia-coach]",   // the cron text marker future runs recognize
		"--forget",          // the wipe flow
		"across currencies", // never sum or compare across currencies
		"Owner's message",   // how omni hands over the user's trailing words
		"status: active",    // the plan page frontmatter
	} {
		if !strings.Contains(coachPrompt, want) {
			t.Errorf("coach prompt does not contain %q", want)
		}
	}
}
