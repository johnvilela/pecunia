package goals

import (
	"errors"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"kakei/internal/core"
)

// Label is how a goal is named in a list. A goal has no code and no colour to
// lead with, so the name leads and the target follows it, dimmed.
func Label(g Goal) string {
	return g.Name + "  " + core.DimStyle.Render(g.Fmt(g.Target))
}

// stateColor is the goal's own state, since it has no colour of its own: green
// once it is reached, red while it is going backwards, and the default
// foreground for the long stretch in between, where a colour would only be
// noise.
func stateColor(g Goal) string {
	switch {
	case g.Reached():
		return core.ColorByName("green").Hex
	case g.Progress() < 0:
		return core.ColorByName("red").Hex
	default:
		return ""
	}
}

const barWidth = 20

// bar is how far along the goal is, clamped at both ends: a goal past its
// target reads full rather than overflowing, and one that has gone backwards
// reads empty.
//
// ponytail: strings.Repeat and integer math, the same shape as cards.usageBar —
// nothing animates, and the two exact amounts are printed right above it. Two
// copies now; lift it into core if a third module grows a bar.
func bar(g Goal) string {
	filled := 0
	if g.Target > 0 {
		// Integer math: no float ever touches an amount, here either.
		filled = int(g.Progress() * barWidth / g.Target)
	}
	filled = max(0, min(filled, barWidth))

	return lipgloss.NewStyle().Foreground(lipgloss.Color(stateColor(g))).
		Render(strings.Repeat("█", filled)) +
		core.DimStyle.Render(strings.Repeat("░", barWidth-filled))
}

// pct is the bar as a number, and unlike the bar it is not clamped: 180% is
// news, and a full bar cannot tell it from 100%.
func pct(g Goal) string {
	if g.Target <= 0 {
		return ""
	}
	return strconv.FormatInt(g.Progress()*100/g.Target, 10) + "%"
}

// Form drives create and edit. On create, pass a Goal pre-filled with the kind
// and currency defaults.
//
// linked is how many transactions already name this goal — zero on a new one.
// It decides whether the currency may still be chosen.
func Form(g *Goal, title string, linked int) error {
	target := ""
	if g.Target != 0 {
		target = core.FormatAmount(g.Target, g.Cur())
	}

	fields := []huh.Field{
		huh.NewInput().Title("Name").Value(&g.Name).Validate(core.ValidateName),
		huh.NewInput().Title("Description").Description("optional").Value(&g.Description),
		huh.NewSelect[string]().Title("Kind").Value(&g.Kind).Options(
			huh.NewOption("Saving — money set aside", KindSaving),
			huh.NewOption("Paying — something worked down", KindPaying)),
	}
	// The currency is only offered while nothing is linked. Asking for it and
	// then refusing the whole form at the store is a worse way to say the same
	// thing — and the store still says it, because huh skips its validators when
	// stdin ends mid-form.
	if linked == 0 {
		fields = append(fields, huh.NewSelect[string]().Title("Currency").
			Options(core.CurrencyOptions()...).Value(&g.Currency))
	}
	// The currency comes first so this validator knows the scale to read the
	// target at — the same ordering cards.Form uses for currency-before-limit.
	fields = append(fields, huh.NewInput().Title("Target").
		Description("what reaching it is worth").Value(&target).
		Validate(func(v string) error {
			n, err := core.ParseAmount(v, core.CurrencyByCode(g.Currency))
			if err != nil {
				return err
			}
			if n <= 0 {
				return errors.New("target must be more than zero")
			}
			return nil
		}))

	form := huh.NewForm(huh.NewGroup(fields...).Title(title)).WithTheme(huh.ThemeCharm())
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return core.ErrCancelled
		}
		return err
	}

	g.Name = strings.TrimSpace(g.Name)
	// huh skips its validators when stdin ends mid-form, so an unreadable target
	// is left as it was rather than written through — and the store refuses the
	// zero a new goal started with.
	if n, err := core.ParseAmount(target, g.Cur()); err == nil {
		g.Target = n
	}
	return nil
}

// pickerRow is how one goal reads in the picker list.
func pickerRow(g Goal) core.Choice {
	return core.Choice{
		Label:  Label(g),
		Desc:   g.Fmt(g.Progress()) + " " + g.Verb() + "  " + pct(g),
		Filter: g.Name,
	}
}

// Pick shows the list used whenever a command is given no ID.
func Pick(gs []Goal, title string) (Goal, error) {
	return core.Pick(gs, title, pickerRow)
}

// Table is the static list output — no alt screen, so `kakei g | grep` works.
func Table(gs []Goal) string {
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(core.DimStyle).
		Headers("GOAL", "PROGRESS", "TARGET").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return core.HeaderStyle.Padding(0, 1)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})
	for _, g := range gs {
		// The currency symbol is in the amounts, so a currency column would only
		// repeat it — and the verb is what tells a saving goal from a paying one
		// without a column of its own.
		t.Row(g.Name,
			lipgloss.NewStyle().Foreground(lipgloss.Color(stateColor(g))).Render(g.Fmt(g.Progress()))+
				" "+core.DimStyle.Render(g.Verb()+"  "+pct(g)),
			core.DimStyle.Render(g.Fmt(g.Target)))
	}
	return t.Render()
}

// cardWidth is the narrowest the card gets; longer names simply widen it.
const cardWidth = 40

const (
	createdIcon = "✚" // Dingbats, same block as every other card — no Nerd Font needed.
	updatedIcon = "#"
)

// left is what is still to go — or, once the goal is past its target, how far
// past. "R$-120.00 to go" is a riddle; "R$120.00 past it" is not.
func left(g Goal) string {
	switch v := g.Remaining(); {
	case v > 0:
		return g.Fmt(v) + " " + core.DimStyle.Render("to go")
	case v == 0:
		return core.DimStyle.Render("reached")
	default:
		return g.Fmt(-v) + " " + core.DimStyle.Render("past it — reached")
	}
}

// Details renders one goal as a card bordered in the colour of its state. There
// are no field names: the currency symbol, the verb and the bar already say
// what every value is.
func Details(g Goal) string {
	accent := lipgloss.Color(stateColor(g))
	if accent == "" {
		accent = lipgloss.Color(core.DimColor)
	}

	lines := []string{lipgloss.NewStyle().Bold(true).Render(g.Name)}
	if g.Description != "" {
		lines = append(lines, core.DimStyle.Render(g.Description))
	}
	lines = append(lines, "",
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(stateColor(g))).
			Render(g.Fmt(g.Progress()))+
			" "+core.DimStyle.Render(g.Verb()+"  of "+g.Fmt(g.Target)),
		left(g),
		bar(g)+"  "+core.DimStyle.Render(pct(g)))

	if g.CreatedAt != "" {
		lines = append(lines, "", core.DimStyle.Render(
			createdIcon+" "+g.CreatedAt+"   "+updatedIcon+" "+g.UpdatedAt))
	}

	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	// Width covers the padding too, so the content gets it back — without the +4
	// the longest line wraps.
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
