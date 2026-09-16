package notes

import (
	"strings"
	"testing"
	"time"
)

// Every assertion here is a substring check: lipgloss wraps its output in ANSI
// escapes that change with the terminal profile, but the text does not.

func scored(n Note, score int) Note {
	n.Score, n.Level = score, Level(score)
	return n
}

func TestPriority(t *testing.T) {
	t.Run("a note that moved shows both levels", func(t *testing.T) {
		got := Priority(scored(health(), 74))
		for _, want := range []string{"LOW", "→", "HIGH", "74"} {
			if !strings.Contains(got, want) {
				t.Errorf("Priority() = %q; want %q in it", got, want)
			}
		}
	})
	t.Run("a closed note shows its base alone, no score", func(t *testing.T) {
		n := scored(health(), 0)
		n.Status = StatusDone
		got := Priority(n)
		if !strings.Contains(got, "LOW") || strings.Contains(got, "→") || strings.Contains(got, "0") {
			t.Errorf("Priority() = %q", got)
		}
	})
	t.Run("a note where it stayed shows one", func(t *testing.T) {
		got := Priority(scored(health(), 20))
		if !strings.Contains(got, "LOW") || strings.Contains(got, "→") || !strings.Contains(got, "20") {
			t.Errorf("Priority() = %q", got)
		}
	})
}

func TestDue(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.Local)
	cases := []struct{ target, want string }{
		{"", "—"},
		{"2026-12-16", "2026-12-16  in 91d"},
		{"2026-09-16", "2026-09-16  today"},
		{"2026-09-17", "2026-09-17  tomorrow"},
		{"2026-09-13", "2026-09-13  3d overdue"},
	}
	for _, tc := range cases {
		n := health()
		n.Target = tc.target
		if got := Due(n, now); got != tc.want {
			t.Errorf("Due(%q) = %q; want %q", tc.target, got, tc.want)
		}
	}
}

func TestTable(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.Local)
	a := scored(health(), 74)
	b := scored(Note{ID: 5, Title: "Renegociar o cartão", Priority: PriorityHigh, Status: StatusDoing,
		Target: "2026-09-13", Tags: []string{"bank"}, Problem: "5-renegociar.md: line 3: unknown key \"priorty\""}, 60)
	c := scored(Note{ID: 8, Title: "Old one", Priority: PriorityCritical, Status: StatusDone}, 0)
	got := Table([]Note{a, b, c}, now)
	if strings.Contains(got, "CRITICAL → LOW") {
		t.Errorf("a done note reads as moved:\n%s", got)
	}
	for _, want := range []string{
		"PRIORITY", "SCORE", "TITLE", "TARGET", "TAGS",
		"3", "Get a better health care", "LOW", "→", "HIGH", "74", "in 91d", "#health", "#insurance",
		"5", "!", "Renegociar o cartão", "doing", "3d overdue", "#bank",
		`unknown key "priorty"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Table() lacks %q:\n%s", want, got)
		}
	}
}

func TestDetails(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.Local)
	n := scored(health(), 74)
	n.ReadCount, n.EditCount = 7, 4
	bd := Breakdown{Base: 20, Target: 25, Engagement: 15, Activity: 14, Decay: 0, Total: 74}
	links := Links{Accounts: []string{"Banco Inter (INTER)"}}

	t.Run("the card and the body", func(t *testing.T) {
		got := Details(n, bd, "# Why\n\nBecause the plan is bad.", links, "/n/3-get-a-better-health-care.md", now)
		for _, want := range []string{
			"Get a better health care",
			"LOW", "→", "HIGH", "74",
			"score 74 = base 20 + target 25 + reads 15 + activity 14 − stale 0",
			"open", "2026-12-16", "in 91d", "in 3 months",
			"#health", "#insurance",
			"Banco Inter (INTER)",
			"read 7×", "edited 4×",
			"/n/3-get-a-better-health-care.md",
			"Why", "Because the plan is bad.",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("Details() lacks %q:\n%s", want, got)
			}
		}
	})

	t.Run("an empty body says how to write one", func(t *testing.T) {
		got := Details(n, bd, "", Links{}, "/n/3.md", now)
		if !strings.Contains(got, "empty — pecunia n e 3 to write it") {
			t.Errorf("Details() = %s", got)
		}
	})

	t.Run("a problem is shown on the card", func(t *testing.T) {
		m := n
		m.Problem = "file missing — pecunia n sync"
		if got := Details(m, bd, "", Links{}, "/n/3.md", now); !strings.Contains(got, "file missing") {
			t.Errorf("Details() = %s", got)
		}
	})
}

func TestLabel(t *testing.T) {
	got := Label(scored(health(), 74))
	for _, want := range []string{"#3", "Get a better health care", "HIGH"} {
		if !strings.Contains(got, want) {
			t.Errorf("Label() = %q; want %q in it", got, want)
		}
	}
}
