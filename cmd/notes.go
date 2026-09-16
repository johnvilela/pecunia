package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"pecunia/internal/accounts"
	"pecunia/internal/cards"
	"pecunia/internal/core"
	"pecunia/internal/goals"
	"pecunia/internal/notes"
	"pecunia/internal/transactions"
)

const notesHelp = `Notes with a priority that moves.

Usage:
  pecunia notes [command] [ID] [flags]
  pecunia n     [command] [ID] [flags]

Commands:
  (none)              list the open notes, highest effective priority first
  new     | n [TITLE] create a note in your editor
  edit    | e [ID]    edit a note in your editor
  delete  | d [ID]    delete a note and its file
  sync                re-index files edited outside pecunia, adopt new .md files
  ID                  show one note; counts as a read
  --path  [ID]        print the note's file path and nothing else

List flags:
  --all               done and dropped too
  --status S          open, doing, done or dropped
  --priority L        effective level: low, medium, high or critical
  --min-score N       effective score at least N
  --tag T             carrying this tag
  --search Q          Q in the title or the body
  --due WHEN          target on or before WHEN — a day or a phrase, as in the file
  --overdue           target already passed
  --account REF       about this account (CODE or ID); likewise --card, --goal

A note is a markdown file in the notes directory — ~/.config/pecunia/notes, or
$PECUNIA_NOTES — with its title, priority, status, target, tags and links in a
front matter block, and the body yours. SQLite keeps only what a list needs.
Edit the file with anything; the next list picks the change up.

The priority you write never changes by itself. Beside it pecunia shows an
effective one, worked out on every read: a near or missed target, a note you
keep opening, and activity on the accounts, cards or goals it names all push
it up; a month or more left alone pulls it down. So a LOW note can surface
as HIGH when its time comes, and a HIGH one sinks once it is forgotten.
Notes are referenced by id. Leaving [ID] out opens a picker. Add -h to any
command for its own help.
`

var noteSubHelp = map[string]string{
	"new": `Create a note.

Usage:
  pecunia notes new [TITLE] [flags]
  pecunia n n       [TITLE] [flags]

Flags:
  -p, --priority L    low, medium (default), high or critical
  -t, --target WHEN   today, tomorrow, next week|month|year, in 3 months,
                      2 weeks, 2026-12, 16/12/2026 or 2026-12-16
      --status S      open (default), doing, done or dropped
      --tags a,b      up to five, lowercased
      --account CODE  accounts the note is about; likewise --card CODE and
                      --goal ID; comma-separated for more than one
      --no-edit       save from the flags alone, without opening the editor

Opens $VISUAL, or $EDITOR, or vi, on a draft with the front matter filled
from the flags and the cursor on the body. For vim, nvim, nano, micro, emacs,
helix, VS Code, Sublime and Zed the cursor lands on the body; any other
editor just opens the file. Quitting without writing creates nothing. A front
matter pecunia cannot read offers to reopen the editor; declining leaves the
draft in the notes directory for pecunia n sync to adopt once it is fixed.

A phrase in target is resolved to a day when the note is saved, and kept
beside the day as a comment. Codes and ids in accounts, cards and goals are
checked: a note about an account that does not exist is a typo.
`,
	"edit": `Edit a note.

Usage:
  pecunia notes edit [ID]
  pecunia n e        [ID]

Opens the note's file in your editor, cursor on the body. Without ID, pick
from a list first. Closing without a change counts as a read; a change is
saved back in canonical form, counted as an edit, and logged with the fields
that moved. The file keeps its name whatever happens to the title.
`,
	"delete": `Delete a note for good.

Usage:
  pecunia notes delete [ID]
  pecunia n d        [ID]

Asks for confirmation. Without ID, pick from a list first. The file goes with
the row.
`,
	"sync": `Re-index the notes directory.

Usage:
  pecunia notes sync
  pecunia n sync

Reads every note's file again, whatever its mtime says, and brings the row up
to date — a target phrase typed by hand becomes a day, and the file is written
back in canonical form. Every .md file no note owns is adopted as a new note
and renamed. A note whose file is gone is reported, never dropped: that is
pecunia n d ID.

A plain pecunia n already picks up a file whose mtime moved; sync is for
when it did not, or when files were added.
`,
}

