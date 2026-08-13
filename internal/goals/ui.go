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

// reachedMark flags a goal that is there. Plain Unicode, same as the ❄ accounts
// use and the ↑ on a card — no Nerd Font needed.
const reachedMark = "✓"

// name is what a goal is called wherever there is one column for it, with the
// mark when it is done. Rendered rather than stored, so nothing has to be
// stripped back out of what the user typed.
func name(g Goal) string {
	if !g.Reached() {
		return g.Name
	}
	return g.Name + " " + lipgloss.NewStyle().
		Foreground(lipgloss.Color(core.ColorByName("green").Hex)).Render(reachedMark)
}

// Label is how a goal is named in a list. A goal has no code and no colour to
// lead with, so the name leads and the target follows it, dimmed.
func Label(g Goal) string {
	return name(g) + "  " + core.DimStyle.Render(g.Fmt(g.Target))
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
//
// The returned string is why the target moved, asked for only on an edit and
// kept by the store only if it really did move. A new goal has no history to
// explain: its target is where the history starts.
func Form(g *Goal, title string, linked int) (string, error) {
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

	// Only on an edit, and always on one: a conditional field that appears the
	// moment the target is touched is a page huh would have to redraw underfoot,
	// and the store drops the note when the target did not move anyway.
	note := ""
	if g.ID != 0 {
		fields = append(fields, huh.NewInput().Title("Why?").
			Description("optional — kept only if the target changed").Value(&note))
	}

	form := huh.NewForm(huh.NewGroup(fields...).Title(title)).WithTheme(huh.ThemeCharm())
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", core.ErrCancelled
		}
		return "", err
	}

	g.Name = strings.TrimSpace(g.Name)
	// huh skips its validators when stdin ends mid-form, so an unreadable target
	// is left as it was rather than written through — and the store refuses the
	// zero a new goal started with.
	if n, err := core.ParseAmount(target, g.Cur()); err == nil {
		g.Target = n
	}
	return strings.TrimSpace(note), nil
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
		t.Row(name(g),
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

// dividerRune draws the rule between the goal itself and its history. Box
// drawing, the same rune the card's own rounded border is made of.
const dividerRune = "─"

// history is what the target has been, newest first: the day it moved and what
// it moved between, then the reason on its own line under them, where a long
// one has room to be read.
//
// The day only — the clock time is stored but a target does not move twice in an
// afternoon, and the date is what anyone reading this is looking for.
func history(g Goal, log []TargetChange) []string {
	lines := []string{core.DimStyle.Render("target")}
	for _, c := range log {
		lines = append(lines, core.DimStyle.Render(day(c.CreatedAt))+"  "+
			g.Fmt(c.Previous)+core.DimStyle.Render(" → ")+g.Fmt(c.Target))
		if c.Note != "" {
			lines = append(lines, core.DimStyle.Render(c.Note))
		}
	}
	return lines
}

// day is the date out of a stored timestamp. They are written by SQLite as
// "2026-08-13 09:12:00", so the day is everything up to the space.
func day(stamp string) string {
	date, _, _ := strings.Cut(stamp, " ")
	return date
}

// rule is a divider as wide as the widest line it sits among, so it spans the
// card rather than guessing at it.
func rule(lines []string) string {
	w := cardWidth - 4 // the card's floor, less the padding it will get back
	for _, l := range lines {
		w = max(w, lipgloss.Width(l))
	}
	return core.DimStyle.Render(strings.Repeat(dividerRune, w))
}

// Details renders one goal as a card bordered in the colour of its state. There
// are no field names: the currency symbol, the verb and the bar already say
// what every value is.
//
// log is the goal's target history, and may be nil — the list of cards passes
// none, so a screen of goals stays readable, and only `kakei g ID` asks for it.
func Details(g Goal, log []TargetChange) string {
	accent := lipgloss.Color(stateColor(g))
	if accent == "" {
		accent = lipgloss.Color(core.DimColor)
	}

	lines := []string{lipgloss.NewStyle().Bold(true).Render(name(g))}
	if g.Description != "" {
		lines = append(lines, core.DimStyle.Render(g.Description))
	}
	lines = append(lines, "",
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(stateColor(g))).
			Render(g.Fmt(g.Progress()))+
			" "+core.DimStyle.Render(g.Verb()+"  of "+g.Fmt(g.Target)),
		left(g),
		bar(g)+"  "+core.DimStyle.Render(pct(g)))

	// The goal's own dates first, then the rule, then what its target has been:
	// the two blocks are about different things, and the rule is what says so.
	if g.CreatedAt != "" {
		lines = append(lines, "", core.DimStyle.Render(
			createdIcon+" "+g.CreatedAt+"   "+updatedIcon+" "+g.UpdatedAt))
	}
	if len(log) > 0 {
		entries := history(g, log)
		// Drawn to the width of everything it has to separate, which is only
		// known once every line exists.
		lines = append(lines, rule(append(append([]string{}, lines...), entries...)))
		lines = append(lines, entries...)
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
