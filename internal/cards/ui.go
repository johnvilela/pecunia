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

// usedColor colors nothing until the card is over its limit. An account's
// balance can be good news and earns a green; a card's never can, so a green
// on every healthy card would only be noise.
func usedColor(c Card) string {
	if c.Available() < 0 {
		return core.ColorByName("red").Hex
	}
	return ""
}

// styledUsed is the USED / LIMIT cell: what is owed, then what it is owed
// against. The header names both in the same order, so neither needs a word.
func styledUsed(c Card) string {
	used := lipgloss.NewStyle().Foreground(lipgloss.Color(usedColor(c))).Render(c.Fmt(c.Balance))
	// The mark qualifies the limit, so it sits with the limit.
	return used + " " + core.DimStyle.Render("/ "+c.Fmt(c.Limit)+overSuffix(c))
}

// overSuffix is the mark, or nothing at all on a card that declines at its
// limit — which is most of them.
func overSuffix(c Card) string {
	if c.OverLimitAllowed {
		return " " + overMark
	}
	return ""
}

// usagePct is the bar as a number. A card with no limit has no percentage —
// there is nothing to be a fraction of.
func usagePct(c Card) string {
	if c.Limit <= 0 {
		return ""
	}
	return strconv.FormatInt(c.Balance*100/c.Limit, 10) + "%"
}

// days is the closing and due day as one compact cell.
func days(c Card) string {
	return strconv.Itoa(c.ClosingDay) + "/" + strconv.Itoa(c.DueDay)
}

// overMark flags a card the issuer lets you push past its limit. Arrows are
// plain Unicode, same as the ❄ accounts use — no Nerd Font needed.
const overMark = "↑"

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
	if over := usedColor(c); over != "" {
		color = over
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

	// The currency is only offered while nothing is charged to the card. Asking
	// for it and then refusing the whole form at the store is a worse way to say
	// the same thing — and the store still says it, because huh skips its
	// validators when stdin ends mid-form.
	linked := 0
	if c.ID != 0 {
		var err error
		if linked, err = s.Linked(c.ID); err != nil {
			return err
		}
	}

	fields := []huh.Field{
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
	}
	if linked == 0 {
		fields = append(fields, huh.NewSelect[string]().Title("Currency").
			Options(core.CurrencyOptions()...).Value(&c.Currency))
	}
	fields = append(fields,
		// Currency comes first so the two validators below know the scale.
		huh.NewInput().Title("Limit").Value(&limit).Validate(amount),
		huh.NewConfirm().Title("May it be used over the limit?").
			Affirmative("Yes").Negative("No").Value(&c.OverLimitAllowed))
	if c.ID == 0 {
		// Only on create. After that the balance is the ledger's alone: what
		// the card holds is what its transactions say it holds.
		fields = append(fields,
			// Limit and the allowance both come first, so this validator can
			// refuse a balance the card would have declined.
			huh.NewInput().Title("Balance").Description("already used on the open invoice").
				Value(&balance).Validate(func(v string) error {
				if err := amount(v); err != nil {
					return err
				}
				cur := core.CurrencyByCode(c.Currency)
				b, _ := core.ParseAmount(v, cur)
				l, _ := core.ParseAmount(limit, cur)
				return Card{Currency: c.Currency, Balance: b, Limit: l,
					OverLimitAllowed: c.OverLimitAllowed}.ValidateBalance()
			}))
	}
	fields = append(fields,
		huh.NewInput().Title("Closing day").Description("day of the month, 1-31").
			Value(&closing).Validate(day),
		huh.NewInput().Title("Due day").Description("day of the month, 1-31").
			Value(&due).Validate(day))

	form := huh.NewForm(huh.NewGroup(fields...).Title(title)).WithTheme(huh.ThemeCharm())

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
		Headers("CARD", "USED / LIMIT", "CLOSE/DUE").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return core.HeaderStyle.Padding(0, 1)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})
	for _, c := range cards {
		// The currency symbol is in the amounts, so a currency column would
		// only repeat it.
		t.Row(Label(c), styledUsed(c), days(c))
	}
	return t.Render()
}

// cardWidth is the narrowest the card gets; longer names simply widen it.
const cardWidth = 38

const (
	createdIcon = "✚" // Dingbats — no Nerd Font needed.
	updatedIcon = "#"
)

// remaining is what is left to spend — or, once that goes negative, how far
// past the limit the card is. "R$-1120.00 available" is a riddle; "R$1120.00
// over the limit" is not.
func remaining(c Card) string {
	v, word := c.Available(), "available"
	if v < 0 {
		v, word = -v, "over the limit"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(usedColor(c))).Render(c.Fmt(v)) +
		" " + core.DimStyle.Render(word)
}

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
	// The card has the room the table does not, so every number says what it
	// is rather than relying on its position.
	lines = append(lines, "",
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(usedColor(c))).Render(c.Fmt(c.Balance))+
			" "+core.DimStyle.Render("used  of "+c.Fmt(c.Limit)+overSuffix(c)),
		remaining(c),
		usageBar(c)+"  "+core.DimStyle.Render(usagePct(c)),
		"")

	if c.OverLimitAllowed {
		lines = append(lines, core.DimStyle.Render(overMark+" may be used over the limit"), "")
	}

	now := time.Now()
	lines = append(lines,
		core.DimStyle.Render(fmt.Sprintf("closes %d (%s)",
			c.ClosingDay, NextDate(now, c.ClosingDay).Format("2006-01-02"))),
		core.DimStyle.Render(fmt.Sprintf("due %d (%s)",
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
