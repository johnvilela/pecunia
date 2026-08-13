package goals

import (
	"strings"
	"testing"
)

// laptop is a goal worth R$5000.00 with R$1200.00 already against it. Every
// case here builds on a copy, so none of them share state.
func laptop() Goal {
	return Goal{
		ID: 1, Name: "New laptop", Description: "money for the new machine",
		Target: 500000, Currency: "BRL", Kind: KindSaving, Net: 120000,
	}
}

func TestProgress(t *testing.T) {
	cases := []struct {
		name string
		kind string
		net  int64
		want int64
	}{
		{"a saving goal counts income minus outcome", KindSaving, 120000, 120000},
		{"a paying goal counts outcome minus income", KindPaying, -120000, 120000},
		{"a goal with nothing against it is at zero", KindSaving, 0, 0},
		{"a saving goal that spent more than it saved goes backwards", KindSaving, -5000, -5000},
		{"a paying goal that took money back goes backwards", KindPaying, 5000, -5000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := laptop()
			g.Kind, g.Net = tc.kind, tc.net
			if got := g.Progress(); got != tc.want {
				t.Fatalf("Progress() = %d; want %d", got, tc.want)
			}
		})
	}
}

func TestRemainingAndReached(t *testing.T) {
	cases := []struct {
		name      string
		net       int64
		remaining int64
		reached   bool
	}{
		{"part way there", 120000, 380000, false},
		{"nothing yet", 0, 500000, false},
		{"one centavo short", 499999, 1, false},
		{"exactly there", 500000, 0, true},
		{"past it", 512000, -12000, true},
		{"going backwards", -1000, 501000, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := laptop()
			g.Net = tc.net
			if got := g.Remaining(); got != tc.remaining {
				t.Errorf("Remaining() = %d; want %d", got, tc.remaining)
			}
			if got := g.Reached(); got != tc.reached {
				t.Errorf("Reached() = %v; want %v", got, tc.reached)
			}
		})
	}
}

func TestFmt(t *testing.T) {
	t.Run("a real is two decimal places", func(t *testing.T) {
		g := laptop()
		if got := g.Fmt(500000); got != "R$5000.00" {
			t.Fatalf("Fmt(500000) = %q; want R$5000.00", got)
		}
	})

	t.Run("bitcoin is eight", func(t *testing.T) {
		g := laptop()
		g.Currency = "BTC"
		if got := g.Fmt(150000000); got != "₿1.50000000" {
			t.Fatalf("Fmt(150000000) = %q; want ₿1.50000000", got)
		}
	})

	t.Run("an unknown currency still renders", func(t *testing.T) {
		g := laptop()
		g.Currency = "ZZZ"
		if got := g.Fmt(100); got == "" {
			t.Fatal("Fmt with a hand-edited currency = empty; want the fallback to render")
		}
	})
}

func TestVerb(t *testing.T) {
	saving, paying := laptop(), laptop()
	paying.Kind = KindPaying
	if got := saving.Verb(); got != "saved" {
		t.Errorf("a saving goal's Verb() = %q; want saved", got)
	}
	if got := paying.Verb(); got != "paid off" {
		t.Errorf("a paying goal's Verb() = %q; want paid off", got)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Goal)
		want string // a substring of the error, or empty for "accepted"
	}{
		{"a saving goal is accepted", func(*Goal) {}, ""},
		{"a paying goal is accepted", func(g *Goal) { g.Kind = KindPaying }, ""},
		{"a nameless goal is refused", func(g *Goal) { g.Name = "" }, "name"},
		{"a blank name is refused", func(g *Goal) { g.Name = "   " }, "name"},
		{"a target of zero is refused", func(g *Goal) { g.Target = 0 }, "more than zero"},
		{"a negative target is refused", func(g *Goal) { g.Target = -1 }, "more than zero"},
		{"an unknown kind is refused", func(g *Goal) { g.Kind = "wishing" }, "saving or paying"},
		{"an unknown currency is refused", func(g *Goal) { g.Currency = "ZZZ" }, "currency"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := laptop()
			tc.edit(&g)
			err := g.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Validate() = %v; want it accepted", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v; want an error mentioning %q", err, tc.want)
			}
		})
	}
}
