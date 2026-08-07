package accounts

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// ErrCancelled means the user backed out of a form or picker; callers treat it
// as "do nothing", not as a failure.
var ErrCancelled = errors.New("cancelled")

// dimColor is the gray everything out of play is rendered in: table borders,
// headers, and frozen accounts.
const dimColor = "245"

var (
	labelStyle  = lipgloss.NewStyle().Bold(true).Width(12)
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(dimColor))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor))
	frozenMark  = lipgloss.NewStyle().Foreground(lipgloss.Color("#05A2C2")).Render("❄")
)

func styledCode(a Account) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(a.Col().Hex)).Bold(true).Render(a.Code)
}

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
		return dimColor
	}
	return a.Col().Hex
}

// balanceColor is green for a credit and red for a debit. Zero is neither, so
// it keeps the default foreground; a frozen account is dimmed regardless.
func balanceColor(a Account) string {
	switch {
	case a.IsFrozen:
		return dimColor
	case a.Balance > 0:
		return ColorByName("green").Hex
	case a.Balance < 0:
		return ColorByName("red").Hex
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
	original := NormalizeCode(a.Code)
	balance := FormatAmount(a.Balance, a.Cur())

	colorOpts := make([]huh.Option[string], len(Palette))
	for i, c := range Palette {
		swatch := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Hex)).Render("███")
		colorOpts[i] = huh.NewOption(swatch+" "+c.Name, c.Name)
	}
	currencyOpts := make([]huh.Option[string], len(Currencies))
	for i, c := range Currencies {
		currencyOpts[i] = huh.NewOption(fmt.Sprintf("%s  %s (%s)", c.Symbol, c.Label, c.Code), c.Code)
	}

	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Name").Value(&a.Name).
			Validate(func(v string) error {
				if strings.TrimSpace(v) == "" {
					return errors.New("name is required")
				}
				return nil
			}),
		huh.NewInput().Title("Description").Description("optional").Value(&a.Description),
		huh.NewInput().Title("Code").Description(fmt.Sprintf("%d characters — suggestion pre-filled", CodeLen)).
			Value(&a.Code).
			Validate(func(v string) error {
				if err := ValidateCode(v); err != nil {
					return err
				}
				if NormalizeCode(v) == original {
					return nil
				}
				taken, err := s.CodeTaken(v)
				if err != nil {
					return err
				}
				if taken {
					return fmt.Errorf("code %s is already in use", NormalizeCode(v))
				}
				return nil
			}),
		huh.NewSelect[string]().Title("Color").Options(colorOpts...).Value(&a.Color),
		huh.NewSelect[string]().Title("Currency").Options(currencyOpts...).Value(&a.Currency),
		// Currency comes first so this validator knows the scale to parse with.
		huh.NewInput().Title("Balance").Value(&balance).
			Validate(func(v string) error {
				_, err := ParseAmount(v, CurrencyByCode(a.Currency))
				return err
			}),
	).Title(title)).WithTheme(huh.ThemeCharm())

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return ErrCancelled
		}
		return err
	}

	v, err := ParseAmount(balance, CurrencyByCode(a.Currency))
	if err != nil {
		return err
	}
	a.Name = strings.TrimSpace(a.Name)
	a.Code = NormalizeCode(a.Code)
	a.Balance = v
	return nil
}

func Confirm(title, description string) (bool, error) {
	ok := false
	err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(title).Description(description).
			Affirmative("Yes, delete").Negative("Cancel").Value(&ok),
	)).WithTheme(huh.ThemeCharm()).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return false, nil
	}
	return ok, err
}

type pickerItem struct{ a Account }

func (i pickerItem) Title() string { return Label(i.a) }

func (i pickerItem) Description() string {
	c := i.a.Cur()
	return fmt.Sprintf("%s%s %s", c.Symbol, i.a.Amount(), c.Code)
}

func (i pickerItem) FilterValue() string { return i.a.Code + " " + i.a.Name }

type picker struct {
	list   list.Model
	choice *Account
}

func (p picker) Init() tea.Cmd { return nil }

func (p picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.list.SetSize(msg.Width, msg.Height)
	case tea.KeyMsg:
		if p.list.FilterState() != list.Filtering {
			switch msg.String() {
			case "ctrl+c", "esc", "q":
				return p, tea.Quit
			case "enter":
				if it, ok := p.list.SelectedItem().(pickerItem); ok {
					p.choice = &it.a
				}
				return p, tea.Quit
			}
		}
	}
	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

func (p picker) View() string { return p.list.View() }

// Pick shows the bubbles list used whenever a command is given no {CODE|ID}.
func Pick(accs []Account, title string) (Account, error) {
	items := make([]list.Item, len(accs))
	for i, a := range accs {
		items[i] = pickerItem{a}
	}
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = title
	l.SetShowStatusBar(false)

	m, err := tea.NewProgram(picker{list: l}, tea.WithAltScreen()).Run()
	if err != nil {
		return Account{}, err
	}
	if p, ok := m.(picker); ok && p.choice != nil {
		return *p.choice, nil
	}
	return Account{}, ErrCancelled
}

// Table is the static list output — no alt screen, so `kakei ac | grep` works.
func Table(accs []Account) string {
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(dimStyle).
		Headers("ACCOUNT", "BALANCE").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle.Padding(0, 1)
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
		lines = append(lines, dimStyle.Render(a.Description))
	}
	// The amount carries the sign color; the code beside it is the only place
	// the currency is spelled out.
	lines = append(lines, "",
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(balanceColor(a))).
			Render(a.Cur().Symbol+a.Amount())+" "+dimStyle.Render(a.Cur().Code),
		"")

	// The ❄ beside the code already says whether the account is frozen, and the
	// id is only ever typed by someone who read it off `kakei ac`.
	if a.CreatedAt != "" {
		lines = append(lines, dimStyle.Render(
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
