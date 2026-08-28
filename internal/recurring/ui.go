package recurring

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"pecunia/internal/accounts"
	"pecunia/internal/cards"
	"pecunia/internal/categories"
	"pecunia/internal/core"
	"pecunia/internal/transactions"
)

// The mark each state gets on the board. Plain Unicode, same block as the ❄ on
// a frozen account — no Nerd Font needed.
var marks = map[string]string{
	StatusOverdue:  "!",
	StatusOpen:     "●",
	StatusUpcoming: "○",
	StatusPaid:     "✓",
	StatusArchived: "·",
}

// stateColor is what a state is worth looking at in: red for late, amber for
// payable now, green for settled, and the dim default for a month that has not
// started.
func stateColor(status string) string {
	switch status {
	case StatusOverdue:
		return core.ColorByName("red").Hex
	case StatusOpen:
		return core.ColorByName("amber").Hex
	case StatusPaid:
		return core.ColorByName("green").Hex
	default:
		// Upcoming and archived alike: nothing to do about either today.
		return core.DimColor
	}
}

// code is the bill's code in its own colour, bracketed like every other code in
// pecunia.
func code(b Bill) string {
	return "[" + lipgloss.NewStyle().Foreground(lipgloss.Color(core.ColorByName(b.Color).Hex)).
		Bold(true).Render(b.Code) + "]"
}

// Label is how a bill is named in a list: its code, then what it is.
func Label(b Bill) string { return code(b) + " " + b.Name }

// order is how urgent each state is. It is what the board sorts on, so the
// thing that needs paying today is never below the one already settled.
var order = map[string]int{
	StatusOverdue: 0, StatusOpen: 1, StatusUpcoming: 2, StatusPaid: 3, StatusArchived: 4,
}

// when is the date half of a state: the one thing worth knowing about a cycle
// once its status has been said. It is the board's own column, so the status
// word is never repeated in it.
func when(occ Occurrence) string {
	switch occ.Status {
	case StatusOverdue:
		return plural(occ.Late, "day") + " late"
	case StatusOpen:
		return "due " + occ.DueOn
	case StatusUpcoming:
		return "opens " + occ.OpenOn
	case StatusArchived:
		return "nothing due"
	default:
		return occ.Cycle
	}
}

// state is the two halves in one line, for the places with room for a sentence
// rather than a column — the details card and the picker.
func state(occ Occurrence) string { return occ.Status + " — " + when(occ) }

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// Board is what `pecunia bill` shows: one line per bill for the cycle it is at,
// the ones that need paying first, and a footer of what is still owed.
//
// It is a plain rendering rather than a lipgloss table because the amounts and
// the wording carry the meaning here, and a table's borders around four columns
// of short text is more furniture than content.
func Board(bs []Bill, today time.Time) string {
	if len(bs) == 0 {
		return core.DimStyle.Render("no bills yet") + "\n"
	}

	type line struct {
		bill Bill
		occ  Occurrence
	}
	lines := make([]line, 0, len(bs))
	for _, b := range bs {
		lines = append(lines, line{b, b.Current(today)})
	}
	// Most urgent first; within a state, whatever is due soonest, and the code
	// breaks the tie so two bills on the same day never swap places between runs.
	slices.SortFunc(lines, func(a, b line) int {
		if a.occ.Status != b.occ.Status {
			return order[a.occ.Status] - order[b.occ.Status]
		}
		if a.occ.DueOn != b.occ.DueOn {
			return strings.Compare(a.occ.DueOn, b.occ.DueOn)
		}
		return strings.Compare(a.bill.Code, b.bill.Code)
	})

	// The total is the last row rather than a line under the table, and its
	// label leans right so it sits against the figure it belongs to instead of
	// starting where a bill's name would. Filled in once the rows are counted.
	totalRow := -1

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(core.DimStyle).
		Headers("BILL", "AMOUNT", "STATUS", "WHEN").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				// The amount header sits over a right-aligned column, so it is
				// right-aligned too.
				if col == 1 {
					return core.HeaderStyle.Padding(0, 1).Align(lipgloss.Right)
				}
				return core.HeaderStyle.Padding(0, 1)
			}
			// The amounts are the column the eye runs down, so they are the ones
			// that line up on the right.
			if col == 1 || (row == totalRow && col == 0) {
				return lipgloss.NewStyle().Padding(0, 1).Align(lipgloss.Right)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})

	owed := map[string]int64{}
	for _, l := range lines {
		// What it cost when it is settled, what it is expected to cost when it is
		// not — an energy bill's expected amount is a guess until it is paid.
		amount := l.bill.Expected
		switch l.occ.Status {
		case StatusPaid:
			amount = l.occ.Paid
		case StatusOpen, StatusOverdue:
			// What is actually being chased. An upcoming month is not owed yet,
			// and an archived bill is not owed at all.
			owed[l.bill.Currency] += amount
		}

		shown := core.DimStyle.Render("—")
		if amount > 0 {
			shown = l.bill.Fmt(amount)
		}
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(stateColor(l.occ.Status)))
		t.Row(
			Label(l.bill),
			shown,
			style.Render(marks[l.occ.Status]+" "+l.occ.Status),
			core.DimStyle.Render(when(l.occ)))
	}

	// The total closes the table instead of hanging under it, so the figure
	// lands in the column it is a total of and the block reads as one thing.
	// One figure per currency: centavos and satoshis do not add up, and there
	// is no rate anywhere in pecunia to make them.
	if total := core.MoneyLine(owed); total != "" {
		totalRow = len(lines)
		t.Row(core.DimStyle.Render("still owed"), total, "", "")
	}
	return t.Render() + "\n"
}

