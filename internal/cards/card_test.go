package cards

import (
	"strings"
	"testing"
	"time"
)

func TestAvailable(t *testing.T) {
	cases := []struct {
		name    string
		limit   int64
		balance int64
		want    int64
	}{
		{"part of the limit used", 500000, 123850, 376150},
		{"nothing used", 500000, 0, 500000},
		{"fully used", 500000, 500000, 0},
		{"over the limit", 500000, 600000, -100000},
		{"no limit at all", 0, 1, -1},
		{"a refund puts it over the limit", 500000, -5000, 505000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Card{Limit: tc.limit, Balance: tc.balance}
			if got := c.Available(); got != tc.want {
				t.Fatalf("Available() = %d; want %d", got, tc.want)
			}
		})
	}
}

func TestParseDay(t *testing.T) {
	good := []struct {
		in   string
		want int
	}{
		{"1", 1},
		{"15", 15},
		{"31", 31},
		{" 15 ", 15},
		{"05", 5},
	}
	for _, tc := range good {
		t.Run("accepts "+tc.in, func(t *testing.T) {
			got, err := ParseDay(tc.in)
			if err != nil || got != tc.want {
				t.Fatalf("ParseDay(%q) = %d, %v; want %d", tc.in, got, err, tc.want)
			}
		})
	}

	bad := []string{"0", "32", "-1", "abc", "", "   ", "1.5", "1 5"}
	for _, in := range bad {
		t.Run("rejects "+in, func(t *testing.T) {
			if got, err := ParseDay(in); err == nil {
				t.Fatalf("ParseDay(%q) = %d; want an error", in, got)
			}
		})
	}
}

func TestNextDate(t *testing.T) {
	date := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}

	cases := []struct {
		name string
		from string
		day  int
		want string
	}{
		{"later this month", "2026-08-06", 15, "2026-08-15"},
		{"today counts as next", "2026-08-06", 6, "2026-08-06"},
		{"already passed, so next month", "2026-08-06", 1, "2026-09-01"},
		{"the 31st of a 31-day month", "2026-08-06", 31, "2026-08-31"},
		{"the 31st clamps in a short month", "2026-09-06", 31, "2026-09-30"},
		{"the 31st clamps to february", "2026-02-06", 31, "2026-02-28"},
		{"february in a leap year", "2028-02-06", 30, "2028-02-29"},
		{"december rolls into january", "2026-12-20", 5, "2027-01-05"},
		// Rolling forward from a 31st must not overshoot: naive month addition
		// turns 31 Aug into 1 Oct and 31 Jan into 3 Mar.
		{"rolls forward from a 31st", "2026-08-31", 15, "2026-09-15"},
		{"rolls forward from january 31st", "2026-01-31", 5, "2026-02-05"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NextDate(date(tc.from), tc.day)
			if got.Format("2006-01-02") != tc.want {
				t.Fatalf("NextDate(%s, %d) = %s; want %s",
					tc.from, tc.day, got.Format("2006-01-02"), tc.want)
			}
		})
	}
}

func TestFmt(t *testing.T) {
	cases := []struct {
		name     string
		currency string
		v        int64
		want     string
	}{
		{"real", "BRL", 376150, "R$3761.50"},
		{"dollar", "USD", 5, "$0.05"},
		{"negative", "BRL", -100000, "R$-1000.00"},
		{"bitcoin keeps eight places", "BTC", 1, "₿0.00000001"},
		{"unknown currency falls back", "XXX", 100, "$1.00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Card{Currency: tc.currency}
			if got := c.Fmt(tc.v); got != tc.want {
				t.Fatalf("Fmt(%d) = %q; want %q", tc.v, got, tc.want)
			}
		})
	}
}

func TestCardAccessors(t *testing.T) {
	t.Run("reads its own color and currency", func(t *testing.T) {
		c := Card{Color: "teal", Currency: "BTC"}
		if c.Col().Name != "teal" || c.Cur().Code != "BTC" {
			t.Fatalf("accessors gave %s / %s", c.Col().Name, c.Cur().Code)
		}
	})

	t.Run("a hand-edited row falls back instead of crashing", func(t *testing.T) {
		c := Card{Color: "puce", Currency: "XXX"}
		if c.Col().Hex == "" || c.Cur().Code == "" {
			t.Fatalf("unknown color/currency gave %+v / %+v", c.Col(), c.Cur())
		}
	})
}

func TestValidateBalance(t *testing.T) {
	cases := []struct {
		name    string
		limit   int64
		balance int64
		allowed bool
		wantErr bool
	}{
		{"under the limit", 500000, 123850, false, false},
		{"exactly at the limit", 500000, 500000, false, false},
		{"a penny over, not allowed", 500000, 500001, false, true},
		{"far over, not allowed", 300000, 412000, false, true},
		{"far over, allowed", 300000, 412000, true, false},
		{"a refund is never over", 500000, -5000, false, false},
		{"no limit and nothing used", 0, 0, false, false},
		{"no limit but something used", 0, 1, false, true},
		{"no limit, allowed over", 0, 1, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Card{Currency: "BRL", Limit: tc.limit, Balance: tc.balance, OverLimitAllowed: tc.allowed}
			err := c.ValidateBalance()
			if tc.wantErr && err == nil {
				t.Fatalf("balance %d against limit %d (allowed=%v) was accepted",
					tc.balance, tc.limit, tc.allowed)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("balance %d against limit %d (allowed=%v) = %v",
					tc.balance, tc.limit, tc.allowed, err)
			}
		})
	}

	t.Run("the error names both amounts and the way out", func(t *testing.T) {
		err := Card{Currency: "BRL", Limit: 300000, Balance: 412000}.ValidateBalance()
		for _, want := range []string{"R$4120.00", "R$3000.00", "allow"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		}
	})
}

func TestAddMonths(t *testing.T) {
	date := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}

	cases := []struct {
		name string
		from string
		n    int
		want string
	}{
		{"the same day next month", "2026-08-14", 1, "2026-09-14"},
		{"nowhere at all", "2026-08-14", 0, "2026-08-14"},
		{"across the year", "2026-11-14", 3, "2027-02-14"},
		// AddDate alone turns 31 January into 3 March.
		{"the 31st into february", "2026-01-31", 1, "2026-02-28"},
		{"the 31st into a leap february", "2028-01-31", 1, "2028-02-29"},
		{"the 31st into a 30-day month", "2026-08-31", 1, "2026-09-30"},
		// The clamp is per step from the original day, not cumulative: five
		// months on from the 31st is the 31st again, not the 28th.
		{"the 31st five months on", "2026-01-31", 5, "2026-06-30"},
		{"the 31st twelve months on", "2026-01-31", 12, "2027-01-31"},
		{"backwards", "2026-08-14", -2, "2026-06-14"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AddMonths(date(tc.from), tc.n)
			if got.Format("2006-01-02") != tc.want {
				t.Fatalf("AddMonths(%s, %d) = %s; want %s",
					tc.from, tc.n, got.Format("2006-01-02"), tc.want)
			}
		})
	}
}
