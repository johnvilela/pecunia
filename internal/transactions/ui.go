package transactions

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"kakei/internal/accounts"
	"kakei/internal/cards"
	"kakei/internal/categories"
	"kakei/internal/core"
)

// Amount is the signed money, green when it arrives and red when it leaves.
// Unlike a card's balance, a transaction's direction is the whole point of it,
// so it is always coloured.
func Amount(t Transaction) string {
	sign, color := "+", core.ColorByName("green").Hex
	if t.Kind == KindOutcome {
		sign, color = "-", core.ColorByName("red").Hex
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).
		Render(sign + t.Cur().Symbol + t.Amount())
}

// tag is how one reference — a category, an account, a card — reads inline: its
// code in its own colour, bracketed like everywhere else in kakei.
func tag(r Ref) string {
	if r.ID == 0 {
		return ""
	}
	code := lipgloss.NewStyle().Foreground(lipgloss.Color(r.Col().Hex)).Bold(true).Render(r.Code)
	return "[" + code + "]"
}

// Label is how a transaction is named in a list: when it happened, then what it
// was. A transaction has no code of its own to lead with.
func Label(t Transaction) string {
	return core.DimStyle.Render(t.Date) + "  " + t.Title
}

// sourceKind names what the money moved through, for the places with room to say
// it in words.
func sourceKind(t Transaction) string {
	if t.IsCard() {
		return "credit card"
	}
	return "account"
}

// Table is the static list output — no alt screen, so `kakei t | grep` works.
func Table(ts []Transaction) string {
	tbl := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(core.DimStyle).
		Headers("DATE", "TITLE", "CATEGORY", "SOURCE", "AMOUNT").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return core.HeaderStyle.Padding(0, 1)
			}
			// The amount is the column the eye runs down, so it is the one that
			// gets right-aligned.
			if col == 4 {
				return lipgloss.NewStyle().Padding(0, 1).Align(lipgloss.Right)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})
	for _, t := range ts {
		tbl.Row(core.DimStyle.Render(t.Date), t.Title, tag(t.Category), tag(t.Target()), Amount(t))
	}
	return tbl.Render()
}

// pickerRow is how one transaction reads in the picker list.
func pickerRow(t Transaction) core.Choice {
	return core.Choice{
		Label: Label(t),
		Desc:  Amount(t) + "  " + core.DimStyle.Render(t.Target().Code),
		// Everything someone might remember the transaction by: what it was,
		// what it was filed under, where it came from, what it was tagged.
		Filter: strings.Join(append([]string{
			t.Title, t.Description, t.Category.Code, t.Target().Code, t.Date,
		}, t.Tags...), " "),
	}
}

// Pick shows the list used whenever a command is given no {ID}.
func Pick(ts []Transaction, title string) (Transaction, error) {
	return core.Pick(ts, title, pickerRow)
}

// cardWidth is the narrowest the details card gets; longer titles widen it.
const cardWidth = 40

const (
	createdIcon = "✚" // Dingbats, same block as every other card — no Nerd Font needed.
	updatedIcon = "#"
)