// cardWidth is the narrowest the details card gets; longer names widen it.
const cardWidth = 44

const (
	createdIcon = "✚" // Dingbats, same block as every other card — no Nerd Font needed.
	updatedIcon = "#"
	dividerRune = "─"
)

// Details renders one bill as a card bordered in the colour of where its cycle
// stands, with what it has really cost underneath.
//
// paid is the bill's payments, newest first, and may be nil — a bill nobody has
// paid yet has no history to show and no average to take.
func Details(b Bill, paid []transactions.Transaction, today time.Time) string {
	occ := b.Current(today)
	accent := lipgloss.Color(stateColor(occ.Status))

	head := b.Name
	if !b.Active {
		head += " " + core.DimStyle.Render("(archived)")
	}
	lines := []string{code(b) + " " + lipgloss.NewStyle().Bold(true).Render(head)}
	if b.Description != "" {
		lines = append(lines, core.DimStyle.Render(b.Description))
	}

	amount := core.DimStyle.Render("no amount yet")
	if b.Expected > 0 {
		amount = lipgloss.NewStyle().Bold(true).Render(b.Fmt(b.Expected)) +
			core.DimStyle.Render(" expected")
	}
	lines = append(lines, "", amount,
		lipgloss.NewStyle().Foreground(accent).Render(state(occ)),
		core.DimStyle.Render(fmt.Sprintf("cycle %s · opens %s · due %s",
			occ.Cycle, occ.OpenOn, occ.DueOn)),
		"",
		core.DimStyle.Render("paid from ")+tag(b.Source())+" "+core.DimStyle.Render(b.Source().Name))

	if b.Category.ID != 0 {
		lines = append(lines, core.DimStyle.Render("filed under ")+tag(b.Category)+
			" "+core.DimStyle.Render(b.Category.Name))
	}
	if len(b.Tags) > 0 {
		lines = append(lines, core.DimStyle.Render("#"+strings.Join(b.Tags, "  #")))
	}
	if b.CreatedAt != "" {
		lines = append(lines, "", core.DimStyle.Render(
			createdIcon+" "+b.CreatedAt+"   "+updatedIcon+" "+b.UpdatedAt))
	}

	// What it has really cost, which is the whole reason to look at an energy
	// bill: the expected amount is a guess and these are the numbers.
	if entries := history(b, paid); len(entries) > 0 {
		lines = append(lines, rule(append(slices.Clone(lines), entries...)))
		lines = append(lines, entries...)
	}

	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	// Width covers the padding too, so the content gets it back — without the +4
	// the longest line wraps.
	w := max(lipgloss.Width(body)+4, cardWidth)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(0, 2).
		Width(w).
		Render(body) + "\n"
}

// MaxHistory is how many payments the detail view shows and averages over. A
// year is what makes a monthly bill's swing legible — and the user asked for
// twelve.
const MaxHistory = 12

