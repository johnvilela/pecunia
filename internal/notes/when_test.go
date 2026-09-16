package notes

import (
	"strings"
	"testing"
	"time"
)

func TestParseWhen(t *testing.T) {
	// The last day of January: the one day that shows every clamp.
	now := time.Date(2026, 1, 31, 15, 0, 0, 0, time.Local)
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"2026-12-16", "2026-12-16"},
		{"16/12/2026", "2026-12-16"},
		{"2026-02", "2026-02-28"},
		{"2026-12", "2026-12-31"},
		{"today", "2026-01-31"},
		{"tomorrow", "2026-02-01"},
		{"next week", "2026-02-07"},
		{"next month", "2026-02-28"},
		{"next year", "2027-01-31"},
		{"in 3 months", "2026-04-30"},
		{"in 1 day", "2026-02-01"},
		{"in 2 weeks", "2026-02-14"},
		{"in 1 year", "2027-01-31"},
		{"2 weeks", "2026-02-14"},
		{"6 months", "2026-07-31"},
		{"  In 3   Months ", "2026-04-30"},
		{"TODAY", "2026-01-31"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseWhen(tc.in, now)
			if err != nil {
				t.Fatalf("ParseWhen(%q) = %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParseWhen(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}

	for _, in := range []string{"soon", "in 0 days", "in -2 days", "32/01/2026", "2026-13", "next decade", "in three months", "2026-02-30"} {
		t.Run("refuses "+in, func(t *testing.T) {
			_, err := ParseWhen(in, now)
			if err == nil {
				t.Fatalf("ParseWhen(%q) succeeded; want an error", in)
			}
			if !strings.Contains(err.Error(), "in N days") {
				t.Fatalf("ParseWhen(%q) = %v; want it to list the accepted forms", in, err)
			}
		})
	}
}
