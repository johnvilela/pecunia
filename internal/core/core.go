// Package core holds what every pecunia module shares: the currencies and the
// scale their amounts are stored at, the color palette, the 5-character codes
// and the picker/confirm widgets. It knows nothing about accounts or cards.
package core

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Currency describes how an amount is stored and shown. Exp is the number of
// decimal places, which is also the power of ten between the stored integer
// (minor units) and the displayed amount: cents for fiat, satoshis for Bitcoin.
type Currency struct {
	Code   string
	Label  string
	Symbol string
	Exp    int
}

var Currencies = []Currency{
	{Code: "USD", Label: "Dollar", Symbol: "$", Exp: 2},
	{Code: "EUR", Label: "Euro", Symbol: "€", Exp: 2},
	{Code: "BRL", Label: "Brazilian Real", Symbol: "R$", Exp: 2},
	{Code: "BTC", Label: "Bitcoin", Symbol: "₿", Exp: 8},
}

// CurrencyByCode falls back to the first currency so a hand-edited row can
// still be listed instead of crashing the command.
func CurrencyByCode(code string) Currency {
	for _, c := range Currencies {
		if c.Code == code {
			return c
		}
	}
	return Currencies[0]
}

// Color is one of the twelve presets. Rows store the name, so changing a hex
// value re-themes everything instead of breaking it.
type Color struct {
	Name string
	Hex  string
}

var Palette = []Color{
	{"red", "#E5484D"},
	{"orange", "#F76B15"},
	{"amber", "#FFB224"},
	{"yellow", "#F5D90A"},
	{"lime", "#99D52A"},
	{"green", "#30A46C"},
	{"teal", "#12A594"},
	{"cyan", "#05A2C2"},
	{"blue", "#0091FF"},
	{"indigo", "#3E63DD"},
	{"violet", "#8E4EC6"},
	{"pink", "#E93D82"},
}

func ColorByName(name string) Color {
	for _, c := range Palette {
		if c.Name == name {
			return c
		}
	}
	return Palette[0]
}

const CodeLen = 5

// codeAlphabet drops O/0 and I/1 so a *generated* code read off a screen can be
// typed back. It does not constrain a code the user picks: someone naming their
// account INTER already knows how to type it.
const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// RandomCode returns a candidate code; uniqueness is the store's job.
func RandomCode() string {
	b := make([]byte, CodeLen)
	rand.Read(b) // crypto/rand.Read never returns an error
	out := make([]byte, CodeLen)
	for i, v := range b {
		out[i] = codeAlphabet[int(v)%len(codeAlphabet)]
	}
	return string(out)
}

// SuggestCode returns a code no row is using yet, for a form to pre-fill.
// taken is the store's own lookup — the alternative was a copy of this loop in
// every store.
func SuggestCode(taken func(string) (bool, error)) (string, error) {
	for range 20 {
		code := RandomCode()
		used, err := taken(code)
		if err != nil {
			return "", err
		}
		if !used {
			return code, nil
		}
	}
	return "", errors.New("could not find a free code")
}

// NormalizeCode uppercases and trims so "wllt" and "WLLT " are the same code.
func NormalizeCode(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

func ValidateCode(s string) error {
	s = NormalizeCode(s)
	if len(s) != CodeLen {
		return fmt.Errorf("code must be exactly %d characters", CodeLen)
	}
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return errors.New("code may only use letters and digits")
		}
	}
	return nil
}

// ValidateName is the guard on the one field nothing can default. It lives
// here because the stores and the forms have to agree on it: huh returns
// without running its validators when stdin ends mid-form, so the form alone
// is not enough to keep a nameless row out of the database.
func ValidateName(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("name is required")
	}
	return nil
}

// CodeErr turns the UNIQUE constraint into something readable.
func CodeErr(err error, code string) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("code %s is already in use", code)
	}
	return err
}

// FKErr turns a foreign key violation into something readable. SQLite words
// every one of them the same way — "FOREIGN KEY constraint failed" and a
// number — so only the caller knows what was still holding on.
func FKErr(err error, msg string) error {
	if err != nil && strings.Contains(err.Error(), "FOREIGN KEY") {
		return errors.New(msg)
	}
	return err
}

// ParseAmount converts a decimal string into minor units for c. It works on the
// digits as text — no float ever touches an amount — so BTC keeps all eight
// places and fiat cents stay exact. A comma is accepted as the decimal
// separator, which is what a Brazilian keyboard types.
func ParseAmount(s string, c Currency) (int64, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	if s == "" {
		return 0, nil
	}

	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(strings.TrimPrefix(s, "-"), "+")

	whole, frac, hasFrac := strings.Cut(s, ".")
	if hasFrac && strings.Contains(frac, ".") {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	if whole == "" {
		whole = "0"
	}
	if len(frac) > c.Exp {
		return 0, fmt.Errorf("%s allows at most %d decimal places", c.Code, c.Exp)
	}
	if !isDigits(whole) || !isDigits(frac) {
		return 0, fmt.Errorf("invalid amount %q", s)
	}

	v, err := strconv.ParseInt(whole+frac+strings.Repeat("0", c.Exp-len(frac)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("amount out of range")
	}
	if neg {
		v = -v
	}
	return v, nil
}

// FormatAmount is ParseAmount's inverse: minor units back to a decimal string
// with exactly c.Exp places.
func FormatAmount(v int64, c Currency) string {
	sign := ""
	if v < 0 {
		sign = "-"
	}
	digits := strconv.FormatUint(abs(v), 10)
	if c.Exp == 0 {
		return sign + digits
	}
	for len(digits) <= c.Exp {
		digits = "0" + digits
	}
	cut := len(digits) - c.Exp
	return sign + digits[:cut] + "." + digits[cut:]
}

// abs goes through uint64 so math.MinInt64 does not wrap back to itself.
func abs(v int64) uint64 {
	if v < 0 {
		return uint64(-(v + 1)) + 1
	}
	return uint64(v)
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
