package categories

import (
	"strings"
	"testing"
)

// Every assertion here is a substring check: lipgloss wraps its output in ANSI
// escapes that change with the terminal profile, but the text does not.

func TestLabel(t *testing.T) {
	t.Run("carries the code and the name", func(t *testing.T) {
		got := Label(home())
		for _, want := range []string{"HOME1", "Home", "[", "]"} {
			if !strings.Contains(got, want) {
				t.Fatalf("Label = %q; want it to contain %q", got, want)
			}
		}
	})
}

func TestTable(t *testing.T) {
	rows := []Category{
		home(),
		{Code: "HOBBY", Name: "Hobbies", Color: "yellow"},
	}

	t.Run("names both columns", func(t *testing.T) {
		got := Table(rows)
		for _, want := range []string{"CATEGORY", "DESCRIPTION"} {
			if !strings.Contains(got, want) {
				t.Fatalf("Table = %q; want the %s header", got, want)
			}
		}
	})

	t.Run("shows every row", func(t *testing.T) {
		got := Table(rows)
		for _, want := range []string{"HOME1", "Home", "rent and repairs", "HOBBY", "Hobbies"} {
			if !strings.Contains(got, want) {
				t.Fatalf("Table is missing %q", want)
			}
		}
	})

	t.Run("has no money column", func(t *testing.T) {
		got := Table(rows)
		for _, unwanted := range []string{"BALANCE", "LIMIT", "$"} {
			if strings.Contains(got, unwanted) {
				t.Fatalf("Table contains %q; a category holds no money", unwanted)
			}
		}
	})

	t.Run("an empty list still renders the headers", func(t *testing.T) {
		if got := Table(nil); !strings.Contains(got, "CATEGORY") {
			t.Fatalf("Table(nil) = %q", got)
		}
	})
}

func TestDetails(t *testing.T) {
	t.Run("shows every value and no field names", func(t *testing.T) {
		c := home()
		c.CreatedAt = "2026-08-08 12:00:00"
		c.UpdatedAt = "2026-08-08 13:00:00"
		got := Details(c)

		for _, want := range []string{"HOME1", "Home", "rent and repairs",
			createdIcon + " 2026-08-08 12:00:00", updatedIcon + " 2026-08-08 13:00:00", "╭", "╯"} {
			if !strings.Contains(got, want) {
				t.Fatalf("Details is missing %q:\n%s", want, got)
			}
		}
		for _, unwanted := range []string{"Code", "Name", "Description", "Color"} {
			if strings.Contains(got, unwanted) {
				t.Fatalf("Details names the field %q; the card shows values only", unwanted)
			}
		}
	})

	t.Run("drops an empty description", func(t *testing.T) {
		c := home()
		with := strings.Count(Details(c), "\n")
		c.Description = ""
		if without := strings.Count(Details(c), "\n"); without != with-1 {
			t.Fatalf("card is %d lines without a description; want one fewer than %d", without, with)
		}
	})

	t.Run("drops the footer when there are no timestamps", func(t *testing.T) {
		if got := Details(home()); strings.Contains(got, createdIcon) {
			t.Fatalf("Details = %q; want no footer on an unsaved category", got)
		}
	})

	t.Run("renders a hand-edited color instead of crashing", func(t *testing.T) {
		c := home()
		c.Color = "puce"
		if got := Details(c); !strings.Contains(got, "HOME1") {
			t.Fatalf("Details = %q", got)
		}
	})
}

func TestPickerRow(t *testing.T) {
	t.Run("filters on the code and the name", func(t *testing.T) {
		got := pickerRow(home())
		if got.Filter != "HOME1 Home" {
			t.Fatalf("Filter = %q; want %q", got.Filter, "HOME1 Home")
		}
		if got.Desc != "rent and repairs" {
			t.Fatalf("Desc = %q", got.Desc)
		}
		if !strings.Contains(got.Label, "HOME1") {
			t.Fatalf("Label = %q", got.Label)
		}
	})
}