var errNoNotes = errors.New("no notes yet — create one with: pecunia n n")

// openEditor is what new and edit hand the file to. A variable so the tests
// can swap in one that writes without a terminal.
var openEditor = notes.OpenEditor

func runNotes(args []string) error {
	if len(args) == 0 {
		return listNotes(nil)
	}
	sub, rest := args[0], args[1:]
	if isHelpFlag(sub) {
		fmt.Fprint(out, notesHelp)
		return nil
	}
	if sub == "sync" {
		if len(rest) > 0 && isHelpFlag(rest[0]) {
			fmt.Fprint(out, noteSubHelp["sync"])
			return nil
		}
		return withNotes(func(_ *sql.DB, s *notes.Store) error { return syncNotes(s) })
	}
	if sub == "--path" {
		return withNotes(func(_ *sql.DB, s *notes.Store) error {
			n, err := resolveOrPickNote(s, rest, "Which note?")
			if err != nil {
				return err
			}
			fmt.Fprintln(out, s.Path(n))
			return nil
		})
	}
	if strings.HasPrefix(sub, "-") {
		return listNotes(args)
	}

	name := map[string]string{
		"new": "new", "n": "new",
		"edit": "edit", "e": "edit",
		"delete": "delete", "d": "delete",
	}[sub]

	if name == "" {
		// Anything else is an id.
		return withNotes(func(conn *sql.DB, s *notes.Store) error {
			n, err := resolveOrPickNote(s, args, "Note details")
			if err != nil {
				return err
			}
			return showNote(conn, s, n)
		})
	}
	if len(rest) > 0 && isHelpFlag(rest[0]) {
		fmt.Fprint(out, noteSubHelp[name])
		return nil
	}

	return withNotes(func(conn *sql.DB, s *notes.Store) error {
		switch name {
		case "new":
			return createNote(s, rest)
		case "edit":
			return editNote(s, rest)
		default:
			return deleteNote(s, rest)
		}
	})
}

func withNotes(fn func(*sql.DB, *notes.Store) error) error {
	return withConn(func(conn *sql.DB) error {
		dir, err := notes.Dir()
		if err != nil {
			return err
		}
		return fn(conn, notes.NewStore(conn, dir))
	})
}

// resolveNote turns a reference into a note. A note has no code, so anything
// that is not a number is a mistake worth naming rather than a lookup to try.
func resolveNote(s *notes.Store, ref string) (notes.Note, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(ref), 10, 64)
	if err != nil {
		return notes.Note{}, fmt.Errorf("no note matching %q — notes are referenced by id", ref)
	}
	n, err := s.Get(id)
	if errors.Is(err, notes.ErrNotFound) {
		return n, fmt.Errorf("no note matching %q", ref)
	}
	return n, err
}

// resolveOrPickNote turns an optional ID argument into a note, falling back to
// the picker over the open ones when none was given.
func resolveOrPickNote(s *notes.Store, args []string, title string) (notes.Note, error) {
	if len(args) > 0 && args[0] != "" {
		return resolveNote(s, args[0])
	}
	all, err := s.List(notes.Filter{})
	if err != nil {
		return notes.Note{}, err
	}
	if len(all) == 0 {
		return notes.Note{}, errNoNotes
	}
	return notes.Pick(all, title)
}

