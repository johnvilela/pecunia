package core

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// ErrCancelled means the user backed out of a form or picker; callers treat it
// as "do nothing", not as a failure.
var ErrCancelled = errors.New("cancelled")

// DimColor is the gray everything out of play is rendered in: table borders,
// headers, and anything frozen.
const DimColor = "245"

var (
	HeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(DimColor))
	DimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(DimColor))
)

// ColorOptions is the palette as a huh select, each entry prefixed with a
// swatch in its own color.
func ColorOptions() []huh.Option[string] {
	opts := make([]huh.Option[string], len(Palette))
	for i, c := range Palette {
		swatch := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Hex)).Render("███")
		opts[i] = huh.NewOption(swatch+" "+c.Name, c.Name)
	}
	return opts
}

func CurrencyOptions() []huh.Option[string] {
	opts := make([]huh.Option[string], len(Currencies))
	for i, c := range Currencies {
		opts[i] = huh.NewOption(fmt.Sprintf("%s  %s (%s)", c.Symbol, c.Label, c.Code), c.Code)
	}
	return opts
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

// Choice is how one row of the picker reads: the line, the line under it, and
// the text the filter matches against.
type Choice struct{ Label, Desc, Filter string }

// choice pairs a value with its rendered row so the list can carry it.
type choice[T any] struct {
	v T
	c Choice
}

func (i choice[T]) Title() string       { return i.c.Label }
func (i choice[T]) Description() string { return i.c.Desc }
func (i choice[T]) FilterValue() string { return i.c.Filter }

type picker[T any] struct {
	list   list.Model
	chosen *T
}

func (p picker[T]) Init() tea.Cmd { return nil }

func (p picker[T]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.list.SetSize(msg.Width, msg.Height)
	case tea.KeyMsg:
		if p.list.FilterState() != list.Filtering {
			switch msg.String() {
			case "ctrl+c", "esc", "q":
				return p, tea.Quit
			case "enter":
				if it, ok := p.list.SelectedItem().(choice[T]); ok {
					p.chosen = &it.v
				}
				return p, tea.Quit
			}
		}
	}
	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

func (p picker[T]) View() string { return p.list.View() }

// Pick shows the bubbles list used whenever a command is given no {CODE|ID}.
// row says how each item renders; everything else is the same for every module.
func Pick[T any](items []T, title string, row func(T) Choice) (T, error) {
	var zero T

	rows := make([]list.Item, len(items))
	for i, v := range items {
		rows[i] = choice[T]{v: v, c: row(v)}
	}
	l := list.New(rows, list.NewDefaultDelegate(), 0, 0)
	l.Title = title
	l.SetShowStatusBar(false)

	m, err := tea.NewProgram(picker[T]{list: l}, tea.WithAltScreen()).Run()
	if err != nil {
		return zero, err
	}
	if p, ok := m.(picker[T]); ok && p.chosen != nil {
		return *p.chosen, nil
	}
	return zero, ErrCancelled
}
