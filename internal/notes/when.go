package notes

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pecunia/internal/cards"
	"pecunia/internal/transactions"
)

// whenForms is the grammar a target accepts, spelled out once for the error.
const whenForms = "YYYY-MM-DD, DD/MM/YYYY, YYYY-MM, today, tomorrow, next week|month|year, or in N days|weeks|months|years"

var relative = regexp.MustCompile(`^(?:in )?(\d+) (day|week|month|year)s?$`)

// ParseWhen reads a target the way the owner writes one — a day, a month, or a
// phrase — and hands back the day it means as YYYY-MM-DD. Empty means no
// target. A phrase is resolved against now once, when the file is saved; the
// file then carries the day, with the phrase kept beside it as a comment, so
// "in 3 months" never drifts forward on the next edit.
//
// A month (2026-12) is its last day: the owner said "by December", and the
// first of December is not that. Month arithmetic clamps the way installments
// do — 31 January plus one month is 28 February — through cards.AddMonths.
func ParseWhen(s string, now time.Time) (string, error) {
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	if s == "" {
		return "", nil
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	day := func(t time.Time) (string, error) { return t.Format(transactions.DateLayout), nil }

	switch s {
	case "today":
		return day(today)
	case "tomorrow":
		return day(today.AddDate(0, 0, 1))
	case "next week":
		return day(today.AddDate(0, 0, 7))
	case "next month":
		return day(cards.AddMonths(today, 1))
	case "next year":
		return day(cards.AddMonths(today, 12))
	}

	if m := relative.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 {
			return "", whenErr(s)
		}
		switch m[2] {
		case "day":
			return day(today.AddDate(0, 0, n))
		case "week":
			return day(today.AddDate(0, 0, 7*n))
		case "month":
			return day(cards.AddMonths(today, n))
		default:
			return day(cards.AddMonths(today, 12*n))
		}
	}

	if iso, err := transactions.ParseDate(s); err == nil {
		return iso, nil
	}
	if d, err := time.ParseInLocation("02/01/2006", s, now.Location()); err == nil {
		return day(d)
	}
	if m, err := time.ParseInLocation("2006-01", s, now.Location()); err == nil {
		// Day 0 of the next month is the last day of this one.
		return day(time.Date(m.Year(), m.Month()+1, 0, 0, 0, 0, 0, now.Location()))
	}
	return "", whenErr(s)
}

func whenErr(s string) error {
	return fmt.Errorf("cannot read %q as a target — try %s", s, whenForms)
}