// parseListFilter turns the list flags into a filter, resolving every
// reference through its own module so --account inter works the way
// pecunia ac inter does.
func parseListFilter(conn *sql.DB, args []string) (notes.Filter, bool, error) {
	fs := flag.NewFlagSet("notes", flag.ContinueOnError)
	// The flag package's own usage dump would print the flags a second time,
	// in its own single-dash spelling, next to the error report() prints.
	fs.SetOutput(io.Discard)
	var (
		all      = fs.Bool("all", false, "done and dropped too")
		status   = fs.String("status", "", "open, doing, done or dropped")
		priority = fs.String("priority", "", "effective level")
		minScore = fs.Int("min-score", 0, "effective score at least this")
		tag      = fs.String("tag", "", "carrying this tag")
		search   = fs.String("search", "", "text in the title or the body")
		due      = fs.String("due", "", "target on or before this")
		overdue  = fs.Bool("overdue", false, "target already passed")
		account  = fs.String("account", "", "account CODE or ID")
		card     = fs.String("card", "", "credit card CODE or ID")
		goal     = fs.String("goal", "", "goal ID")
	)
	if err := fs.Parse(args); err != nil {
		return notes.Filter{}, false, fmt.Errorf("%w — see: pecunia n -h", err)
	}
	if n := fs.NArg(); n > 0 {
		return notes.Filter{}, false, fmt.Errorf("unexpected argument %q — filters are flags, and an id takes none", fs.Arg(0))
	}
	f := notes.Filter{All: *all, MinScore: *minScore, Tag: *tag, Search: *search, Overdue: *overdue}
	var err error
	if f.Status, err = oneOf("--status", *status, notes.Statuses, "status"); err != nil {
		return f, false, err
	}
	if f.Priority, err = oneOf("--priority", *priority, notes.Priorities, "priority"); err != nil {
		return f, false, err
	}
	if *due != "" {
		if f.DueBy, err = notes.ParseWhen(*due, time.Now()); err != nil {
			return f, false, fmt.Errorf("--due: %w", err)
		}
	}
	if *account != "" {
		a, err := accounts.NewStore(conn).Resolve(*account)
		if err != nil {
			return f, false, fmt.Errorf("no account matching %q", *account)
		}
		f.Account = a.ID
	}
	if *card != "" {
		c, err := cards.NewStore(conn).Resolve(*card)
		if err != nil {
			return f, false, fmt.Errorf("no credit card matching %q", *card)
		}
		f.Card = c.ID
	}
	if *goal != "" {
		g, err := resolveGoal(goals.NewStore(conn), *goal)
		if err != nil {
			return f, false, err
		}
		f.Goal = g.ID
	}
	narrowed := len(args) > 0
	return f, narrowed, nil
}

// oneOf checks a flag against a closed set; empty passes as empty.
func oneOf(flagName, value string, set []string, what string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || slices.Contains(set, value) {
		return value, nil
	}
	return "", fmt.Errorf("%s: %q is not a %s — one of %s", flagName, value, what, strings.Join(set, ", "))
}

func listNotes(args []string) error {
	return withNotes(func(conn *sql.DB, s *notes.Store) error {
		f, narrowed, err := parseListFilter(conn, args)
		if err != nil {
			return err
		}
		all, err := s.List(f)
		if err != nil {
			return err
		}
		switch {
		case len(all) == 0 && narrowed:
			fmt.Fprintln(out, "no notes match — widen with: pecunia n --all")
		case len(all) == 0:
			fmt.Fprintln(out, errNoNotes)
		default:
			fmt.Fprintln(out, notes.Table(all, time.Now()))
		}
		return nil
	})
}

// showNote is `pecunia n ID`: the card, the body, and one more read on the
// counter.
func showNote(conn *sql.DB, s *notes.Store, n notes.Note) error {
	now := time.Now()
	body, _ := s.Body(n) // a file that will not read shows as its Problem
	act, err := s.Activity([]int64{n.ID}, now)
	if err != nil {
		return err
	}
	fmt.Fprint(out, notes.Details(n, notes.Explain(n, act[n.ID], now), body, noteLinks(conn, n), s.Path(n), now))
	return s.Touch(n.ID)
}

// noteLinks spells the note's links for a person: names with their codes.
func noteLinks(conn *sql.DB, n notes.Note) notes.Links {
	var l notes.Links
	as, cs, gs := accounts.NewStore(conn), cards.NewStore(conn), goals.NewStore(conn)
	for _, code := range n.Accounts {
		if a, err := as.ByCode(code); err == nil {
			l.Accounts = append(l.Accounts, a.Name+" ("+a.Code+")")
		} else {
			l.Accounts = append(l.Accounts, code)
		}
	}
	for _, code := range n.Cards {
		if c, err := cs.ByCode(code); err == nil {
			l.Cards = append(l.Cards, c.Name+" ("+c.Code+")")
		} else {
			l.Cards = append(l.Cards, code)
		}
	}
	for _, id := range n.Goals {
		if g, err := gs.Get(id); err == nil {
			l.Goals = append(l.Goals, fmt.Sprintf("%s (#%d)", g.Name, g.ID))
		} else {
			l.Goals = append(l.Goals, fmt.Sprintf("#%d", id))
		}
	}
	return l
}

