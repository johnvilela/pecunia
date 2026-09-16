package notes

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pecunia/internal/core"
	"pecunia/internal/transactions"
)

// The front matter is a fixed set of keys between two --- lines, read by a
// parser of its own rather than a YAML library: the format is pecunia's, the
// canonical form is always written by pecunia, and what a hand edit can get
// wrong — a misspelled key, an unclosed list — deserves an error naming the
// line rather than a silent default. The subset accepted is what an editor
// produces: key: value, quoted or not, inline [a, b] lists, block "- a" lists,
// blank lines and # comments.

// keys, in the order the canonical form writes them.
var keys = []string{"title", "priority", "status", "target", "tags", "accounts", "cards", "goals"}

var listKeys = map[string]bool{"tags": true, "accounts": true, "cards": true, "goals": true}

var keyLine = regexp.MustCompile(`^([a-z_]+):(.*)$`)

// Doc is a file as read: the front matter's raw values, the body and where the
// body starts. Nothing is normalised here — Note does that — so what the
// owner typed is still visible to the error that refuses it.
type Doc struct {
	Title, Priority, Status string
	// Target is the value as written: a day or a phrase. TargetPhrase is the
	// comment beside a day, which is where the canonical form keeps the phrase
	// the day came from.
	Target, TargetPhrase         string
	Tags, Accounts, Cards, Goals []string
	Body                         string
	// BodyLine is the 1-based line the body starts on — the line an editor is
	// opened at.
	BodyLine int
}

// ParseFile reads one note file, naming it in any error.
func ParseFile(path string) (Doc, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return Doc{}, err
	}
	d, err := Parse(src)
	if err != nil {
		return Doc{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return d, nil
}

// Parse reads a note file's front matter and body.
func Parse(src []byte) (Doc, error) {
	text := strings.TrimPrefix(string(src), "\ufeff")
	text = strings.TrimSuffix(text, "\n")
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], "\r")
	}
	if strings.TrimRight(lines[0], " \t") != "---" {
		return Doc{}, fmt.Errorf("no front matter — the file must start with a line that is just ---")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return Doc{}, fmt.Errorf("front matter never closes — missing the second ---")
	}

	var d Doc
	lists := map[string]*[]string{"tags": &d.Tags, "accounts": &d.Accounts, "cards": &d.Cards, "goals": &d.Goals}
	scalars := map[string]*string{"title": &d.Title, "priority": &d.Priority, "status": &d.Status, "target": &d.Target}
	seen := map[string]bool{}
	open := "" // the list key a block "- item" line belongs to, if any
	for i := 1; i < end; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "-" || strings.HasPrefix(line, "- ") {
			if open == "" {
				return Doc{}, fmt.Errorf("line %d: cannot read %q — expected key: value", i+1, line)
			}
			item, _ := stripComment(strings.TrimSpace(line[1:]))
			if item = unquote(item); item != "" {
				*lists[open] = append(*lists[open], item)
			}
			continue
		}
		m := keyLine.FindStringSubmatch(line)
		if m == nil {
			return Doc{}, fmt.Errorf("line %d: cannot read %q — expected key: value", i+1, line)
		}
		key := m[1]
		if lists[key] == nil && scalars[key] == nil {
			return Doc{}, fmt.Errorf("line %d: unknown key %q — known keys: %s", i+1, key, strings.Join(keys, ", "))
		}
		if seen[key] {
			return Doc{}, fmt.Errorf("line %d: key %q appears twice", i+1, key)
		}
		seen[key] = true
		open = ""
		value, comment := stripComment(strings.TrimSpace(m[2]))

		if listKeys[key] {
			switch {
			case value == "":
				*lists[key] = []string{}
				open = key
			case strings.HasPrefix(value, "["):
				if !strings.HasSuffix(value, "]") {
					return Doc{}, fmt.Errorf("line %d: list never closes — missing ]", i+1)
				}
				*lists[key] = splitList(value[1 : len(value)-1])
			default:
				*lists[key] = []string{unquote(value)}
			}
			continue
		}
		*scalars[key] = unquote(value)
		if key == "target" {
			d.TargetPhrase = comment
		}
	}

	rest := lines[end+1:]
	d.BodyLine = end + 2
	if len(rest) > 1 && rest[0] == "" {
		rest = rest[1:]
		d.BodyLine++
	}
	d.Body = strings.TrimRight(strings.Join(rest, "\n"), " \t\n")
	return d, nil
}

// stripComment splits a value from the comment after it. A # starts a comment
// only at the start or after whitespace, and never inside quotes — "Issue#12"
// is a title, `"Hash # here"` is a quoted one.
func stripComment(s string) (value, comment string) {
	quote := rune(0)
	for i, r := range s {
		switch {
		case quote == '"' && r == '\\':
			continue
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
		}
	}
	return strings.TrimSpace(s), ""
}

// unquote takes the quotes off a scalar: "..." with \" and \\ escapes, or
// '...' with ” for a quote. Anything else is itself.
func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	switch {
	case s[0] == '"' && s[len(s)-1] == '"':
		inner := s[1 : len(s)-1]
		var b strings.Builder
		for i := 0; i < len(inner); i++ {
			if inner[i] == '\\' && i+1 < len(inner) && (inner[i+1] == '"' || inner[i+1] == '\\') {
				i++
			}
			b.WriteByte(inner[i])
		}
		return b.String()
	case s[0] == '\'' && s[len(s)-1] == '\'':
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	return s
}

