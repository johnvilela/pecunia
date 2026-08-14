package accounts

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"kakei/internal/core"
)

var frozenMark = lipgloss.NewStyle().Foreground(lipgloss.Color("#05A2C2")).Render("❄")

// Label is how an account is named everywhere it appears in a list: its code in
// its own color, then the name, then the frozen mark when it is frozen. A
// frozen account is dimmed whole, code included — it is out of play, so it
// should not compete with the active ones for attention.
func Label(a Account) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(labelColor(a)))
	if a.IsFrozen {
		return style.Render("[" + a.Code + "] " + a.Name + " ❄")
	}
	return "[" + style.Bold(true).Render(a.Code) + "] " + a.Name
}

// labelColor is the account's own color, or the dim gray once it is frozen.
func labelColor(a Account) string {
	if a.IsFrozen {
		return core.DimColor
	}
	return a.Col().Hex
}

// balanceColor is green for a credit and red for a debit. Zero is neither, so
// it keeps the default foreground; a frozen account is dimmed regardless.
func balanceColor(a Account) string {
	switch {
	case a.IsFrozen:
		return core.DimColor
	case a.Balance > 0:
		return core.ColorByName("green").Hex
	case a.Balance < 0:
		return core.ColorByName("red").Hex
	}
	return ""
}

// styledAmount renders a balance with its currency symbol, colored by sign.
func styledAmount(a Account) string {
	c := a.Cur()
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(balanceColor(a))).
		Render(c.Symbol + a.Amount())
}

// Form drives create and edit. On create, pass an Account pre-filled with a
// suggested code and the palette/currency defaults.
func Form(s *Store, a *Account, title string) error {
	original := core.NormalizeCode(a.Code)
	balance := core.FormatAmount(a.Balance, a.Cur())

	// The currency is only offered while nothing is filed against the account.
	// Asking for it and then refusing the whole form at the store is a worse way
	// to say the same thing — and the store still says it, because huh skips its
	// validators when stdin ends mid-form.
	linked := 0
	if a.ID != 0 {
		var err error
		if linked, err = s.Linked(a.ID); err != nil {
			return err
		}
	}

	fields := []huh.Field{
		huh.NewInput().Title("Name").Value(&a.Name).Validate(core.ValidateName),
		huh.NewInput().Title("Description").Description("optional").Value(&a.Description),
		huh.NewInput().Title("Code").Description(fmt.Sprintf("%d characters — suggestion pre-filled", core.CodeLen)).
			Value(&a.Code).
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
		huh.NewSelect[string]().Title("Color").Options(core.ColorOptions()...).Value(&a.Color),
	}
	if linked == 0 {
		fields = append(fields, huh.NewSelect[string]().Title("Currency").
			Options(core.CurrencyOptions()...).Value(&a.Currency))
	}
	// Currency comes first so this validator knows the scale to parse with.
	fields = append(fields, huh.NewInput().Title("Balance").Value(&balance).
		Validate(func(v string) error {
			_, err := core.ParseAmount(v, core.CurrencyByCode(a.Currency))
			return err
		}))

	form := huh.NewForm(huh.NewGroup(fields...).Title(title)).WithTheme(huh.ThemeCharm())

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return core.ErrCancelled
		}
		return err
	}

	v, err := core.ParseAmount(balance, core.CurrencyByCode(a.Currency))
	if err != nil {
		return err
	}
	a.Name = strings.TrimSpace(a.Name)
	a.Code = core.NormalizeCode(a.Code)
	a.Balance = v
	return nil
}

// pickerRow is how one account reads in the picker list.
func pickerRow(a Account) core.Choice {
	c := a.Cur()
	return core.Choice{
		Label:  Label(a),
		Desc:   fmt.Sprintf("%s%s %s", c.Symbol, a.Amount(), c.Code),
		Filter: a.Code + " " + a.Name,
	}
}

// Pick shows the list used whenever a command is given no {CODE|ID}.
func Pick(accs []Account, title string) (Account, error) {
	return core.Pick(accs, title, pickerRow)
}

// Table is the static list output — no alt screen, so `kakei ac | grep` works.
func Table(accs []Account) string {
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(core.DimStyle).
		Headers("ACCOUNT", "BALANCE").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return core.HeaderStyle.Padding(0, 1)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})
	for _, a := range accs {
		// The currency symbol is in the amount, so a currency column would only
		// repeat it.
		t.Row(Label(a), styledAmount(a))
	}
	return t.Render()
}

// cardWidth is the narrowest the card gets; longer names simply widen it.
const cardWidth = 34

const (
	createdIcon = "✚" // Dingbats, same block as the ❄ — no Nerd Font needed.
	updatedIcon = "#"
)

// Details renders one account as a card bordered in the account's own color —
// no field names, since the color, the currency symbol and the ❄ already say
// what each value is. A frozen account is dimmed border and all.
func Details(a Account) string {
	accent := lipgloss.Color(labelColor(a))

	code := lipgloss.NewStyle().Bold(true).Foreground(accent).Render(a.Code)
	if a.IsFrozen {
		code += " " + frozenMark
	}

	lines := []string{code, lipgloss.NewStyle().Bold(true).Render(a.Name)}
	if a.Description != "" {
		lines = append(lines, core.DimStyle.Render(a.Description))
	}
	// The amount carries the sign color; the code beside it is the only place
	// the currency is spelled out.
	lines = append(lines, "",
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(balanceColor(a))).
			Render(a.Cur().Symbol+a.Amount())+" "+core.DimStyle.Render(a.Cur().Code),
		"")

	// The ❄ beside the code already says whether the account is frozen, and the
	// id is only ever typed by someone who read it off `kakei ac`.
	if a.CreatedAt != "" {
		lines = append(lines, core.DimStyle.Render(
			createdIcon+" "+a.CreatedAt+"   "+updatedIcon+" "+a.UpdatedAt))
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