// Details renders one transaction as a card. It borrows the category's colour,
// falling back to the account's or card's, so the card is recognisable before a
// word of it is read.
func Details(t Transaction) string {
	accent := lipgloss.Color(t.Category.Col().Hex)
	if t.Category.ID == 0 {
		accent = lipgloss.Color(t.Target().Col().Hex)
	}

	lines := []string{lipgloss.NewStyle().Bold(true).Render(t.Title)}
	if t.Description != "" {
		lines = append(lines, core.DimStyle.Render(t.Description))
	}
	lines = append(lines, "", lipgloss.NewStyle().Bold(true).Render(Amount(t)), "")

	if t.Category.ID != 0 {
		lines = append(lines, tag(t.Category)+" "+core.DimStyle.Render(t.Category.Name))
	}
	lines = append(lines,
		tag(t.Target())+" "+core.DimStyle.Render(t.Target().Name+" ("+sourceKind(t)+")"),
		core.DimStyle.Render(t.Date))

	if len(t.Tags) > 0 {
		lines = append(lines, "", core.DimStyle.Render("#"+strings.Join(t.Tags, "  #")))
	}
	if t.CreatedAt != "" {
		lines = append(lines, "", core.DimStyle.Render(
			createdIcon+" "+t.CreatedAt+"   "+updatedIcon+" "+t.UpdatedAt))
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

// Source is a chosen target: which of the two tables, and which row in it. The
// form offers accounts and cards in one select, so "exactly one target, never
// both" is not a rule anything has to check — there is only ever one answer.
type Source struct {
	ID     int64
	IsCard bool
}

func sourceValue(kind string, id int64) string { return kind + ":" + strconv.FormatInt(id, 10) }

func parseSource(v string) (Source, error) {
	kind, id, ok := strings.Cut(v, ":")
	if !ok || (kind != "account" && kind != "card") {
		return Source{}, fmt.Errorf("pick an account or a credit card")
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n == 0 {
		return Source{}, fmt.Errorf("pick an account or a credit card")
	}
	return Source{ID: n, IsCard: kind == "card"}, nil
}

// FormData is everything the form offers to choose from. The command layer
// gathers it, so the form itself needs no store.
type FormData struct {
	Accounts   []accounts.Account
	Cards      []cards.Card
	Categories []categories.Category
	Tags       []string // every tag already in use, for the autocomplete
}

// currency looks up what the chosen source is denominated in, which is the scale
// the typed amount is read at.
func (d FormData) currency(s Source) string {
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

// sourceOptions merges accounts and cards into one select. Frozen accounts are
// left out: a frozen account is out of play, and filing new money through it is
// almost always a slip.
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

// Form drives create and edit.
func Form(d FormData, t *Transaction, title string) error {
	if len(d.sourceOptions()) == 0 {
		return errors.New("nothing to file this against — create an account with: kakei ac n")
	}

	source := sourceValue("account", t.Account.ID)
	if t.IsCard() {
		source = sourceValue("card", t.Card.ID)
	}
	category := t.Category.ID
	date := t.Date
	if date == "" {
		date = Today()
	}
	// The amount starts blank on a new transaction rather than at "0.00", which
	// would have to be cleared before anything could be typed.
	amount := ""
	if t.Value != 0 {
		amount = core.FormatAmount(t.Value, t.Cur())
	}
	var reused []string
	var fresh string

	fields := []huh.Field{
		huh.NewInput().Title("Title").Value(&t.Title).Validate(ValidateTitle),
		huh.NewInput().Title("Description").Description("optional").Value(&t.Description),
		huh.NewInput().Title("Date").Description(DateLayout).Value(&date).
			Validate(func(v string) error {
				_, err := ParseDate(v)
				return err
			}),
		huh.NewSelect[string]().Title("Kind").Value(&t.Kind).Options(
			huh.NewOption("Outcome — money out", KindOutcome),
			huh.NewOption("Income — money in", KindIncome)),
		huh.NewSelect[string]().Title("Account or credit card").
			Options(d.sourceOptions()...).Value(&source),
		// The source comes first so this validator knows the scale to read the
		// amount at — the same ordering cards.Form uses for currency-before-limit.
		huh.NewInput().Title("Amount").Description("what it was worth, no sign").
			Value(&amount).Validate(func(v string) error {
			s, err := parseSource(source)
			if err != nil {
				return err
			}
			n, err := core.ParseAmount(v, core.CurrencyByCode(d.currency(s)))
			if err != nil {
				return err
			}
			if n <= 0 {
				return errors.New("amount must be more than zero")
			}
			return nil
		}),
		huh.NewSelect[int64]().Title("Category").Options(d.categoryOptions()...).Value(&category),
	}

	// Nothing to reuse on a database whose first transaction this is, and an
	// empty select is worse than no select.
	if len(d.Tags) > 0 {
		reused = pick(t.Tags, d.Tags)
		fields = append(fields, huh.NewMultiSelect[string]().
			Title("Tags").Description("/ filters the tags already in use").
			Options(huh.NewOptions(d.Tags...)...).
			Limit(MaxTags).Filterable(true).Value(&reused))
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
	value, err := core.ParseAmount(amount, core.CurrencyByCode(d.currency(s)))
	if err != nil {
		return err
	}
	// huh skips its validators when stdin ends mid-form, so an unparseable date
	// is left as whatever was there rather than written through — and the store
	// refuses the blank one a new transaction started with.
	if parsed, err := ParseDate(date); err == nil {
		t.Date = parsed
	}

	t.Title = strings.TrimSpace(t.Title)
	t.Value = value
	t.Account, t.Card = Ref{}, Ref{}
	if s.IsCard {
		t.Card = Ref{ID: s.ID}
	} else {
		t.Account = Ref{ID: s.ID}
	}
	t.Currency = d.currency(s)
	t.Category = Ref{ID: category}
	t.Tags = NormalizeTags(append(reused, ParseTags(fresh)...))
	return nil
}

// pick keeps only the values that are still on offer, so an edit's pre-selected
// tags do not include one the multi-select has no option for.
func pick(want, offered []string) []string {
	var out []string
	for _, w := range want {
		for _, o := range offered {
			if w == o {
				out = append(out, w)
				break
			}
		}
	}
	return out
}
