package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pecunia/internal/accounts"
)

// runSummaryIn points PECUNIA_DB at a database of this case's own, captures what
// the command writes and returns both. Nothing here opens a form or a picker,
// so the whole command is drivable from a test.
func runSummaryIn(t *testing.T, dbPath string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("PECUNIA_DB", dbPath)

	var buf bytes.Buffer
	old := out
	out = &buf
	t.Cleanup(func() { out = old })

	err := runSummary(args)
	return buf.String(), err
}

func TestSummaryHelp(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		t.Run(flag+" prints the summary help", func(t *testing.T) {
			// Help must not need a database at all — point at a path that
			// cannot be created and it should still print.
			path := filepath.Join(t.TempDir(), "nope", "unused.db")
			got, err := runSummaryIn(t, path, flag)
			if err != nil {
				t.Fatalf("help returned %v", err)
			}
			if !strings.Contains(got, "Where you stand") {
				t.Errorf("help is missing its opening line:\n%s", got)
			}
			if strings.Contains(got, "flag: help requested") {
				t.Errorf("the flag package answered instead of the help text:\n%s", got)
			}
		})
	}
}

func TestSummaryScope(t *testing.T) {
	t.Run("no arguments summarises today", func(t *testing.T) {
		got, err := runSummaryIn(t, newTestDB(t))
		if err != nil {
			t.Fatal(err)
		}
		if want := time.Now().Format("Monday, 2 January 2006"); !strings.Contains(got, want) {
			t.Errorf("summary does not lead with %q:\n%s", want, got)
		}
	})

	t.Run("--date takes one day", func(t *testing.T) {
		got, err := runSummaryIn(t, newTestDB(t), "--date", "2026-08-10")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "Monday, 10 August 2026") {
			t.Errorf("summary is not the day it was asked for:\n%s", got)
		}
	})

	t.Run("--month widens the day to its month", func(t *testing.T) {
		got, err := runSummaryIn(t, newTestDB(t), "--month", "--date", "2026-07-04")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "July 2026") {
			t.Errorf("summary is not the month the day falls in:\n%s", got)
		}
	})

	t.Run("--month on its own is this month", func(t *testing.T) {
		got, err := runSummaryIn(t, newTestDB(t), "--month")
		if err != nil {
			t.Fatal(err)
		}
		if want := time.Now().Format("January 2006"); !strings.Contains(got, want) {
			t.Errorf("summary does not lead with %q:\n%s", want, got)
		}
	})

	t.Run("reads what is really in the database", func(t *testing.T) {
		path := newTestDB(t)
		seed(t, path, accounts.Account{Code: "INTER", Name: "Banco Inter", Color: "orange",
			Currency: "BRL", Balance: 482350})

		got, err := runSummaryIn(t, path, "--date", "2026-08-10")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"BALANCES", "INTER", "R$4823.50"} {
			if !strings.Contains(got, want) {
				t.Errorf("summary is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("says so when there is nothing to summarise yet", func(t *testing.T) {
		got, err := runSummaryIn(t, newTestDB(t), "--date", "2026-08-10")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "pecunia ac n") {
			t.Errorf("a fresh database is not told where to start:\n%s", got)
		}
	})
}

func TestSummaryRefuses(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"a date that is not a date", []string{"--date", "yesterday"}, "YYYY-MM-DD"},
		{"a day that never happened", []string{"--date", "2026-02-30"}, "YYYY-MM-DD"},
		{"an argument that is not a flag", []string{"today"}, "today"},
		{"a flag it does not have", []string{"--week"}, "week"},
	}
	for _, tc := range cases {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			_, err := runSummaryIn(t, newTestDB(t), tc.args...)
			if err == nil {
				t.Fatalf("%v was accepted", tc.args)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not name %q", err, tc.want)
			}
		})
	}
}
