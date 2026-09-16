package notes

import (
	"time"
)

// Activity is how busy the accounts, cards and goals a note names have been:
// distinct transactions on any of them in the last 30 and 90 days.
type Activity struct{ Tx30, Tx90 int }

// Breakdown is a score with its parts showing, so "why is this note HIGH?" has
// an answer on the detail card.
type Breakdown struct{ Base, Target, Engagement, Activity, Decay, Total int }

// The score is what turns a priority the owner wrote once into one that moves.
// Every part is a small integer with a cap, so no single signal can drown the
// others, and the base is the largest of them — the owner's own call still
// counts most.
//
//	base        low 20, medium 40, high 60, critical 80
//	target      +30 once past, +25 within a week, +15 within a month,
//	            +8 within three, +3 within a year
//	engagement  reads + 2×edits, up to 15
//	activity    2×(transactions in 30 days) + a third of the older ones
//	            within 90, up to 20
//	decay       2 per full week left alone past the first month, up to 35
//
// Done and dropped score 0 whatever else is true. The buckets Level cuts the
// 0–100 range into put each base inside its own level, so a fresh untouched
// note reads as the priority it was given.
const (
	engagementCap = 15
	activityCap   = 20
	decayCap      = 35
	graceDays     = 30 // a month alone before decay starts
)

var base = map[string]int{PriorityLow: 20, PriorityMedium: 40, PriorityHigh: 60, PriorityCritical: 80}

// Explain scores a note and says where every point came from.
func Explain(n Note, act Activity, now time.Time) Breakdown {
	bd := Breakdown{Base: base[n.Priority]}
	if days, ok := n.DaysToTarget(now); ok {
		switch {
		case days < 0:
			bd.Target = 30
		case days <= 7:
			bd.Target = 25
		case days <= 30:
			bd.Target = 15
		case days <= 90:
			bd.Target = 8
		case days <= 365:
			bd.Target = 3
		}
	}
	bd.Engagement = min(engagementCap, n.ReadCount+2*n.EditCount)
	bd.Activity = min(activityCap, 2*act.Tx30+(act.Tx90-act.Tx30)/3)
	if idle := idleDays(n, now); idle > graceDays {
		bd.Decay = min(decayCap, 2*((idle-graceDays)/7))
	}
	if n.Open() {
		bd.Total = max(0, min(100, bd.Base+bd.Target+bd.Engagement+bd.Activity-bd.Decay))
	}
	return bd
}

// Score is the one number a list sorts by.
func Score(n Note, act Activity, now time.Time) int { return Explain(n, act, now).Total }

// Level is a score read back as a priority word, so the effective priority is
// spelled the way the base one is.
func Level(score int) string {
	switch {
	case score >= 75:
		return PriorityCritical
	case score >= 55:
		return PriorityHigh
	case score >= 30:
		return PriorityMedium
	}
	return PriorityLow
}

// stampLayout is how SQLite writes datetime('now'): UTC, no zone.
const stampLayout = "2006-01-02 15:04:05"

// idleDays is how many whole days since the note was last read, edited or
// created — whichever is latest. A stamp that does not parse counts as now:
// a broken row should not read as forgotten.
func idleDays(n Note, now time.Time) int {
	var latest time.Time
	for _, s := range []string{n.LastReadAt, n.UpdatedAt, n.CreatedAt} {
		t, err := time.ParseInLocation(stampLayout, s, time.UTC)
		if err != nil {
			continue
		}
		if t.After(latest) {
			latest = t
		}
	}
	if latest.IsZero() {
		return 0
	}
	return int(now.Sub(latest).Hours() / 24)
}
