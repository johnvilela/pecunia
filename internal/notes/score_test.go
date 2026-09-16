package notes

import (
	"testing"
	"time"
)

// now is a fixed day for every score case: 2026-09-16, mid-afternoon, local.
var now = time.Date(2026, 9, 16, 15, 0, 0, 0, time.Local)

// stamp is a UTC timestamp the way SQLite writes them, days before now.
func stamp(daysAgo int) string {
	return now.UTC().AddDate(0, 0, -daysAgo).Format("2006-01-02 15:04:05")
}

// fresh is a note touched today with nothing else going for it, so each case
// changes one thing and reads one component.
func fresh(priority string) Note {
	return Note{Title: "x", Priority: priority, Status: StatusOpen,
		CreatedAt: stamp(0), UpdatedAt: stamp(0)}
}

func TestExplainBase(t *testing.T) {
	for priority, want := range map[string]int{PriorityLow: 20, PriorityMedium: 40, PriorityHigh: 60, PriorityCritical: 80} {
		if got := Explain(fresh(priority), Activity{}, now); got.Base != want || got.Total != want {
			t.Errorf("%s: base %d total %d; want %d", priority, got.Base, got.Total, want)
		}
	}
}

func TestExplainTarget(t *testing.T) {
	cases := []struct {
		name string
		days int
		want int
	}{
		{"overdue", -1, 30}, {"long overdue", -400, 30},
		{"today", 0, 25}, {"a week away", 7, 25},
		{"eight days", 8, 15}, {"a month", 30, 15},
		{"31 days", 31, 8}, {"90 days", 90, 8},
		{"91 days", 91, 3}, {"a year", 365, 3},
		{"over a year", 366, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := fresh(PriorityLow)
			n.Target = now.AddDate(0, 0, tc.days).Format("2006-01-02")
			if got := Explain(n, Activity{}, now).Target; got != tc.want {
				t.Fatalf("target %d days out = %d; want %d", tc.days, got, tc.want)
			}
		})
	}
	if got := Explain(fresh(PriorityLow), Activity{}, now).Target; got != 0 {
		t.Errorf("no target = %d; want 0", got)
	}
}

func TestExplainEngagement(t *testing.T) {
	cases := []struct{ reads, edits, want int }{
		{0, 0, 0}, {1, 0, 1}, {0, 1, 2}, {5, 3, 11}, {7, 4, 15}, {40, 10, 15},
	}
	for _, tc := range cases {
		n := fresh(PriorityLow)
		n.ReadCount, n.EditCount = tc.reads, tc.edits
		if got := Explain(n, Activity{}, now).Engagement; got != tc.want {
			t.Errorf("%d reads, %d edits = %d; want %d", tc.reads, tc.edits, got, tc.want)
		}
	}
}

func TestExplainActivity(t *testing.T) {
	cases := []struct{ tx30, tx90, want int }{
		{0, 0, 0}, {1, 1, 2}, {0, 9, 3}, {6, 12, 14}, {10, 10, 20}, {30, 90, 20},
	}
	for _, tc := range cases {
		if got := Explain(fresh(PriorityLow), Activity{Tx30: tc.tx30, Tx90: tc.tx90}, now).Activity; got != tc.want {
			t.Errorf("%d/%d transactions = %d; want %d", tc.tx30, tc.tx90, got, tc.want)
		}
	}
}

func TestExplainDecay(t *testing.T) {
	cases := []struct {
		name string
		idle int
		want int
	}{
		{"today", 0, 0}, {"a month", 30, 0}, {"31 days", 31, 0}, {"37 days is one week past", 37, 2},
		{"two months", 60, 8}, {"160 days", 160, 35}, {"a year is capped", 365, 35},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := fresh(PriorityHigh)
			n.CreatedAt, n.UpdatedAt = stamp(tc.idle), stamp(tc.idle)
			if got := Explain(n, Activity{}, now).Decay; got != tc.want {
				t.Fatalf("idle %d days = %d; want %d", tc.idle, got, tc.want)
			}
		})
	}

	t.Run("a read counts as touching it", func(t *testing.T) {
		n := fresh(PriorityHigh)
		n.CreatedAt, n.UpdatedAt, n.LastReadAt = stamp(160), stamp(160), stamp(3)
		if got := Explain(n, Activity{}, now).Decay; got != 0 {
			t.Fatalf("decay = %d; want 0 after a read three days ago", got)
		}
	})

	t.Run("an unreadable stamp is treated as today", func(t *testing.T) {
		n := fresh(PriorityHigh)
		n.CreatedAt, n.UpdatedAt = "", "garbage"
		if got := Explain(n, Activity{}, now).Decay; got != 0 {
			t.Fatalf("decay = %d; want 0", got)
		}
	})
}

func TestScoreMoves(t *testing.T) {
	t.Run("a low note climbs to high", func(t *testing.T) {
		n := fresh(PriorityLow)
		n.Target = now.AddDate(0, 0, 5).Format("2006-01-02")
		n.ReadCount, n.EditCount = 7, 4
		bd := Explain(n, Activity{Tx30: 6, Tx90: 12}, now)
		if bd.Total != 74 || Level(bd.Total) != PriorityHigh {
			t.Fatalf("Explain() = %+v (%s); want 74, high", bd, Level(bd.Total))
		}
	})

	t.Run("a high note left alone sinks to low", func(t *testing.T) {
		n := fresh(PriorityHigh)
		n.CreatedAt, n.UpdatedAt = stamp(160), stamp(160)
		bd := Explain(n, Activity{}, now)
		if bd.Total != 25 || Level(bd.Total) != PriorityLow {
			t.Fatalf("Explain() = %+v (%s); want 25, low", bd, Level(bd.Total))
		}
	})

	t.Run("a critical note never sinks below medium", func(t *testing.T) {
		n := fresh(PriorityCritical)
		n.CreatedAt, n.UpdatedAt = stamp(400), stamp(400)
		if got := Score(n, Activity{}, now); got != 45 || Level(got) != PriorityMedium {
			t.Fatalf("Score() = %d (%s); want 45, medium", got, Level(got))
		}
	})

	t.Run("an overdue low note is medium", func(t *testing.T) {
		n := fresh(PriorityLow)
		n.Target = now.AddDate(0, 0, -3).Format("2006-01-02")
		if got := Score(n, Activity{}, now); got != 50 || Level(got) != PriorityMedium {
			t.Fatalf("Score() = %d (%s); want 50, medium", got, Level(got))
		}
	})

	t.Run("done and dropped score nothing", func(t *testing.T) {
		for _, status := range []string{StatusDone, StatusDropped} {
			n := fresh(PriorityCritical)
			n.Status = status
			n.Target = now.Format("2006-01-02")
			if got := Score(n, Activity{Tx30: 50}, now); got != 0 {
				t.Errorf("%s: Score() = %d; want 0", status, got)
			}
		}
	})

	t.Run("the score is capped at 100", func(t *testing.T) {
		n := fresh(PriorityCritical)
		n.Target = now.Format("2006-01-02")
		n.ReadCount = 50
		if got := Score(n, Activity{Tx30: 50}, now); got != 100 {
			t.Fatalf("Score() = %d; want 100", got)
		}
	})
}

func TestLevel(t *testing.T) {
	for score, want := range map[int]string{
		0: PriorityLow, 29: PriorityLow, 30: PriorityMedium, 54: PriorityMedium,
		55: PriorityHigh, 74: PriorityHigh, 75: PriorityCritical, 100: PriorityCritical,
	} {
		if got := Level(score); got != want {
			t.Errorf("Level(%d) = %s; want %s", score, got, want)
		}
	}
}
