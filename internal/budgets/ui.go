package budgets

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"pecunia/internal/core"
	"pecunia/internal/transactions"
)

// stateColor is where the month stands, as a colour: red past the cap, amber
// running ahead of the month, green while there is room, and the dim grey
// everything out of play is drawn in.
func stateColor(b Budget, today time.Time) string {
	switch b.Status(today) {
	case StatusOver:
		return core.ColorByName("red").Hex
	case StatusAhead:
		return core.ColorByName("amber").Hex
	case StatusArchived:
		return core.DimColor
	default:
		return core.ColorByName("green").Hex
	}
}

func stateStyle(b Budget, today time.Time) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(stateColor(b, today)))
}

const barWidth = 20

// bar is how much of the cap has gone, clamped at both ends: a budget past its
// cap reads full rather than overflowing the column it is drawn in, and one
// that refunds took below zero reads empty.
//
// ponytail: strings.Repeat and integer math, the third copy of this shape after
// cards.usageBar and goals.bar. Lift the three into core if a fourth appears.
// No pace tick inside it either — the drift line says the same thing in money,
// and a tick would cost the fixed width the table column lines up on.
func bar(b Budget, today time.Time) string {
	filled := 0
	if b.Amount > 0 {
		// Integer math: no float ever touches an amount, here either.
		filled = int(b.Spent * barWidth / b.Amount)
	}
	filled = max(0, min(filled, barWidth))

	return stateStyle(b, today).Render(strings.Repeat("█", filled)) +
		core.DimStyle.Render(strings.Repeat("░", barWidth-filled))
}

// left is what is still there to spend — or, once the budget is past its cap,
// how far past. "R$-70.00 left" is a riddle; "R$70.00 over" is not.
func left(b Budget) string {
	switch v := b.Remaining(); {
	case v > 0:
		return b.Fmt(v) + " " + core.DimStyle.Render("left")
	case v == 0:
		return core.DimStyle.Render("spent to the cap")
	default:
		return b.Fmt(-v) + " " + core.DimStyle.Render("over")
	}
}

// against is how the month is running, in money rather than in a percentage: a
// cap is only reached at the end of the month, so what matters is whether today
// is ahead of it, and by how much.
//
// An archived budget is not judged against anything — it is not being tracked,
// and saying it is behind would be news about a month nobody is watching.
func against(b Budget, today time.Time) string {
	if !b.Active {
		return core.DimStyle.Render(StatusArchived)
	}
	switch d := b.Drift(today); {
	case d > 0:
		return b.Fmt(d) + " " + core.DimStyle.Render("ahead of the month")
	case d == 0:
		return core.DimStyle.Render("exactly on pace")
	default:
		return b.Fmt(-d) + " " + core.DimStyle.Render("under the month")
	}
}

// Label is how a budget is named in a list: the name, then the cap it is for.
func Label(b Budget) string {
	return b.Name + "  " + core.DimStyle.Render(b.Fmt(b.Amount))
}

// title is the month a reading is for, spelled the way a summary spells it.
func title(cycle string) string {
	d, err := time.Parse(CycleLayout, cycle)
	if err != nil {
		return cycle
	}
	return d.Format("January 2006")
}

// Table is the static list output — no alt screen, so `pecunia bg | grep` works.
// Nil comes back empty rather than as a header with nothing under it, so the
// caller can say why there is nothing instead.
func Table(bs []Budget, today time.Time) string {
	if len(bs) == 0 {
		return ""
	}
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(core.DimStyle).
		Headers("BUDGET", "SPENT", "LEFT", "MONTH").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return core.HeaderStyle.Padding(0, 1)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})
	for _, b := range bs {
		status := b.Status(today)
		t.Row(b.Name,
			stateStyle(b, today).Render(b.Fmt(b.Spent))+
				core.DimStyle.Render(" of "+b.Fmt(b.Amount)),
			left(b),
			bar(b, today)+"  "+stateStyle(b, today).Render(status))
	}
	return t.Render()
}

// pickerRow is how one budget reads in the picker list. The picker has no date
// to judge against, so it shows the cap and the spend and leaves the verdict to
// the screen the pick leads to.
func pickerRow(b Budget) core.Choice {
	return core.Choice{
		Label:  Label(b),
		Desc:   b.Fmt(b.Spent) + " spent  " + strconv.FormatInt(b.Pct(), 10) + "%",
		Filter: b.Code + " " + b.Name + " " + b.Category.Name,
	}
}

// Pick shows the list used whenever a command is given no {CODE|ID}.
func Pick(bs []Budget, title string) (Budget, error) {
	return core.Pick(bs, title, pickerRow)
}

// cardWidth is the narrowest the card gets; longer names simply widen it.
const cardWidth = 44

const (
	createdIcon = "✚" // Dingbats, the same block every other card uses.
	updatedIcon = "#"
	dividerRune = "─"
)

// day is the date out of a stored timestamp. SQLite writes them as
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

// updates is what the cap has been, newest first: the day it moved and what it
// moved between, then the reason under them where a long one has room to read.
func updates(b Budget, log []AmountChange) []string {
	lines := []string{core.DimStyle.Render("updates")}
	for _, c := range log {
		lines = append(lines, core.DimStyle.Render(day(c.CreatedAt))+"  "+
			b.Fmt(c.Previous)+core.DimStyle.Render(" → ")+b.Fmt(c.Amount))
		if c.Note != "" {
			lines = append(lines, core.DimStyle.Render(c.Note))
		}
	}
	return lines
}

