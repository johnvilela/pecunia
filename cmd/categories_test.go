package main

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"kakei/internal/categories"
	"kakei/internal/db"
)

// runCategoriesIn points KAKEI_DB at a database of this case's own, captures
// what the command writes and returns both.
//
// Only the paths that never open a form are driven from here: new, edit and the
// delete confirmation all block on a TTY, so they stay in categories.Form's
// territory and are covered through the store instead.
func runCategoriesIn(t *testing.T, dbPath string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("KAKEI_DB", dbPath)

	var buf bytes.Buffer
	old := out
	out = &buf
	t.Cleanup(func() { out = old })

	err := runCategories(args)
	return buf.String(), err
}

// seedCategory puts one category in the database at path and hands it back.
func seedCategory(t *testing.T, path string, c categories.Category) categories.Category {
	t.Helper()
	t.Setenv("KAKEI_DB", path)
	conn, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := categories.NewStore(conn).Create(&c); err != nil {
		t.Fatal(err)
	}
	return c
}

func groceries() categories.Category {
	return categories.Category{
		Code: "FOOD1", Name: "Food & Groceries", Description: "supermarket runs", Color: "lime",
	}
}

func TestCategoriesHelp(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"top level -h", []string{"-h"}, "Manage categories"},
		{"top level --help", []string{"--help"}, "Manage categories"},
		{"new -h", []string{"new", "-h"}, "Create a category"},
		{"n -h", []string{"n", "-h"}, "Create a category"},
		{"edit -h", []string{"edit", "-h"}, "Edit a category"},
		{"e --help", []string{"e", "--help"}, "Edit a category"},
		{"delete --help", []string{"delete", "--help"}, "Delete a category"},
		{"d -h", []string{"d", "-h"}, "Delete a category"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Help must not need a database at all — point at a path that
			// cannot be created and it should still print.
			got, err := runCategoriesIn(t, filepath.Join(t.TempDir(), "nope", "unused.db"), tc.args...)
			if err != nil {
				t.Fatalf("kakei ct %v = %v", tc.args, err)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("kakei ct %v printed %q; want it to contain %q", tc.args, got, tc.want)
			}
		})
	}
}

func TestCategoriesList(t *testing.T) {
	t.Run("shows the table", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedCategory(t, path, groceries())
		seedCategory(t, path, categories.Category{Code: "WORK1", Name: "Work", Color: "indigo"})

		got, err := runCategoriesIn(t, path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"CATEGORY", "DESCRIPTION", "FOOD1", "Food & Groceries", "supermarket runs", "WORK1"} {
			if !strings.Contains(got, want) {
				t.Fatalf("list is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("an empty database says how to start", func(t *testing.T) {
		got, err := runCategoriesIn(t, filepath.Join(t.TempDir(), "kakei.db"))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"no categories yet", "kakei setup", "kakei ct n"} {
			if !strings.Contains(got, want) {
				t.Fatalf("empty list = %q; want it to mention %q", got, want)
			}
		}
	})
}

func TestCategoriesDetails(t *testing.T) {
	t.Run("resolves a code in any case, and an id", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		c := seedCategory(t, path, groceries())

		for _, ref := range []string{"FOOD1", "food1", strconv.FormatInt(c.ID, 10)} {
			got, err := runCategoriesIn(t, path, ref)
			if err != nil {
				t.Fatalf("kakei ct %s = %v", ref, err)
			}
			if !strings.Contains(got, "Food & Groceries") {
				t.Fatalf("kakei ct %s printed %q", ref, got)
			}
		}
	})

	t.Run("an unknown reference names what was asked for", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kakei.db")
		seedCategory(t, path, groceries())

		_, err := runCategoriesIn(t, path, "NOPE1")
		if err == nil || !strings.Contains(err.Error(), `no category matching "NOPE1"`) {
			t.Fatalf("kakei ct NOPE1 = %v; want it to name the reference", err)
		}
	})
}

// Edit and delete open a form or a confirm prompt, but the lookup happens
// first — so the failing-lookup path is reachable without a TTY.
func TestCategoriesEditAndDeleteMissing(t *testing.T) {
	for _, sub := range []string{"edit", "e", "delete", "d"} {
		t.Run(sub+" on an unknown reference", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "kakei.db")
			seedCategory(t, path, groceries())

			_, err := runCategoriesIn(t, path, sub, "NOPE1")
			if err == nil || !strings.Contains(err.Error(), `no category matching "NOPE1"`) {
				t.Fatalf("kakei ct %s NOPE1 = %v", sub, err)
			}
		})
	}
}

func TestCategoriesWithoutADatabase(t *testing.T) {
	t.Run("reports the error instead of panicking", func(t *testing.T) {
		// A directory where the file should be: Open cannot create it.
		dir := t.TempDir()
		if _, err := runCategoriesIn(t, dir); err == nil {
			t.Fatal("kakei ct on an unopenable database = nil; want an error")
		}
	})
}

func TestCategoryDeleteNote(t *testing.T) {
	cases := []struct {
		name    string
		budgets int
		want    string
	}{
		{"nothing caps it", 0, "cannot be undone"},
		{"one budget goes with it", 1, "1 budget"},
		{"several go with it", 3, "3 budget"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := categoryDeleteNote(tc.budgets)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("note = %q; want it to mention %q", got, tc.want)
			}
		})
	}

	t.Run("a category nothing caps says nothing about budgets", func(t *testing.T) {
		if strings.Contains(categoryDeleteNote(0), "budget") {
			t.Fatalf("note = %q; want no mention of budgets", categoryDeleteNote(0))
		}
	})
}