// splitList reads the inside of [a, b, "c, d"], commas outside quotes
// separating the items.
func splitList(s string) []string {
	out := []string{}
	quote := rune(0)
	start := 0
	flush := func(end int) {
		if item := unquote(strings.TrimSpace(s[start:end])); item != "" {
			out = append(out, item)
		}
	}
	for i, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == ',':
			flush(i)
			start = i + 1
		}
	}
	flush(len(s))
	return out
}

// Note normalises a parsed file into a note ready for the store: trimmed,
// lowercased where case does not matter, defaults filled, the target resolved
// against now, and validated.
func (d Doc) Note(now time.Time) (Note, error) {
	n := Note{
		Title:    strings.TrimSpace(d.Title),
		Priority: strings.ToLower(strings.TrimSpace(d.Priority)),
		Status:   strings.ToLower(strings.TrimSpace(d.Status)),
	}
	if n.Priority == "" {
		n.Priority = PriorityMedium
	}
	if n.Status == "" {
		n.Status = StatusOpen
	}

	target, phrase := strings.TrimSpace(d.Target), strings.TrimSpace(d.TargetPhrase)
	if iso, err := transactions.ParseDate(target); err == nil {
		n.Target, n.TargetPhrase = iso, phrase
	} else if target != "" {
		iso, err := ParseWhen(target, now)
		if err != nil {
			return Note{}, fmt.Errorf("target: %w", err)
		}
		n.Target, n.TargetPhrase = iso, target
	}
	if n.TargetPhrase == n.Target {
		n.TargetPhrase = ""
	}

	n.Tags = transactions.NormalizeTags(d.Tags)
	n.Accounts = codes(d.Accounts)
	n.Cards = codes(d.Cards)
	for _, g := range d.Goals {
		id, err := strconv.ParseInt(strings.TrimSpace(g), 10, 64)
		if err != nil || id < 1 {
			return Note{}, fmt.Errorf("goals: %q is not a goal id", g)
		}
		n.Goals = append(n.Goals, id)
	}
	return n, n.Validate()
}

// codes normalises a list of account or card codes: uppercased, trimmed,
// deduped, empties dropped, nil when nothing is left.
func codes(in []string) []string {
	var out []string
	for _, s := range in {
		c := core.NormalizeCode(s)
		if c == "" {
			continue
		}
		dup := false
		for _, have := range out {
			if have == c {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, c)
		}
	}
	return out
}

// Render is the canonical file: every key, in order, lists inline, the target
// as a day with its phrase beside it as a comment, one blank line, then the
// body. Parse(Render(n, body)) gives n and body back.
func Render(n Note, body string) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: " + scalar(n.Title) + "\n")
	b.WriteString("priority: " + n.Priority + "\n")
	b.WriteString("status: " + n.Status + "\n")
	b.WriteString("target:" + targetValue(n) + "\n")
	b.WriteString("tags: " + list(n.Tags) + "\n")
	b.WriteString("accounts: " + list(n.Accounts) + "\n")
	b.WriteString("cards: " + list(n.Cards) + "\n")
	b.WriteString("goals: " + list(ints(n.Goals)) + "\n")
	b.WriteString("---\n\n")
	if body = strings.TrimRight(body, " \t\n"); body != "" {
		b.WriteString(body + "\n")
	}
	return []byte(b.String())
}

// draftLine is the column the hints start at in a draft.
const draftLine = 31

// RenderDraft is the file a new note opens as: the values so far, a hint
// beside each key, and an empty line for the body. The second value is that
// line, for the editor to open at.
func RenderDraft(n Note) ([]byte, int) {
	target := n.TargetPhrase
	if target == "" {
		target = n.Target
	}
	title := "title:"
	if n.Title != "" {
		title += " " + scalar(n.Title)
	}
	rows := []struct{ value, hint string }{
		{"priority: " + n.Priority, "low | medium | high | critical"},
		{"status: " + n.Status, "open | doing | done | dropped"},
		{"target: " + target, "today | tomorrow | next month | in 3 months | 2026-12 | 16/12/2026"},
		{"tags: " + list(n.Tags), "e.g. [health, insurance]"},
		{"accounts: " + list(n.Accounts), "account codes, e.g. [INTER]"},
		{"cards: " + list(n.Cards), "card codes"},
		{"goals: " + list(ints(n.Goals)), "goal ids, e.g. [3]"},
	}
	var b strings.Builder
	b.WriteString("---\n" + title + "\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "%-*s # %s\n", draftLine-1, r.value, r.hint)
	}
	b.WriteString("---\n\n")
	return []byte(b.String()), 3 + len(rows) + 1
}

func targetValue(n Note) string {
	if n.Target == "" {
		return ""
	}
	if n.TargetPhrase != "" && n.TargetPhrase != n.Target {
		return " " + n.Target + " # " + n.TargetPhrase
	}
	return " " + n.Target
}

func list(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = scalar(s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func ints(ids []int64) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = strconv.FormatInt(id, 10)
	}
	return out
}

// scalar writes a value, quoting it when a YAML reader — or this parser —
// would otherwise misread it: a colon or hash that could start something,
// brackets, quotes, or whitespace at the edges.
func scalar(s string) string {
	if s == "" || s != strings.TrimSpace(s) || strings.ContainsAny(s, ":#[]{}\"&*!|>%@`") ||
		strings.HasPrefix(s, "'") || strings.HasPrefix(s, "-") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
	}
	return s
}
