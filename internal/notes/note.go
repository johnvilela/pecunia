// Package notes holds the notes domain: a thought with a priority that moves.
//
// A note is two things kept in step: a markdown file in the notes directory,
// which the owner edits in their own editor, and a row in SQLite holding what a
// list, a filter or a score needs. The file is the record — its front matter
// carries the title, the priority the owner chose, the status, the target, the
// tags and the accounts, cards or goals the note is about; the body is theirs.
// The row is an index of that file plus the counters (reads, edits) the file
// cannot keep for itself.
//
// The priority in the file is the owner's and is never rewritten. What
// pecunia shows beside it is worked out on every read — see Score — so a note
// filed LOW can surface as HIGH once its target is near and it keeps being
// opened, and a HIGH one can sink to LOW once it is left alone.
package notes

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"pecunia/internal/transactions"
)

// The base priorities, in climbing order. Level turns a score back into one of
// these words so the effective priority reads the same way the base one does.
const (
	PriorityLow      = "low"
	PriorityMedium   = "medium"
	PriorityHigh     = "high"
	PriorityCritical = "critical"
)

var Priorities = []string{PriorityLow, PriorityMedium, PriorityHigh, PriorityCritical}

// The statuses. open and doing are the live ones; done and dropped stay for the
// record but leave the default list and score nothing.
const (
	StatusOpen    = "open"
	StatusDoing   = "doing"
	StatusDone    = "done"
	StatusDropped = "dropped"
)

var Statuses = []string{StatusOpen, StatusDoing, StatusDone, StatusDropped}

var ErrNotFound = errors.New("note not found")

// slugLen caps the readable part of a file name; the id in front of it is what
// keeps the name unique.
const slugLen = 40

type Note struct {
	ID       int64
	Title    string
	Priority string // the base, as written in the file
	Status   string
	// Target is "" or a day, YYYY-MM-DD. TargetPhrase is what the owner typed
	// ("in 3 months"), kept as the comment beside the resolved day in the file.
	Target       string
	TargetPhrase string
	Tags         []string
	// What the note is about, as the codes and ids the file names. Resolved
	// against the other modules on save; read back from note_links joined to
	// their tables, so a deleted account simply stops appearing here.
	Accounts []string
	Cards    []string
	Goals    []int64
	// Path is the file's name inside the notes directory, never a full path.
	// Mtime and BodyHash are the file as last indexed — see Store.refresh.
	Path     string
	Mtime    int64
	BodyHash string
	// The counters the score reads. Reads are shows at the terminal; edits are
	// editor sessions that changed the file (and external edits sync picks up).
	ReadCount  int
	EditCount  int
	LastReadAt string
	CreatedAt  string
	UpdatedAt  string

	// Worked out on every read, never stored.
	Score   int
	Level   string // Score bucketed back to a priority word
	Problem string // why the file could not be re-indexed, or ""

	// The ids behind Accounts, Cards and Goals, filled by Store.ResolveLinks
	// before a write. Unexported: nothing outside the store should trust them.
	accountIDs []int64
	cardIDs    []int64
	goalIDs    []int64
}

// Validate is the store boundary guard. It holds on its own — a file the owner
// wrote by hand is the usual input — so a broken row never reaches the
// database.
func (n Note) Validate() error {
	if strings.TrimSpace(n.Title) == "" {
		return errors.New("title is required")
	}
	if !slices.Contains(Priorities, n.Priority) {
		return fmt.Errorf("%q is not a priority — one of %s", n.Priority, strings.Join(Priorities, ", "))
	}
	if !slices.Contains(Statuses, n.Status) {
		return fmt.Errorf("%q is not a status — one of %s", n.Status, strings.Join(Statuses, ", "))
	}
	if n.Target != "" {
		if _, err := transactions.ParseDate(n.Target); err != nil {
			return fmt.Errorf("target must be YYYY-MM-DD, not %q", n.Target)
		}
	}
	if len(n.Tags) > transactions.MaxTags {
		return fmt.Errorf("at most %d tags", transactions.MaxTags)
	}
	return nil
}

// Open is whether the note is still in play.
func (n Note) Open() bool { return n.Status == StatusOpen || n.Status == StatusDoing }

// DaysToTarget is how many calendar days until the target, negative once it
// has passed, and false when there is no target to count to. Calendar days,
// not hours: a target is a day the owner wrote down, and "due today" has to
// hold from midnight to midnight.
func (n Note) DaysToTarget(now time.Time) (int, bool) {
	if n.Target == "" {
		return 0, false
	}
	target, err := time.ParseInLocation(transactions.DateLayout, n.Target, now.Location())
	if err != nil {
		return 0, false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return int(target.Sub(today).Hours() / 24), true
}

// Slug is the readable part of a file name: lowercase, letters and digits kept
// (accents included — the owner's own language belongs in their file names),
// everything else folded into single hyphens, cut to slugLen runes.
func Slug(title string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			hyphen = false
			continue
		}
		if !hyphen && b.Len() > 0 {
			b.WriteByte('-')
			hyphen = true
		}
	}
	slug := []rune(strings.Trim(b.String(), "-"))
	if len(slug) > slugLen {
		slug = slug[:slugLen]
	}
	out := strings.Trim(string(slug), "-")
	if out == "" {
		return "note"
	}
	return out
}

// FileName is what a note's file is called: the id keeps it unique, the slug
// keeps it readable. It is set once, on create, and never follows the title.
func FileName(id int64, title string) string {
	return fmt.Sprintf("%d-%s.md", id, Slug(title))
}
