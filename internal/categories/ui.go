package categories

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"kakei/internal/core"
)

// Label is how a category is named everywhere it appears in a list: its code in
// its own color, then the name.
func Label(c Category) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Col().Hex))
	return "[" + style.Bold(true).Render(c.Code) + "] " + c.Name
}

// Form drives create and edit. On create, pass a Category pre-filled with a
// suggested code and the palette default.
//
// ponytail: the code validator is the same closure as accounts.Form and
// cards.Form. Three copies now — lift it into core as
// ValidateCodeFree(taken, original) if a fourth module shows up.
func Form(s *Store, c *Category, title string) error {
	original := core.NormalizeCode(c.Code)

	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Name").Value(&c.Name).Validate(core.ValidateName),
		huh.NewInput().Title("Description").Description("optional").Value(&c.Description),
		huh.NewInput().Title("Code").Description(fmt.Sprintf("%d characters — suggestion pre-filled", core.CodeLen)).
			Value(&c.Code).
			Validate(func(v string) error {
				if err := core.ValidateCode(v); err != nil {
					return err
				}
				if core.NormalizeCode(v) == original {
					return nil
				}
				taken, err := s.CodeTaken(v)
				if err != nil {
					return err
				}
				if taken {
					return fmt.Errorf("code %s is already in use", core.NormalizeCode(v))
				}
				return nil
			}),
		huh.NewSelect[string]().Title("Color").Options(core.ColorOptions()...).Value(&c.Color),
	).Title(title)).WithTheme(huh.ThemeCharm())

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return core.ErrCancelled
		}
		return err
	}

	c.Name = strings.TrimSpace(c.Name)
	c.Code = core.NormalizeCode(c.Code)
	return nil
}

// pickerRow is how one category reads in the picker list.
func pickerRow(c Category) core.Choice {
	return core.Choice{
		Label:  Label(c),
		Desc:   c.Description,
		Filter: c.Code + " " + c.Name,
	}
}

// Pick shows the list used whenever a command is given no {CODE|ID}.
func Pick(cats []Category, title string) (Category, error) {
	return core.Pick(cats, title, pickerRow)
}

// Table is the static list output — no alt screen, so `kakei ct | grep` works.
func Table(cats []Category) string {
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(core.DimStyle).
		Headers("CATEGORY", "DESCRIPTION").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return core.HeaderStyle.Padding(0, 1)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})
	for _, c := range cats {
		// The description is context, not the thing itself, so it stays dim and
		// lets the colored code lead.
		t.Row(Label(c), core.DimStyle.Render(c.Description))
	}
	return t.Render()
}

// cardWidth is the narrowest the card gets; longer names simply widen it.
const cardWidth = 34

const (
	createdIcon = "✚" // Dingbats, same block as the accounts card — no Nerd Font needed.
	updatedIcon = "#"
)

// Details renders one category as a card bordered in its own color — no field
// names, since a category is only a code, a name and a line about it.
func Details(c Category) string {
	accent := lipgloss.Color(c.Col().Hex)

	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(accent).Render(c.Code),
		lipgloss.NewStyle().Bold(true).Render(c.Name),
	}
	if c.Description != "" {
		lines = append(lines, core.DimStyle.Render(c.Description))
	}
	if c.CreatedAt != "" {
		lines = append(lines, "", core.DimStyle.Render(
			createdIcon+" "+c.CreatedAt+"   "+updatedIcon+" "+c.UpdatedAt))
	}

	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	// Width covers the padding too, so the content gets it back — without the
	// +4 the longest line wraps.
	w := lipgloss.Width(body) + 4
	if w < cardWidth {
		w = cardWidth
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(0, 2).
		Width(w).
		Render(body) + "\n"
}
