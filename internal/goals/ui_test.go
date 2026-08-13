package goals

import (
	"strings"
	"testing"
)

// Every assertion here is a substring check: lipgloss wraps its output in ANSI
// escapes that change with the terminal profile, but the text does not.

func TestLabel(t *testing.T) {
	got := Label(laptop())
	for _, want := range []string{"New laptop", "R$5000.00"} {
		if !strings.Contains(got, want) {
			t.Errorf("Label = %q; want it to contain %q", got, want)
		}
	}
}

func TestBar(t *testing.T) {
	// filled counts the drawn blocks, which is the only thing about the bar that
	// is not styling.
	filled := func(g Goal) int { return strings.Count(bar(g), "█") }

	cases := []struct {
		name string
		net  int64
		want int
	}{
		{"empty at nothing", 0, 0},
		{"a quarter of the way", 125000, barWidth / 4},
		{"half way", 250000, barWidth / 2},
		{"full at the target", 500000, barWidth},
		{"clamped full past the target", 900000, barWidth},
		{"clamped empty going backwards", -100000, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := laptop()
			g.Net = tc.net
			if got := filled(g); got != tc.want {
				t.Fatalf("bar filled %d of %d blocks; want %d", got, barWidth, tc.want)
			}
		})
	}

	t.Run("the bar is always its full width", func(t *testing.T) {
		g := laptop()
		g.Net = 300000
		if n := strings.Count(bar(g), "█") + strings.Count(bar(g), "░"); n != barWidth {
			t.Fatalf("bar is %d blocks wide; want %d", n, barWidth)
		}
	})
}

