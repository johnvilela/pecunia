package logs

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"pecunia/internal/core"
)

// Table is the static trail output — no alt screen, so `pecunia l | grep` works.
// Nil comes back empty rather than as a header with nothing under it, so the
// caller can say why there is nothing instead.
func Table(es []Entry) string {
	if len(es) == 0 {
		return ""
	}
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(core.DimStyle).
		Headers("WHEN", "SOURCE", "ACTION", "ENTITY", "ID", "CHANGES").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return core.HeaderStyle.Padding(0, 1)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})
	for _, e := range es {
		t.Row(e.CreatedAt, e.Source, e.Action, e.Entity,
			fmt.Sprint(e.EntityID), moves(e.Changes))
	}
	return t.Render()
}

// moves is the changes JSON as one readable line: "name: Cash → Wallet",
// sorted so the same edit always reads the same way. Anything unreadable —
// there should be nothing — comes back as it is rather than hiding the row.
func moves(changes string) string {
	if changes == "" {
		return ""
	}
	var parsed map[string]Change
	if err := json.Unmarshal([]byte(changes), &parsed); err != nil {
		return changes
	}
	keys := make([]string, 0, len(parsed))
	for k := range parsed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += core.DimStyle.Render(", ")
		}
		c := parsed[k]
		out += k + ": " + fmt.Sprint(c.Old) + core.DimStyle.Render(" → ") + fmt.Sprint(c.New)
	}
	return out
}
