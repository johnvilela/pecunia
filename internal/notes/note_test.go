package notes

import (
	"strings"
	"testing"
	"time"
)

// health is a note every case here builds a copy of, so none share state.
func health() Note {
	return Note{
		ID: 3, Title: "Get a better health care", Priority: PriorityLow, Status: StatusOpen,
		Target: "2026-12-16", TargetPhrase: "in 3 months", Tags: []string{"health", "insurance"},
		Accounts: []string{"INTER"}, Path: "3-get-a-better-health-care.md",
		CreatedAt: "2026-09-16 12:00:00", UpdatedAt: "2026-09-16 12:00:00",
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Note)
		want   string // "" = valid, else a substring of the error
	}{
		{"the fixture is valid", func(*Note) {}, ""},
		{"a title is required", func(n *Note) { n.Title = "  " }, "title is required"},
		{"the priority must be in the set", func(n *Note) { n.Priority = "urgent" }, `"urgent" is not a priority`},
		{"the status must be in the set", func(n *Note) { n.Status = "paused" }, `"paused" is not a status`},
		{"a target is a day", func(n *Note) { n.Target = "in 3 months" }, "target must be YYYY-MM-DD"},
		{"no target is fine", func(n *Note) { n.Target = "" }, ""},
		{"too many tags", func(n *Note) { n.Tags = []string{"a", "b", "c", "d", "e", "f"} }, "at most 5 tags"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := health()
			tc.change(&n)
			err := n.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Validate() = %v; want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v; want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestOpen(t *testing.T) {
	for status, want := range map[string]bool{
		StatusOpen: true, StatusDoing: true, StatusDone: false, StatusDropped: false,
	} {
		n := health()
		n.Status = status
		if got := n.Open(); got != want {
			t.Errorf("status %s: Open() = %v; want %v", status, got, want)
		}
	}
}

func TestDaysToTarget(t *testing.T) {
	now := time.Date(2026, 9, 16, 23, 30, 0, 0, time.Local)
	cases := []struct {
		name   string
		target string
		days   int
		ok     bool
	}{
		{"three months out", "2026-12-16", 91, true},
		{"today", "2026-09-16", 0, true},
		{"yesterday is minus one", "2026-09-15", -1, true},
		{"no target", "", 0, false},
		{"a broken target is no target", "soon", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := health()
			n.Target = tc.target
			days, ok := n.DaysToTarget(now)
			if ok != tc.ok || days != tc.days {
				t.Fatalf("DaysToTarget() = %d, %v; want %d, %v", days, ok, tc.days, tc.ok)
			}
		})
	}
}

func TestSlug(t *testing.T) {
	cases := []struct {
		name, title, want string
	}{
		{"lowercases and hyphenates", "Get a better Health Care", "get-a-better-health-care"},
		{"accents are kept", "Renegociar o cartão Itaú", "renegociar-o-cartão-itaú"},
		{"punctuation collapses to one hyphen", "Plan: save (12%) -- now!", "plan-save-12-now"},
		{"edges are trimmed", "  ...hello...  ", "hello"},
		{"long titles are cut at forty runes", strings.Repeat("abcde ", 20), "abcde-abcde-abcde-abcde-abcde-abcde-abcd"},
		{"nothing usable falls back", "!!!", "note"},
		{"empty falls back", "", "note"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Slug(tc.title); got != tc.want {
				t.Fatalf("Slug(%q) = %q; want %q", tc.title, got, tc.want)
			}
		})
	}
}

func TestFileName(t *testing.T) {
	if got := FileName(3, "Get a better health care"); got != "3-get-a-better-health-care.md" {
		t.Fatalf("FileName() = %q", got)
	}
}
