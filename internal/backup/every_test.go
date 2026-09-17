package backup

import (
	"strings"
	"testing"
)

func TestParseEvery(t *testing.T) {
	cases := []struct {
		in       string
		calendar string
		cron     string
	}{
		{"1/day", "*-*-* 03:00:00", "0 3 * * *"},
		{"daily", "*-*-* 03:00:00", "0 3 * * *"},
		{"2/day", "*-*-* 03,15:00:00", "0 3,15 * * *"},
		{"3/day", "*-*-* 03,11,19:00:00", "0 3,11,19 * * *"},
		{"4/day", "*-*-* 03,09,15,21:00:00", "0 3,9,15,21 * * *"},
		{"1/week", "Mon *-*-* 03:00:00", "0 3 * * 1"},
		{"weekly", "Mon *-*-* 03:00:00", "0 3 * * 1"},
		{"2/week", "Mon,Thu *-*-* 03:00:00", "0 3 * * 1,4"},
		{"3/week", "Mon,Wed,Fri *-*-* 03:00:00", "0 3 * * 1,3,5"},
		{"7/week", "Mon,Tue,Wed,Thu,Fri,Sat,Sun *-*-* 03:00:00", "0 3 * * 1,2,3,4,5,6,0"},
		{" 2 / Day ", "*-*-* 03,15:00:00", "0 3,15 * * *"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			e, err := ParseEvery(c.in)
			if err != nil {
				t.Fatal(err)
			}
			if got := e.OnCalendar(); got != c.calendar {
				t.Errorf("OnCalendar = %q, want %q", got, c.calendar)
			}
			if got := e.Cron(); got != c.cron {
				t.Errorf("Cron = %q, want %q", got, c.cron)
			}
		})
	}

	t.Run("round-trips through String", func(t *testing.T) {
		for _, in := range []string{"1/day", "2/day", "3/week"} {
			e, err := ParseEvery(in)
			if err != nil {
				t.Fatal(err)
			}
			if e.String() != in {
				t.Errorf("String() = %q, want %q", e.String(), in)
			}
		}
		e, _ := ParseEvery("daily")
		if e.String() != "1/day" {
			t.Errorf("daily String() = %q, want 1/day", e.String())
		}
	})

	bad := []struct{ in, want string }{
		{"", "empty"},
		{"0/day", "at least 1"},
		{"25/day", "at most 24"},
		{"8/week", "at most 7"},
		{"2/month", "day or week"},
		{"two/day", "number"},
		{"2", "N/day or N/week"},
	}
	for _, c := range bad {
		t.Run("rejects "+c.in, func(t *testing.T) {
			_, err := ParseEvery(c.in)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want it to mention %q", err, c.want)
			}
		})
	}
}
