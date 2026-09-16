package notes

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"pecunia/internal/core"
)

// levelColor is the effective level's colour: nothing for medium, which is the
// resting state, dim for low, and the two warm palette colours for the levels
// worth a glance. The base priority is never coloured — it is the owner's
// word, and the colour is pecunia's reading of it.
func levelColor(level string) string {
	switch level {
	case PriorityLow:
		return core.DimColor
	case PriorityHigh:
		return core.ColorByName("orange").Hex
	case PriorityCritical:
		return core.ColorByName("red").Hex
	}
	return ""
}

func levelStyle(level string) lipgloss.Style {
	s := lipgloss.NewStyle().Bold(true)
	if c := levelColor(level); c != "" {
		s = s.Foreground(lipgloss.Color(c))
	}
	return s
}

// Priority is the base and the effective level side by side, with the score:
// "LOW → HIGH 74". When they agree the arrow goes — "LOW 20" — since a note
// that reads as it was filed has nothing to explain.
func Priority(n Note) string {
	out := levelStyle(n.Level).Render(strings.ToUpper(n.Level))
	if n.Level != n.Priority && n.Level != "" {
		out = strings.ToUpper(n.Priority) + core.DimStyle.Render(" → ") + out
	}
	return out + " " + core.DimStyle.Render(strconv.Itoa(n.Score))
}

// Due is the target and how far off it is: "2026-12-16  in 91d",
// "2026-09-13  3d overdue", or a dash for none.
func Due(n Note, now time.Time) string {
	days, ok := n.DaysToTarget(now)
	if !ok {
		return "—"
	}
	var rel string
	switch {
	case days < 0:
		rel = fmt.Sprintf("%dd overdue", -days)
	case days == 0:
		rel = "today"
	case days == 1:
		rel = "tomorrow"
	default:
		rel = fmt.Sprintf("in %dd", days)
	}
	return n.Target + "  " + rel
}

func dueStyled(n Note, now time.Time) string {
	s := Due(n, now)
	if days, ok := n.DaysToTarget(now); ok && days < 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(core.ColorByName("red").Hex)).Render(s)
	}
	return s
}

func tags(n Note) string {
	if len(n.Tags) == 0 {
		return ""
	}
	return core.DimStyle.Render("#" + strings.Join(n.Tags, "  #"))
}

// Label is how a note is named in a picker: the id, the title, the levels.
func Label(n Note) string {
	return core.DimStyle.Render("#"+strconv.FormatInt(n.ID, 10)) + "  " + n.Title + "  " + Priority(n)
}

func pickerRow(n Note) core.Choice {
	desc := n.Status
	if n.Target != "" {
		desc += "  " + Due(n, time.Now())
	}
	if t := tags(n); t != "" {
		desc += "  " + t
	}
	return core.Choice{Label: Label(n), Desc: desc, Filter: n.Title + " " + strings.Join(n.Tags, " ")}
}

// Pick shows the list used whenever a command is given no ID.
func Pick(ns []Note, title string) (Note, error) { return core.Pick(ns, title, pickerRow) }

// problemMark flags a row whose file could not be read; the reason is printed
// under the table.
const problemMark = "!"

// Table is the list: highest effective priority first, as the caller sorted
// it. A note whose file is missing or broken is still a row — marked, with
// the reason under the table — rather than a hole in the list.
func Table(ns []Note, now time.Time) string {
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(core.DimStyle).
		Headers("#", "PRIORITY", "SCORE", "STATUS", "TITLE", "TARGET", "TAGS").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return core.HeaderStyle.Padding(0, 1)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})
	var problems []string
	for _, n := range ns {
		id := strconv.FormatInt(n.ID, 10)
		if n.Problem != "" {
			id += lipgloss.NewStyle().Foreground(lipgloss.Color(core.ColorByName("red").Hex)).Render(problemMark)
			problems = append(problems, problemMark+" "+strconv.FormatInt(n.ID, 10)+": "+n.Problem)
		}
		t.Row(id, priorityCell(n), strconv.Itoa(n.Score), n.Status, n.Title, dueStyled(n, now), tags(n))
	}
	out := t.Render()
	if len(problems) > 0 {
		out += "\n" + strings.Join(problems, "\n")
	}
	return out
}

// priorityCell is Priority without the score, which has a column of its own.
func priorityCell(n Note) string {
	out := levelStyle(n.Level).Render(strings.ToUpper(n.Level))
	if n.Level != n.Priority && n.Level != "" {
		out = strings.ToUpper(n.Priority) + core.DimStyle.Render(" → ") + out
	}
	return out
}

// Links is what a note is about, spelled for a person: names with their codes.
type Links struct{ Accounts, Cards, Goals []string }

// cardWidth is the narrowest the card gets; longer lines simply widen it.
const cardWidth = 48

const (
	createdIcon = "✚"
	updatedIcon = "#"
)

// Details is one note in full: a card of everything the row knows, then the
// body rendered as markdown under it. The body goes outside the card — a
// rendered document has margins and widths of its own, and a border around
// it would fight them.
func Details(n Note, bd Breakdown, body string, links Links, path string, now time.Time) string {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Render(n.Title),
		Priority(n),
		core.DimStyle.Render(fmt.Sprintf("score %d = base %d + target %d + reads %d + activity %d − stale %d",
			bd.Total, bd.Base, bd.Target, bd.Engagement, bd.Activity, bd.Decay)),
		"",
	}
	status := n.Status
	if n.Target != "" {
		status += core.DimStyle.Render("  ·  ") + dueStyled(n, now)
		if n.TargetPhrase != "" {
			status += core.DimStyle.Render("  (" + n.TargetPhrase + ")")
		}
	}
	lines = append(lines, status)
	if t := tags(n); t != "" {
		lines = append(lines, t)
	}
	for _, l := range []struct {
		name  string
		items []string
	}{{"accounts", links.Accounts}, {"cards", links.Cards}, {"goals", links.Goals}} {
		if len(l.items) > 0 {
			lines = append(lines, core.DimStyle.Render(l.name+"  ")+strings.Join(l.items, core.DimStyle.Render(" · ")))
		}
	}
	lines = append(lines, "",
		core.DimStyle.Render(fmt.Sprintf("read %d×  ·  edited %d×", n.ReadCount, n.EditCount)))
	if n.CreatedAt != "" {
		lines = append(lines, core.DimStyle.Render(createdIcon+" "+n.CreatedAt+"   "+updatedIcon+" "+n.UpdatedAt))
	}
	lines = append(lines, core.DimStyle.Render(path))
	if n.Problem != "" {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(lipgloss.Color(core.ColorByName("red").Hex)).
			Render(problemMark+" "+n.Problem))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	w := lipgloss.Width(content) + 4
	if w < cardWidth {
		w = cardWidth
	}
	accent := lipgloss.Color(levelColor(n.Level))
	if accent == "" {
		accent = lipgloss.Color(core.DimColor)
	}
	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(0, 2).
		Width(w).
		Render(content)

	if strings.TrimSpace(body) == "" {
		return card + "\n" + core.DimStyle.Render(
			fmt.Sprintf("  (empty — pecunia n e %d to write it)", n.ID)) + "\n"
	}
	return card + "\n" + markdown(body)
}

// markdown renders the body for the terminal. If the renderer cannot be made
// — no terminal to ask, an odd TERM — the body is printed as it is: a note
// is never unreadable because of its styling.
func markdown(body string) string {
	r, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(80))
	if err != nil {
		return body + "\n"
	}
	out, err := r.Render(body)
	if err != nil {
		return body + "\n"
	}
	return out
}
