package logs

import (
	"strings"
	"testing"
)

func TestTable(t *testing.T) {
	t.Run("nothing renders as nothing", func(t *testing.T) {
		if got := Table(nil); got != "" {
			t.Fatalf("Table(nil) = %q; want empty", got)
		}
	})

	t.Run("a row carries the whole entry", func(t *testing.T) {
		got := Table([]Entry{{
			ID: 1, Source: User, Action: "edited", Entity: "account", EntityID: 7,
			Changes:   `{"name":{"old":"Cash","new":"Wallet"},"balance":{"old":80000,"new":95000}}`,
			CreatedAt: "2026-08-27 09:12:00",
		}})
		for _, want := range []string{
			"user", "edited", "account", "7", "2026-08-27",
			"balance: 80000 → 95000", "name: Cash → Wallet",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("table is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("a create has an empty changes cell", func(t *testing.T) {
		got := Table([]Entry{{
			ID: 1, Source: System, Action: "created", Entity: "card_bill", EntityID: 3,
			CreatedAt: "2026-08-27 09:12:00",
		}})
		if !strings.Contains(got, "card_bill") || strings.Contains(got, "→") {
			t.Fatalf("want a bare created row with no arrow:\n%s", got)
		}
	})
}