// months is what the category has really cost, one line per month against the
// cap that is in force now. A budget missed every month is a cap that is wrong,
// not a month that went badly, and only the run of them says which.
func months(b Budget, history []CycleSpend) []string {
	lines := []string{core.DimStyle.Render("last months")}
	for _, m := range history {
		over := ""
		if m.Spent > b.Amount {
			over = " " + lipgloss.NewStyle().
				Foreground(lipgloss.Color(core.ColorByName("red").Hex)).Render("over")
		}
		lines = append(lines, core.DimStyle.Render(m.Cycle)+"  "+b.Fmt(m.Spent)+over)
	}
	return lines
}

// Details renders one budget as a card bordered in the colour of where its
// month stands. There are no field names: the currency symbol, the bar and the
// wording already say what every value is.
//
// log and history may both be nil — the list of cards passes neither, so a
// screen of budgets stays readable, and only `pecunia bg CODE` asks for them.
func Details(b Budget, log []AmountChange, history []CycleSpend, today time.Time) string {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Render(b.Name) + "  " +
			core.DimStyle.Render(b.Code+" · "+b.Category.Name),
	}
	if b.Description != "" {
		lines = append(lines, core.DimStyle.Render(b.Description))
	}
	lines = append(lines, "",
		core.DimStyle.Render(title(b.Cycle)),
		stateStyle(b, today).Bold(true).Render(b.Fmt(b.Spent))+
			core.DimStyle.Render(" of "+b.Fmt(b.Amount)),
		left(b),
		bar(b, today)+"  "+core.DimStyle.Render(strconv.FormatInt(b.Pct(), 10)+"%"),
		against(b, today))

	if b.CreatedAt != "" {
		lines = append(lines, "", core.DimStyle.Render(
			createdIcon+" "+b.CreatedAt+"   "+updatedIcon+" "+b.UpdatedAt))
	}
	// Each block is about something different, and the rule is what says so.
	for _, block := range [][]string{months(b, history), updates(b, log)} {
		if len(block) <= 1 { // the heading alone means there was nothing to head
			continue
		}
		lines = append(lines, rule(append(append([]string{}, lines...), block...)))
		lines = append(lines, block...)
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
		BorderForeground(lipgloss.Color(stateColor(b, today))).
		Padding(0, 2).
		Width(w).
		Render(body) + "\n"
}

// FormData is what the form offers to choose from. A budget caps a category, so
// there is nothing else to pick.
type FormData struct {
	Categories []transactions.Ref
}

// Form drives create and edit. On create, pass a Budget pre-filled with the
// suggested code and the colour and currency defaults.
//
// counted is how many transactions a budget over this category and currency
// already takes in — zero on a new one. It decides whether the currency may
// still be chosen, the same way a goal's linked count does.
//
// The returned string is why the cap moved, asked for only on an edit and kept
// by the store only if it really did move. A new budget has no history to
// explain: its cap is where the history starts.
func Form(d FormData, b *Budget, title string, counted int) (string, error) {
	amount := ""
	if b.Amount != 0 {
		amount = core.FormatAmount(b.Amount, b.Cur())
	}

	opts := make([]huh.Option[int64], len(d.Categories))
	for i, c := range d.Categories {
		opts[i] = huh.NewOption(c.Code+"  "+c.Name, c.ID)
	}

	fields := []huh.Field{
		huh.NewInput().Title("Name").Value(&b.Name).Validate(core.ValidateName),
		huh.NewInput().Title("Description").Description("optional").Value(&b.Description),
		huh.NewInput().Title("Code").
			Description(fmt.Sprintf("%d characters — suggestion pre-filled", core.CodeLen)).
			Value(&b.Code).Validate(core.ValidateCode),
		huh.NewSelect[int64]().Title("Category").Description("what this caps").
			Options(opts...).Value(&b.Category.ID),
		huh.NewSelect[string]().Title("Color").Options(core.ColorOptions()...).Value(&b.Color),
	}
	// The currency is only offered while nothing has been counted. Asking for it
	// and then refusing the whole form at the store is a worse way to say the
	// same thing — and the store still says it, because huh skips its validators
	// when stdin ends mid-form.
	if counted == 0 {
		fields = append(fields, huh.NewSelect[string]().Title("Currency").
			Options(core.CurrencyOptions()...).Value(&b.Currency))
	}
	// The currency comes first so this validator knows the scale to read the cap
	// at — the same ordering cards.Form uses for currency-before-limit.
	fields = append(fields, huh.NewInput().Title("Monthly cap").
		Description("what this category is allowed to cost each month").Value(&amount).
		Validate(func(v string) error {
			n, err := core.ParseAmount(v, core.CurrencyByCode(b.Currency))
			if err != nil {
				return err
			}
			if n <= 0 {
				return errors.New("a budget must be more than zero")
			}
			return nil
		}))

	// Only on an edit, and always on one: a conditional field that appears the
	// moment the cap is touched is a page huh would have to redraw underfoot,
	// and the store drops the note when the cap did not move anyway.
	note := ""
	if b.ID != 0 {
		fields = append(fields, huh.NewInput().Title("Why?").
			Description("optional — kept only if the cap changed").Value(&note))
	}

	form := huh.NewForm(huh.NewGroup(fields...).Title(title)).WithTheme(huh.ThemeCharm())
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", core.ErrCancelled
		}
		return "", err
	}

	b.Name = strings.TrimSpace(b.Name)
	b.Code = core.NormalizeCode(b.Code)
	// huh skips its validators when stdin ends mid-form, so an unreadable cap is
	// left as it was rather than written through — and the store refuses the zero
	// a new budget started with.
	if n, err := core.ParseAmount(amount, b.Cur()); err == nil {
		b.Amount = n
	}
	return strings.TrimSpace(note), nil
}
