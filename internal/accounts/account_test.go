package accounts

import "testing"

func TestCurrencyByCode(t *testing.T) {
	cases := []struct {
		in       string
		wantCode string
		wantExp  int
	}{
		{"USD", "USD", 2},
		{"BTC", "BTC", 8},
		{"BRL", "BRL", 2},
		{"XXX", "USD", 2}, // unknown falls back to the first currency
		{"", "USD", 2},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := CurrencyByCode(tc.in)
			if got.Code != tc.wantCode || got.Exp != tc.wantExp {
				t.Fatalf("CurrencyByCode(%q) = %s/%d; want %s/%d",
					tc.in, got.Code, got.Exp, tc.wantCode, tc.wantExp)
			}
		})
	}
}

func TestColorByName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"green", "green"},
		{"pink", "pink"},
		{"chartreuse", "red"}, // unknown falls back to the first color
		{"", "red"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := ColorByName(tc.in); got.Name != tc.want {
				t.Fatalf("ColorByName(%q) = %q; want %q", tc.in, got.Name, tc.want)
			}
			if got := ColorByName(tc.in); got.Hex == "" {
				t.Fatalf("ColorByName(%q) has no hex", tc.in)
			}
		})
	}
}

func TestParseAmountAccepts(t *testing.T) {
	usd := CurrencyByCode("USD")
	btc := CurrencyByCode("BTC")

	cases := []struct {
		name string
		in   string
		c    Currency
		want int64
	}{
		{"two places", "1.50", usd, 150},
		{"comma separator", "1,50", usd, 150},
		{"one place", "1.5", usd, 150},
		{"no separator", "1", usd, 100},
		{"zero", "0", usd, 0},
		{"empty is zero", "", usd, 0},
		{"blank is zero", "   ", usd, 0},
		{"negative", "-12.34", usd, -1234},
		{"explicit plus", "+12.34", usd, 1234},
		{"leading separator", ".5", usd, 50},
		{"one satoshi", "0.00000001", btc, 1},
		{"btc scale", "1.5", btc, 150000000},
		{"whole supply", "21000000", btc, 2100000000000000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAmount(tc.in, tc.c)
			if err != nil || got != tc.want {
				t.Fatalf("ParseAmount(%q, %s) = %d, %v; want %d",
					tc.in, tc.c.Code, got, err, tc.want)
			}
		})
	}
}

func TestParseAmountRejects(t *testing.T) {
	usd := CurrencyByCode("USD")
	btc := CurrencyByCode("BTC")

	cases := []struct {
		name string
		in   string
		c    Currency
	}{
		{"too precise for fiat", "1.234", usd},
		{"nine places for btc", "0.000000001", btc},
		{"not a number", "abc", usd},
		{"two separators", "1.2.3", usd},
		{"space as group separator", "1 000", usd},
		{"overflows int64", "99999999999999999999", usd},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := ParseAmount(tc.in, tc.c); err == nil {
				t.Fatalf("ParseAmount(%q, %s) = %d; want an error", tc.in, tc.c.Code, got)
			}
		})
	}
}

func TestFormatAmount(t *testing.T) {
	usd := CurrencyByCode("USD")
	btc := CurrencyByCode("BTC")

	cases := []struct {
		name string
		v    int64
		c    Currency
		want string
	}{
		{"dollars and cents", 150, usd, "1.50"},
		{"zero", 0, usd, "0.00"},
		{"pads to two places", 5, usd, "0.05"},
		{"negative", -1234, usd, "-12.34"},
		{"one satoshi", 1, btc, "0.00000001"},
		{"btc scale", 150000000, btc, "1.50000000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatAmount(tc.v, tc.c); got != tc.want {
				t.Fatalf("FormatAmount(%d, %s) = %q; want %q", tc.v, tc.c.Code, got, tc.want)
			}
		})
	}

	t.Run("min int64 does not wrap", func(t *testing.T) {
		// abs() goes through uint64 for exactly this input.
		if got := FormatAmount(-9223372036854775808, usd); got != "-92233720368547758.08" {
			t.Fatalf("FormatAmount(MinInt64, USD) = %q", got)
		}
	})
}

func TestAmountRoundTrip(t *testing.T) {
	for _, c := range []Currency{CurrencyByCode("USD"), CurrencyByCode("BTC")} {
		for _, v := range []int64{0, 1, 999, -4321, 2100000000000000} {
			t.Run(c.Code, func(t *testing.T) {
				back, err := ParseAmount(FormatAmount(v, c), c)
				if err != nil || back != v {
					t.Fatalf("%s round trip of %d gave %d, %v", c.Code, v, back, err)
				}
			})
		}
	}
}

func TestNormalizeCode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"wllt2", "WLLT2"},
		{" WLLT2 ", "WLLT2"},
		{"\twllt2\n", "WLLT2"},
		{"WLLT2", "WLLT2"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := NormalizeCode(tc.in); got != tc.want {
				t.Fatalf("NormalizeCode(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestValidateCode(t *testing.T) {
	// A code the user types is any five letters or digits. The reduced
	// codeAlphabet exists to keep *generated* codes unambiguous on screen — it
	// is not a restriction on what someone may name their own account.
	good := []struct{ name, in string }{
		{"normalizes before checking", " wllt2 "},
		{"a real word with an I", "INTER"},
		{"a real word with an O", "NUBON"},
		{"digit zero", "ABCD0"},
		{"digit one", "ABCD1"},
		{"all ambiguous characters", "IO01I"},
		{"all digits", "12345"},
	}
	for _, tc := range good {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateCode(tc.in); err != nil {
				t.Fatalf("ValidateCode(%q) = %v; want it accepted", tc.in, err)
			}
		})
	}

	bad := []struct{ name, in string }{
		{"empty", ""},
		{"too short", "ABCD"},
		{"too long", "ABCDEF"},
		{"punctuation", "AB-CD"},
		{"inner space", "AB CD"},
		{"symbol", "ABCD!"},
		{"accented letter", "ÁBCDE"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateCode(tc.in); err == nil {
				t.Fatalf("ValidateCode(%q) = nil; want an error", tc.in)
			}
		})
	}
}

func TestRandomCode(t *testing.T) {
	t.Run("always passes its own validator", func(t *testing.T) {
		for range 200 {
			if err := ValidateCode(RandomCode()); err != nil {
				t.Fatalf("RandomCode produced an invalid code: %v", err)
			}
		}
	})

	t.Run("does not repeat itself", func(t *testing.T) {
		seen := map[string]bool{}
		for range 50 {
			seen[RandomCode()] = true
		}
		if len(seen) < 45 {
			t.Fatalf("only %d distinct codes out of 50 — not random enough", len(seen))
		}
	})
}

func TestAccountAccessors(t *testing.T) {
	a := Account{Color: "teal", Currency: "BTC", Balance: 150000000}
	if a.Cur().Code != "BTC" || a.Col().Name != "teal" || a.Amount() != "1.50000000" {
		t.Fatalf("accessors gave %s / %s / %s", a.Cur().Code, a.Col().Name, a.Amount())
	}
}
