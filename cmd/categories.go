package main

import (
	"database/sql"
	"errors"
	"fmt"

	"kakei/internal/categories"
	"kakei/internal/core"
)

const categoriesHelp = `Manage categories.

Usage:
  kakei category [command] [CODE|ID]
  kakei ct       [command] [CODE|ID]

Commands:
  (none)              list your categories
  new     | n         create a category
  edit    | e  [ref]  edit a category
  delete  | d  [ref]  delete a category
  CODE|ID             show one category in detail

A new database starts with a set of everyday categories; edit or delete them
freely. Leaving [ref] out opens a picker. Add -h to any command for its own
help.
`

var categorySubHelp = map[string]string{
	"new": `Create a category.

Usage:
  kakei category new
  kakei ct n

Opens a form: name, description (optional), code, color. The code is 5
characters and comes pre-filled with a free suggestion. Colors are a preset of
12, and are what makes a category recognisable at a glance in a list.
`,
	"edit": `Edit a category.

Usage:
  kakei category edit [CODE|ID]
  kakei ct e [CODE|ID]

Opens the create form pre-filled. Without CODE|ID, pick from a list first.
`,
	"delete": `Delete a category for good.

Usage:
  kakei category delete [CODE|ID]
  kakei ct d [CODE|ID]

Asks for confirmation. Without CODE|ID, pick from a list first. A category the
starter set put there is not restored once deleted.
`,
}

var errNoCategories = errors.New("no categories yet — run: kakei setup, or create one with: kakei ct n")

func runCategories(args []string) error {
	if len(args) == 0 {
		return listCategories()
	}
	sub, rest := args[0], args[1:]
	if isHelpFlag(sub) {
		fmt.Fprint(out, categoriesHelp)
		return nil
	}

	name := map[string]string{
		"new": "new", "n": "new",
		"edit": "edit", "e": "edit",
		"delete": "delete", "d": "delete",
	}[sub]

	if name == "" {
		// Anything else is a {CODE|ID}.
		return withCategories(func(s *categories.Store) error {
			c, err := resolveOrPickCategory(s, args, "Category details")
			if err != nil {
				return err
			}
			fmt.Fprint(out, categories.Details(c))
			return nil
		})
	}
	if len(rest) > 0 && isHelpFlag(rest[0]) {
		fmt.Fprint(out, categorySubHelp[name])
		return nil
	}

	return withCategories(func(s *categories.Store) error {
		switch name {
		case "new":
			return createCategory(s)
		case "edit":
			return editCategory(s, rest)
		default:
			return deleteCategory(s, rest)
		}
	})
}

func withCategories(fn func(*categories.Store) error) error {
	return withConn(func(conn *sql.DB) error { return fn(categories.NewStore(conn)) })
}

// resolveOrPickCategory turns an optional {CODE|ID} argument into a category,
// falling back to the picker when none was given.
func resolveOrPickCategory(s *categories.Store, args []string, title string) (categories.Category, error) {
	if len(args) > 0 && args[0] != "" {
		c, err := s.Resolve(args[0])
		if errors.Is(err, categories.ErrNotFound) {
			return c, fmt.Errorf("no category matching %q", args[0])
		}
		return c, err
	}
	all, err := s.List()
	if err != nil {
		return categories.Category{}, err
	}
	if len(all) == 0 {
		return categories.Category{}, errNoCategories
	}
	return categories.Pick(all, title)
}

func listCategories() error {
	return withCategories(func(s *categories.Store) error {
		all, err := s.List()
		if err != nil {
			return err
		}
		if len(all) == 0 {
			fmt.Fprintln(out, errNoCategories)
			return nil
		}
		fmt.Fprintln(out, categories.Table(all))
		return nil
	})
}

func createCategory(s *categories.Store) error {
	code, err := s.SuggestCode()
	if err != nil {
		return err
	}
	c := categories.Category{Code: code, Color: core.Palette[0].Name}
	if err := categories.Form(s, &c, "New category"); err != nil {
		return err
	}
	if err := s.Create(&c); err != nil {
		return err
	}
	fmt.Fprintf(out, "created category %s (%s)\n", c.Code, c.Name)
	return nil
}

func editCategory(s *categories.Store, args []string) error {
	c, err := resolveOrPickCategory(s, args, "Edit which category?")
	if err != nil {
		return err
	}
	if err := categories.Form(s, &c, "Edit category"); err != nil {
		return err
	}
	if err := s.Update(c); err != nil {
		return err
	}
	fmt.Fprintf(out, "updated category %s (%s)\n", c.Code, c.Name)
	return nil
}

func deleteCategory(s *categories.Store, args []string) error {
	c, err := resolveOrPickCategory(s, args, "Delete which category?")
	if err != nil {
		return err
	}
	ok, err := core.Confirm(
		fmt.Sprintf("Delete %s (%s)?", c.Code, c.Name),
		"This cannot be undone.")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "cancelled")
		return nil
	}
	if err := s.Delete(c.ID); err != nil {
		return err
	}
	fmt.Fprintf(out, "deleted category %s (%s)\n", c.Code, c.Name)
	return nil
}