// parseInterleaved parses flags that may come before, after or between the
// positional words, so `pecunia n n Health care -p low` and
// `pecunia n n -p low Health care` read the same.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// newFlags is what `pecunia n new` takes before it opens anything.
type newFlags struct {
	note   notes.Note
	noEdit bool
}

func parseNewFlags(args []string) (newFlags, error) {
	fs := flag.NewFlagSet("notes new", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var priority, target, status, tags, account, card, goal string
	for _, name := range []string{"priority", "p"} {
		fs.StringVar(&priority, name, notes.PriorityMedium, "")
	}
	for _, name := range []string{"target", "t"} {
		fs.StringVar(&target, name, "", "")
	}
	fs.StringVar(&status, "status", notes.StatusOpen, "")
	fs.StringVar(&tags, "tags", "", "")
	fs.StringVar(&account, "account", "", "")
	fs.StringVar(&card, "card", "", "")
	fs.StringVar(&goal, "goal", "", "")
	noEdit := fs.Bool("no-edit", false, "")
	words, err := parseInterleaved(fs, args)
	if err != nil {
		return newFlags{}, fmt.Errorf("%w — see: pecunia n n -h", err)
	}

	n := notes.Note{Title: strings.TrimSpace(strings.Join(words, " ")), Tags: transactions.ParseTags(tags)}
	if n.Priority, err = oneOf("--priority", priority, notes.Priorities, "priority"); err != nil {
		return newFlags{}, err
	}
	if n.Status, err = oneOf("--status", status, notes.Statuses, "status"); err != nil {
		return newFlags{}, err
	}
	if n.Target, err = notes.ParseWhen(target, time.Now()); err != nil {
		return newFlags{}, fmt.Errorf("--target: %w", err)
	}
	if strings.TrimSpace(target) != n.Target {
		n.TargetPhrase = strings.TrimSpace(target)
	}
	n.Accounts = commaList(account)
	n.Cards = commaList(card)
	for _, g := range commaList(goal) {
		id, err := strconv.ParseInt(g, 10, 64)
		if err != nil || id < 1 {
			return newFlags{}, fmt.Errorf("--goal: %q is not a goal id", g)
		}
		n.Goals = append(n.Goals, id)
	}
	return newFlags{note: n, noEdit: *noEdit}, nil
}

func commaList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// createNote is `pecunia n new`. Everything the flags can get wrong is
// refused before the editor opens: a typo should cost a retry, not a draft.
func createNote(s *notes.Store, args []string) error {
	nf, err := parseNewFlags(args)
	if err != nil {
		return err
	}
	n := nf.note
	if err := s.ResolveLinks(&n); err != nil {
		return err
	}
	if nf.noEdit {
		if err := s.Create(&n, ""); err != nil {
			return err
		}
		fmt.Fprintf(out, "created note %d: %s (%s)\n", n.ID, n.Title, notes.Priority(n))
		return nil
	}

	if err := os.MkdirAll(s.Dir(), 0o700); err != nil {
		return err
	}
	// The draft is an ordinary .md in the notes directory on purpose: if
	// anything goes wrong past this point, sync adopts it once it parses.
	draft := filepath.Join(s.Dir(), fmt.Sprintf("draft-%d.md", time.Now().UnixNano()))
	data, line := notes.RenderDraft(n)
	if err := os.WriteFile(draft, data, 0o600); err != nil {
		return err
	}
	first := true
	for {
		changed, err := notes.EditFile(draft, line, openEditor)
		if err != nil {
			os.Remove(draft)
			return fmt.Errorf("could not run %s: %w", notes.Editor(), err)
		}
		if !changed && first {
			os.Remove(draft)
			fmt.Fprintln(out, "nothing written — no note created")
			return nil
		}
		first, line = false, 1

		m, body, err := readBack(s, draft)
		if err == nil {
			err = s.Create(&m, body)
		}
		if err != nil {
			ok, cerr := core.Confirm("Could not save the note: "+err.Error(), "Reopen the editor to fix it?", "Reopen")
			if cerr != nil {
				return cerr
			}
			if !ok {
				return fmt.Errorf("%w — left your draft at %s; fix it and run: pecunia n sync", err, draft)
			}
			continue
		}
		os.Remove(draft)
		fmt.Fprintf(out, "created note %d: %s (%s) → %s\n", m.ID, m.Title, notes.Priority(m), s.Path(m))
		return nil
	}
}

// readBack parses what the editor left, ready for the store.
func readBack(s *notes.Store, path string) (notes.Note, string, error) {
	doc, err := notes.ParseFile(path)
	if err != nil {
		return notes.Note{}, "", err
	}
	m, err := doc.Note(time.Now())
	if err != nil {
		return notes.Note{}, "", err
	}
	if err := s.ResolveLinks(&m); err != nil {
		return notes.Note{}, "", err
	}
	return m, doc.Body, nil
}

func editNote(s *notes.Store, args []string) error {
	n, err := resolveOrPickNote(s, args, "Edit which note?")
	if err != nil {
		return err
	}
	path := s.Path(n)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("note %d's file is missing (%s) — pecunia n sync lists it, pecunia n d %d drops it", n.ID, path, n.ID)
	}
	line := 1
	if doc, err := notes.ParseFile(path); err == nil {
		line = doc.BodyLine
	}
	first := true
	for {
		changed, err := notes.EditFile(path, line, openEditor)
		if err != nil {
			return fmt.Errorf("could not run %s: %w", notes.Editor(), err)
		}
		if !changed && first {
			// Opening it and closing it is still reading it.
			if err := s.Touch(n.ID); err != nil {
				return err
			}
			fmt.Fprintf(out, "no changes to note %d\n", n.ID)
			return nil
		}
		first, line = false, 1

		m, body, err := readBack(s, path)
		if err == nil {
			m.ID = n.ID
			err = s.Update(m, body)
		}
		if err != nil {
			ok, cerr := core.Confirm("Could not save the note: "+err.Error(), "Reopen the editor to fix it?", "Reopen")
			if cerr != nil {
				return cerr
			}
			if !ok {
				return fmt.Errorf("%w — the file is as you left it; pecunia n shows the problem until it is fixed", err)
			}
			continue
		}
		got, err := s.Get(n.ID)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "updated note %d: %s (%s)\n", got.ID, got.Title, notes.Priority(got))
		return nil
	}
}