func TestPct(t *testing.T) {
	cases := []struct {
		name string
		net  int64
		want string
	}{
		{"half way", 250000, "50%"},
		{"nothing yet", 0, "0%"},
		// Not clamped: 180% is news, and the bar alone cannot tell it from 100%.
		{"past the target", 900000, "180%"},
		{"going backwards", -50000, "-10%"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := laptop()
			g.Net = tc.net
			if got := pct(g); got != tc.want {
				t.Fatalf("pct = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestTable(t *testing.T) {
	saving := laptop()
	paying := laptop()
	paying.Name, paying.Kind, paying.Net, paying.Target = "Student loan", KindPaying, -300000, 900000

	got := Table([]Goal{saving, paying})
	for _, want := range []string{
		"GOAL", "PROGRESS", "TARGET",
		"New laptop", "R$1200.00", "saved", "R$5000.00",
		"Student loan", "R$3000.00", "paid off", "R$9000.00",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Table is missing %q:\n%s", want, got)
		}
	}
}

func TestDetails(t *testing.T) {
	t.Run("a saving goal shows what is saved, what is left and a bar", func(t *testing.T) {
		got := Details(laptop(), nil)
		for _, want := range []string{
			"New laptop", "money for the new machine",
			"R$1200.00", "saved", "R$5000.00", "R$3800.00", "to go", "24%", "█", "░",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("Details is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("no field names", func(t *testing.T) {
		got := Details(laptop(), nil)
		for _, unwanted := range []string{"Name", "Target", "Currency", "Kind", "Description"} {
			if strings.Contains(got, unwanted) {
				t.Errorf("Details names the field %q:\n%s", unwanted, got)
			}
		}
	})

	t.Run("a reached goal says so", func(t *testing.T) {
		g := laptop()
		g.Net = 500000
		got := Details(g, nil)
		if !strings.Contains(got, "reached") {
			t.Errorf("Details does not say the goal is reached:\n%s", got)
		}
		if strings.Contains(got, "to go") {
			t.Errorf("Details still has something to go:\n%s", got)
		}
	})

	t.Run("a goal past its target says how far past", func(t *testing.T) {
		g := laptop()
		g.Net = 512000
		got := Details(g, nil)
		// "R$-12000.00 to go" is a riddle; "R$120.00 past it" is not.
		if !strings.Contains(got, "R$120.00") || !strings.Contains(got, "past it") {
			t.Errorf("Details does not say how far past the target it is:\n%s", got)
		}
	})

	t.Run("a goal with no description leaves the line out", func(t *testing.T) {
		g := laptop()
		g.Description = ""
		if got := Details(g, nil); strings.Contains(got, "money for the new machine") {
			t.Errorf("Details kept a description that is not there:\n%s", got)
		}
	})

	t.Run("a paying goal reads as paid off", func(t *testing.T) {
		g := laptop()
		g.Kind, g.Net = KindPaying, -120000
		got := Details(g, nil)
		if !strings.Contains(got, "paid off") {
			t.Errorf("Details does not say what a paying goal is at:\n%s", got)
		}
	})
}

func TestPickerRow(t *testing.T) {
	row := pickerRow(laptop())
	if !strings.Contains(row.Label, "New laptop") {
		t.Errorf("picker label = %q; want the name in it", row.Label)
	}
	if !strings.Contains(row.Desc, "R$1200.00") || !strings.Contains(row.Desc, "saved") {
		t.Errorf("picker description = %q; want what the goal is at", row.Desc)
	}
	if !strings.Contains(strings.ToLower(row.Filter), "laptop") {
		t.Errorf("picker filter = %q; want the name to be searchable", row.Filter)
	}
}

func TestReachedMark(t *testing.T) {
	reached := laptop()
	reached.Net = 500000

	t.Run("the table marks a reached goal after its name", func(t *testing.T) {
		got := Table([]Goal{reached})
		if !strings.Contains(got, "New laptop "+reachedMark) {
			t.Fatalf("the table does not mark the reached goal:\n%s", got)
		}
	})

	t.Run("a goal still going is not marked", func(t *testing.T) {
		if got := Table([]Goal{laptop()}); strings.Contains(got, reachedMark) {
			t.Fatalf("the table marks a goal that is not there yet:\n%s", got)
		}
	})

	t.Run("a paying goal is marked the same way", func(t *testing.T) {
		g := laptop()
		g.Kind, g.Net = KindPaying, -500000
		if got := Table([]Goal{g}); !strings.Contains(got, reachedMark) {
			t.Fatalf("a paid-off goal is not marked:\n%s", got)
		}
	})

	t.Run("the card carries it too, since that is the default list", func(t *testing.T) {
		if !strings.Contains(Details(reached, nil), reachedMark) {
			t.Errorf("the card does not mark the reached goal:\n%s", Details(reached, nil))
		}
		if strings.Contains(Details(laptop(), nil), reachedMark) {
			t.Errorf("the card marks a goal that is not there yet:\n%s", Details(laptop(), nil))
		}
	})

	t.Run("the picker and the transaction form's select carry it too", func(t *testing.T) {
		if !strings.Contains(Label(reached), reachedMark) {
			t.Errorf("Label = %q; want the mark on a reached goal", Label(reached))
		}
		if strings.Contains(Label(laptop()), reachedMark) {
			t.Errorf("Label = %q; want no mark while there is still something to go", Label(laptop()))
		}
	})
}

func TestDetailsTargetLog(t *testing.T) {
	log := []TargetChange{
		{Previous: 500000, Target: 350000, Note: "consegui um desconto", CreatedAt: "2026-08-13 09:12:00"},
		{Previous: 600000, Target: 500000, CreatedAt: "2026-07-02 18:30:00"},
	}

	t.Run("every entry says when, from what, to what and why", func(t *testing.T) {
		got := Details(laptop(), log)
		for _, want := range []string{
			"2026-08-13", "R$5000.00", "R$3500.00", "consegui um desconto",
			"2026-07-02", "R$6000.00",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("the history is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("an entry with no reason still renders", func(t *testing.T) {
		got := Details(laptop(), log[1:])
		if !strings.Contains(got, "R$6000.00") || !strings.Contains(got, "2026-07-02") {
			t.Errorf("an entry with no note did not render:\n%s", got)
		}
	})

	t.Run("a goal whose target never moved shows no history at all", func(t *testing.T) {
		got := Details(laptop(), nil)
		if strings.Contains(got, "target") {
			t.Errorf("the card talks about a history it does not have:\n%s", got)
		}
	})

	t.Run("the time of day is left off — the day is what matters", func(t *testing.T) {
		if got := Details(laptop(), log); strings.Contains(got, "09:12:00") {
			t.Errorf("the history is showing clock time:\n%s", got)
		}
	})
}