// history is the last year of payments and what they averaged, or nothing at
// all when there are none.
func history(b Bill, paid []transactions.Transaction) []string {
	if len(paid) > MaxHistory {
		paid = paid[:MaxHistory]
	}
	if len(paid) == 0 {
		return nil
	}

	s := Stats(paid)
	lines := []string{
		core.DimStyle.Render(fmt.Sprintf("last %s", plural(s.Count, "payment"))),
		b.Fmt(s.Avg) + core.DimStyle.Render(" average") +
			core.DimStyle.Render("   "+b.Fmt(s.Min)+" – "+b.Fmt(s.Max)),
		"",
	}
	for _, t := range paid {
		// The date it was paid, then the month it was for when those differ —
		// which is exactly the late payment this module exists to keep straight.
		when := core.DimStyle.Render(t.Date)
		if t.Cycle != "" && t.Cycle != CycleOf(t.Date) {
			when += core.DimStyle.Render(" (for " + t.Cycle + ")")
		}
		lines = append(lines, when+"  "+b.Fmt(t.Value))
	}
	return lines
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

// tag is how one reference reads inline: its code in its own colour, bracketed
// like everywhere else in pecunia.
func tag(r transactions.Ref) string {
	if r.ID == 0 {
		return ""
	}
	return "[" + lipgloss.NewStyle().Foreground(lipgloss.Color(r.Col().Hex)).
		Bold(true).Render(r.Code) + "]"
}

// pickerRow is how one bill reads in the picker list.
func pickerRow(b Bill) core.Choice {
	desc := b.Fmt(b.Expected) + "  " + core.DimStyle.Render(state(b.Current(time.Now())))
	if b.Expected == 0 {
		desc = core.DimStyle.Render(state(b.Current(time.Now())))
	}
	return core.Choice{
		Label:  Label(b),
		Desc:   desc,
		Filter: strings.Join(append([]string{b.Code, b.Name, b.Description}, b.Tags...), " "),
	}
}

// Pick shows the list used whenever a command is given no CODE.
func Pick(bs []Bill, title string) (Bill, error) { return core.Pick(bs, title, pickerRow) }

// FormData is everything the form offers to choose from. The command layer
// gathers it, so the form itself needs no store.
type FormData struct {
	Accounts   []accounts.Account
	Cards      []cards.Card
	Categories []categories.Category
	Tags       []string // every tag already in use, for the autocomplete
}

func sourceValue(kind string, id int64) string { return kind + ":" + strconv.FormatInt(id, 10) }

func parseSource(v string) (transactions.Source, error) {
	kind, id, ok := strings.Cut(v, ":")
	if !ok || (kind != "account" && kind != "card") {
		return transactions.Source{}, errors.New("pick an account or a credit card")
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n == 0 {
		return transactions.Source{}, errors.New("pick an account or a credit card")
	}
	return transactions.Source{ID: n, IsCard: kind == "card"}, nil
}

// currency looks up what the chosen source is denominated in, which is the
// scale the expected amount is read at.
func (d FormData) currency(s transactions.Source) string {
	if s.IsCard {
		for _, c := range d.Cards {
			if c.ID == s.ID {
				return c.Currency
			}
		}
		return ""
	}
	for _, a := range d.Accounts {
		if a.ID == s.ID {
			return a.Currency
		}
	}
	return ""
}

// sourceOptions merges accounts and cards into one select, the same way the
// transaction form does — so "exactly one source" is not a rule anything has to
// check, there is only ever one answer. Frozen accounts are left out: a bill
// paid from an account that is out of play is almost always a slip.
func (d FormData) sourceOptions() []huh.Option[string] {
	var opts []huh.Option[string]
	for _, a := range d.Accounts {
		if a.IsFrozen {
			continue
		}
		opts = append(opts, huh.NewOption(accounts.Label(a), sourceValue("account", a.ID)))
	}
	for _, c := range d.Cards {
		opts = append(opts, huh.NewOption(cards.Label(c), sourceValue("card", c.ID)))
	}
	return opts
}

func (d FormData) categoryOptions() []huh.Option[int64] {
	opts := []huh.Option[int64]{huh.NewOption(core.DimStyle.Render("— none —"), int64(0))}
	for _, c := range d.Categories {
		opts = append(opts, huh.NewOption(categories.Label(c), c.ID))
	}
	return opts
}

// Form drives create and edit. On create, pass a Bill pre-filled with a
// suggested code and the colour default.
func Form(d FormData, b *Bill, title string) error {
	if len(d.sourceOptions()) == 0 {
		return errors.New("nothing to pay this from — create an account with: pecunia ac n")
	}

	source := sourceValue("account", b.Account.ID)
	if b.IsCard() {
		source = sourceValue("card", b.Card.ID)
	} else if b.Account.ID == 0 {
		source = ""
	}
	category := b.Category.ID
	openDay, dueDay := strconv.Itoa(b.OpenDay), strconv.Itoa(b.DueDay)
	// Blank rather than "0.00" on a new bill: a bill nobody has seen a number
	// for yet has no expected amount, and that is a real answer.
	expected := ""
	if b.Expected != 0 {
		expected = core.FormatAmount(b.Expected, b.Cur())
	}
	var reused []string
	var fresh string

	fields := []huh.Field{
		huh.NewInput().Title("Code").Description("how you will type it: pecunia bill ENERG").
			Value(&b.Code).Validate(func(v string) error { return core.ValidateCode(v) }),
		huh.NewInput().Title("Name").Value(&b.Name).Validate(core.ValidateName),
		huh.NewInput().Title("Description").Description("optional").Value(&b.Description),
		huh.NewSelect[string]().Title("Colour").Options(core.ColorOptions()...).Value(&b.Color),
		huh.NewSelect[string]().Title("Paid from").Options(d.sourceOptions()...).Value(&source),
		// The source comes first so this validator knows the scale to read the
		// amount at — the same ordering cards.Form uses for currency-before-limit.
		huh.NewInput().Title("Usual amount").
			Description("optional — what it normally costs, to fill the form in when you pay").
			Value(&expected).Validate(func(v string) error {
			s, err := parseSource(source)
			if err != nil {
				return err
			}
			n, err := core.ParseAmount(v, core.CurrencyByCode(d.currency(s)))
			if err != nil {
				return err
			}
			if n < 0 {
				return errors.New("what a bill costs cannot be negative")
			}
			return nil
		}),
		huh.NewInput().Title("Available from").Description("day of the month it can be paid").
			Value(&openDay).Validate(validateDayField),
		huh.NewInput().Title("Overdue after").
			Description("day of the month it is late — before the day above means the month after").
			Value(&dueDay).Validate(validateDayField),
		huh.NewSelect[int64]().Title("Category").Options(d.categoryOptions()...).Value(&category),
	}

	// Nothing to reuse on a database whose first tag this is, and an empty
	// select is worse than no select.
	if len(d.Tags) > 0 {
		reused = keep(b.Tags, d.Tags)
		fields = append(fields, huh.NewMultiSelect[string]().
			Title("Tags").Description("/ filters the tags already in use").
			Options(huh.NewOptions(d.Tags...)...).
			Limit(transactions.MaxTags).Filterable(true).Value(&reused))
	}
	fields = append(fields, huh.NewInput().
		Title("New tags").Description("comma separated — for tags not in the list above").
		Value(&fresh))

	if err := huh.NewForm(huh.NewGroup(fields...).Title(title)).
		WithTheme(huh.ThemeCharm()).Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return core.ErrCancelled
		}
		return err
	}

	s, err := parseSource(source)
	if err != nil {
		return err
	}
	b.Code = core.NormalizeCode(b.Code)
	b.Name = strings.TrimSpace(b.Name)
	b.Account, b.Card = transactions.Ref{}, transactions.Ref{}
	if s.IsCard {
		b.Card = transactions.Ref{ID: s.ID}
	} else {
		b.Account = transactions.Ref{ID: s.ID}
	}
	b.Currency = d.currency(s)
	b.Category = transactions.Ref{ID: category}
	b.Tags = transactions.NormalizeTags(append(reused, transactions.ParseTags(fresh)...))
	// huh skips its validators when stdin ends mid-form, so anything unreadable
	// is left as it was rather than written through — and the store refuses what
	// a new bill started with.
	if n, err := core.ParseAmount(expected, b.Cur()); err == nil {
		b.Expected = n
	}
	if n, err := cards.ParseDay(openDay); err == nil {
		b.OpenDay = n
	}
	if n, err := cards.ParseDay(dueDay); err == nil {
		b.DueDay = n
	}
	return nil
}

func validateDayField(v string) error {
	_, err := cards.ParseDay(v)
	return err
}

// keep drops the values that are no longer on offer, so an edit's pre-selected
// tags never include one the multi-select has no option for.
func keep(want, offered []string) []string {
	var out []string
	for _, w := range want {
		if slices.Contains(offered, w) {
			out = append(out, w)
		}
	}
	return out
}