func deleteNote(s *notes.Store, args []string) error {
	n, err := resolveOrPickNote(s, args, "Delete which note?")
	if err != nil {
		return err
	}
	ok, err := core.Confirm(fmt.Sprintf("Delete note %d: %s?", n.ID, n.Title),
		"The file "+s.Path(n)+" goes with it. This cannot be undone.", "Yes, delete")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "cancelled")
		return nil
	}
	if err := s.Delete(n.ID); err != nil {
		return err
	}
	fmt.Fprintf(out, "deleted note %d: %s\n", n.ID, n.Title)
	return nil
}

func syncNotes(s *notes.Store) error {
	r, err := s.Sync()
	if err != nil {
		return err
	}
	if r.Empty() {
		fmt.Fprintln(out, "nothing to do")
		return nil
	}
	for _, p := range r.Reindexed {
		fmt.Fprintln(out, "re-indexed:", p)
	}
	for _, a := range r.Adopted {
		fmt.Fprintln(out, "adopted:", a)
	}
	for _, p := range r.Missing {
		line := "missing: " + p
		if id, _, ok := strings.Cut(p, "-"); ok {
			if _, err := strconv.Atoi(id); err == nil {
				line += " — pecunia n d " + id + " drops it"
			}
		}
		fmt.Fprintln(out, line)
	}
	files := make([]string, 0, len(r.Problems))
	for f := range r.Problems {
		files = append(files, f)
	}
	slices.Sort(files)
	for _, f := range files {
		msg := r.Problems[f]
		// The store already names the file in most of these.
		if !strings.HasPrefix(msg, f+": ") {
			msg = f + ": " + msg
		}
		fmt.Fprintln(out, "problem:", msg)
	}
	return nil
}
