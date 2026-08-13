package main

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"kakei/internal/db"
	"kakei/internal/goals"
)

// runGoalsIn points KAKEI_DB at a database of this case's own, captures what
// the command writes and returns both.
//
// Only the paths that never open a form are driven from here: new, edit and the
// delete confirmation all block on a TTY, so they stay in goals.Form's territory
// and are covered through the store instead.
func runGoalsIn(t *testing.T, dbPath string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("KAKEI_DB", dbPath)

	var buf bytes.Buffer
	old := out
	out = &buf
	t.Cleanup(func() { out = old })

	err := runGoals(args)
	return buf.String(), err
}

// seedGoal puts one goal in the database at path and hands it back.
func seedGoal(t *testing.T, path string, g goals.Goal) goals.Goal {
	t.Helper()
	t.Setenv("KAKEI_DB", path)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := goals.NewStore(conn).Create(&g); err != nil {
		t.Fatal(err)
	}
	return g
}

func newLaptop() goals.Goal {
	return goals.Goal{
		Name: "New laptop", Description: "money for the new machine",
		Target: 500000, Currency: "BRL", Kind: goals.KindSaving,
	}
}

func TestGoalsHelp(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"top level -h", []string{"-h"}, "Track goals"},
		{"top level --help", []string{"--help"}, "Track goals"},
		{"new -h", []string{"new", "-h"}, "Create a goal"},
		{"n -h", []string{"n", "-h"}, "Create a goal"},
		{"edit -h", []string{"edit", "-h"}, "Edit a goal"},
		{"e --help", []string{"e", "--help"}, "Edit a goal"},
		{"delete --help", []string{"delete", "--help"}, "Delete a goal"},
		{"d -h", []string{"d", "-h"}, "Delete a goal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Help must not need a database at all — point at a path that cannot
			// be created and it should still print.
			path := filepath.Join(t.TempDir(), "nope", "unused.db")
			got, err := runGoalsIn(t, path, tc.args...)
			if err != nil {
				t.Fatalf("help returned %v", err)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("help = %q; want it to contain %q", got, tc.want)
			}
		})
	}
}

func TestGoalsList(t *testing.T) {
	t.Run("shows a card per goal, with its bar", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedGoal(t, path, newLaptop())
		other := newLaptop()
		other.Name, other.Description = "Holiday", ""
		seedGoal(t, path, other)

		got, err := runGoalsIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"New laptop", "money for the new machine", "Holiday",
			"R$5000.00", "to go", "░",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("list is missing %q:\n%s", want, got)
			}
		}
		if strings.Contains(got, "GOAL") {
			t.Errorf("list fell back to the table:\n%s", got)
		}
	})

	t.Run("--resume shows the table instead", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedGoal(t, path, newLaptop())

		got, err := runGoalsIn(t, path, "--resume")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"GOAL", "PROGRESS", "TARGET", "New laptop", "R$5000.00"} {
			if !strings.Contains(got, want) {
				t.Errorf("--resume is missing %q:\n%s", want, got)
			}
		}
		if strings.Contains(got, "░") {
			t.Errorf("--resume drew a bar, so it is not the compact view:\n%s", got)
		}
	})

	t.Run("--resume on an empty database says how to start", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		got, err := runGoalsIn(t, path, "--resume")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "kakei g n") {
			t.Errorf("--resume does not say how to make a goal:\n%s", got)
		}
	})

	t.Run("an empty database says how to start", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		got, err := runGoalsIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "kakei g n") {
			t.Errorf("list does not say how to make a goal:\n%s", got)
		}
	})
}

func TestGoalsDetails(t *testing.T) {
	t.Run("an id shows the card", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		g := seedGoal(t, path, newLaptop())

		got, err := runGoalsIn(t, path, strconv.FormatInt(g.ID, 10))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"New laptop", "R$5000.00", "to go"} {
			if !strings.Contains(got, want) {
				t.Errorf("details is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("an unknown id says which", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedGoal(t, path, newLaptop())

		_, err := runGoalsIn(t, path, "404")
		if err == nil || !strings.Contains(err.Error(), "404") {
			t.Fatalf("details of an unknown id = %v; want it to name the ref", err)
		}
	})

	t.Run("something that is not a number says goals are referenced by id", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedGoal(t, path, newLaptop())

		_, err := runGoalsIn(t, path, "LAPTOP")
		if err == nil || !strings.Contains(err.Error(), "id") {
			t.Fatalf("details of a code = %v; want it to say goals have no code", err)
		}
	})
}

func TestGoalsEditAndDeleteMissing(t *testing.T) {
	// The lookup has to fail before any form or confirmation opens, or these
	// cases would block on a TTY that is not there.
	for _, verb := range []string{"edit", "e", "delete", "d"} {
		t.Run(verb+" with an unknown id errors", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "kakei.db")
			seedGoal(t, path, newLaptop())

			if _, err := runGoalsIn(t, path, verb, "404"); err == nil {
				t.Fatalf("%s 404 = nil; want an error", verb)
			}
		})
	}
}

func TestGoalsWithoutADatabase(t *testing.T) {
	// A directory is not a database file, so opening it must fail rather than
	// panic somewhere further in.
	dir := t.TempDir()
	if _, err := runGoalsIn(t, dir); err == nil {
		t.Fatal("listing with an unopenable database was not an error")
	}
}
