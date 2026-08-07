package cards

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"kakei/internal/core"
)

// Label is how a card is named everywhere it appears in a list: its code in its
// own color, then the name.
func Label(c Card) string {
	code := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Col().Hex)).Bold(true).Render(c.Code)
	return "[" + code + "] " + c.Name
}

// availableColor is green while there is room on the card and red once it is
// over its limit. Exactly at the limit is neither, so it keeps the default
// foreground. Note this colors Available, not Balance: on a card a big balance
// is what you owe, so the green/red of an account would read backwards.
func availableColor(c Card) string {
	switch {
	case c.Available() > 0:
		return core.ColorByName("green").Hex
	case c.Available() < 0:
		return core.ColorByName("red").Hex
	}
	return ""
}

// styledAvailable is "what is left / the whole limit", the left half colored.
func styledAvailable(c Card) string {
	left := lipgloss.NewStyle().Foreground(lipgloss.Color(availableColor(c))).Render(c.Fmt(c.Available()))
	return left + " " + core.DimStyle.Render("/ "+c.Fmt(c.Limit))
}

// days is the closing and due day as one compact cell.
func days(c Card) string {
	return strconv.Itoa(c.ClosingDay) + "/" + strconv.Itoa(c.DueDay)
}

const barWidth = 20

// usageBar shows how much of the limit is spent.
//
// ponytail: strings.Repeat, not bubbles/progress — nothing here animates, and
// the two exact amounts are printed right above it.
func usageBar(c Card) string {
	filled := barWidth
	if c.Limit > 0 {
		// Integer math: no float ever touches an amount, here either.
		filled = int(c.Balance * barWidth / c.Limit)
	} else if c.Balance <= 0 {
		// No limit set: anything owed is "full", nothing owed is "empty".
		filled = 0
	}
	filled = max(0, min(filled, barWidth))

	color := c.Col().Hex
	if c.Available() < 0 {
		color = core.ColorByName("red").Hex
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(strings.Repeat("█", filled)) +
		core.DimStyle.Render(strings.Repeat("░", barWidth-filled))
}

// Form drives create and edit. On create, pass a Card pre-filled with a
// suggested code and the palette/currency defaults.
func Form(s *Store, c *Card, title string) error {
	original := core.NormalizeCode(c.Code)
	limit := core.FormatAmount(c.Limit, c.Cur())
	balance := core.FormatAmount(c.Balance, c.Cur())
	closing, due := strconv.Itoa(c.ClosingDay), strconv.Itoa(c.DueDay)

	amount := func(v string) error {
		_, err := core.ParseAmount(v, core.CurrencyByCode(c.Currency))
		return err
	}
	day := func(v string) error {
		_, err := ParseDay(v)
		return err
	}

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
		huh.NewSelect[string]().Title("Currency").Options(core.CurrencyOptions()...).Value(&c.Currency),
		// Currency comes first so the two validators below know the scale.
		huh.NewInput().Title("Limit").Value(&limit).Validate(amount),
		huh.NewInput().Title("Balance").Description("already used on the open invoice").
			Value(&balance).Validate(amount),
		huh.NewInput().Title("Closing day").Description("day of the month, 1-31").
			Value(&closing).Validate(day),
		huh.NewInput().Title("Due day").Description("day of the month, 1-31").
			Value(&due).Validate(day),
	).Title(title)).WithTheme(huh.ThemeCharm())

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return core.ErrCancelled
		}
		return err
	}

	cur := core.CurrencyByCode(c.Currency)
	l, err := core.ParseAmount(limit, cur)
	if err != nil {
		return err
	}
	b, err := core.ParseAmount(balance, cur)
	if err != nil {
		return err
	}
	cd, err := ParseDay(closing)
	if err != nil {
		return err
	}
	dd, err := ParseDay(due)
	if err != nil {
		return err
	}

	c.Name = strings.TrimSpace(c.Name)
	c.Code = core.NormalizeCode(c.Code)
	c.Limit, c.Balance, c.ClosingDay, c.DueDay = l, b, cd, dd
	return nil
}

// pickerRow is how one card reads in the picker list.
func pickerRow(c Card) core.Choice {
	return core.Choice{
		Label:  Label(c),
		Desc:   c.Fmt(c.Available()) + " available",
		Filter: c.Code + " " + c.Name,
	}
}

// Pick shows the list used whenever a command is given no {CODE|ID}.
func Pick(cards []Card, title string) (Card, error) {
	return core.Pick(cards, title, pickerRow)
}

// Table is the static list output — no alt screen, so `kakei cc | grep` works.
func Table(cards []Card) string {
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(core.DimStyle).
		Headers("CARD", "AVAILABLE", "CLOSE/DUE").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return core.HeaderStyle.Padding(0, 1)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})
	for _, c := range cards {
		// The currency symbol is in the amounts, so a currency column would
		// only repeat it.
		t.Row(Label(c), styledAvailable(c), days(c))
	}
	return t.Render()
}

// cardWidth is the narrowest the card gets; longer names simply widen it.
const cardWidth = 38

const (
	createdIcon = "✚" // Dingbats — no Nerd Font needed.
	updatedIcon = "#"
)

// Details renders one card bordered in its own color — no field names, since
// the color, the currency symbol and the bar already say what each value is.
func Details(c Card) string {
	accent := lipgloss.Color(c.Col().Hex)

	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(accent).Render(c.Code),
		lipgloss.NewStyle().Bold(true).Render(c.Name),
	}
	if c.Description != "" {
		lines = append(lines, core.DimStyle.Render(c.Description))
	}
	lines = append(lines, "",
		// What the open invoice owes, then what is left of the limit.
		lipgloss.NewStyle().Bold(true).Render(c.Fmt(c.Balance))+" "+core.DimStyle.Render(c.Cur().Code),
		lipgloss.NewStyle().Foreground(lipgloss.Color(availableColor(c))).Render(c.Fmt(c.Available()))+
			" "+core.DimStyle.Render("of "+c.Fmt(c.Limit)),
		usageBar(c),
		"")

	now := time.Now()
	lines = append(lines, core.DimStyle.Render(fmt.Sprintf("closes %d (%s) · due %d (%s)",
		c.ClosingDay, NextDate(now, c.ClosingDay).Format("2006-01-02"),
		c.DueDay, NextDate(now, c.DueDay).Format("2006-01-02"))))

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
