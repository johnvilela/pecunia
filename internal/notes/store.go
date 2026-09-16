package notes

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"pecunia/internal/accounts"
	"pecunia/internal/cards"
	"pecunia/internal/goals"
	"pecunia/internal/logs"
	"pecunia/internal/transactions"
)

// Store keeps the rows and the files in step. One object writes both because
// every caller — the CLI, the MCP tool, the seeder, sync — has to do the same
// two things in the same order, and a second copy of that order is a second
// place for them to drift apart.
type Store struct {
	db  *sql.DB
	dir string
}

// NewStore takes the directory as a parameter rather than reading Dir itself,
// so a test can point one at a temp dir the way it points PECUNIA_DB at one.
func NewStore(db *sql.DB, dir string) *Store { return &Store{db: db, dir: dir} }

// Filter is what a list narrows on. Priority and MinScore read the effective
// score, not the base: the list is sorted by it, and "the high ones" means
// the ones that are high now. DueBy is a day; the caller resolves phrases.
type Filter struct {
	All      bool   // done and dropped too
	Status   string // exactly this status, whatever All says
	Priority string // effective level
	MinScore int
	Tag      string
	Search   string // in the title or the body, case-insensitive
	DueBy    string // target on or before this day
	Overdue  bool
	Account  int64
	Card     int64
	Goal     int64
	Now      time.Time // zero means now
}

// columns carries the tags and the links back with the row. Sub-selects
// rather than joins, so a list is one query however many notes it has and a
// Get for a missing id is no rows rather than one all-NULL row. The links
// join their own tables, which is what makes a deleted account drop out of a
// note's list rather than linger as a number.
const columns = `n.id, n.title, n.priority, n.status, n.target, n.target_phrase, n.path,
	n.mtime, n.body_hash, n.read_count, n.edit_count, n.last_read_at, n.created_at, n.updated_at,
	COALESCE((SELECT group_concat(tag) FROM note_tags WHERE note_id = n.id), ''),
	COALESCE((SELECT group_concat(a.code) FROM note_links l JOIN accounts a ON a.id = l.ref_id
	          WHERE l.note_id = n.id AND l.kind = 'account'), ''),
	COALESCE((SELECT group_concat(c.code) FROM note_links l JOIN credit_cards c ON c.id = l.ref_id
	          WHERE l.note_id = n.id AND l.kind = 'card'), ''),
	COALESCE((SELECT group_concat(g.id) FROM note_links l JOIN goals g ON g.id = l.ref_id
	          WHERE l.note_id = n.id AND l.kind = 'goal'), '')`

func scan(row interface{ Scan(...any) error }) (Note, error) {
	var n Note
	var tags, accs, crds, gls string
	err := row.Scan(&n.ID, &n.Title, &n.Priority, &n.Status, &n.Target, &n.TargetPhrase, &n.Path,
		&n.Mtime, &n.BodyHash, &n.ReadCount, &n.EditCount, &n.LastReadAt, &n.CreatedAt, &n.UpdatedAt,
		&tags, &accs, &crds, &gls)
	if err != nil {
		return n, err
	}
	n.Tags = split(tags)
	n.Accounts = split(accs)
	n.Cards = split(crds)
	for _, g := range split(gls) {
		id, _ := strconv.ParseInt(g, 10, 64)
		n.Goals = append(n.Goals, id)
	}
	slices.Sort(n.Goals)
	return n, nil
}

// split undoes group_concat: nil for nothing, sorted otherwise, since the
// concat order is whatever SQLite felt like.
func split(s string) []string {
	if s == "" {
		return nil
	}
	out := strings.Split(s, ",")
	slices.Sort(out)
	return out
}

// List is every note the filter keeps, highest score first. The SQL narrows
// what it can; the search reads bodies from the files, and the score filters
// run after the scores exist.
func (s *Store) List(f Filter) ([]Note, error) {
	now := f.Now
	if now.IsZero() {
		now = time.Now()
	}
	var where []string
	var args []any
	add := func(cond string, a ...any) {
		where = append(where, cond)
		args = append(args, a...)
	}
	switch {
	case f.Status != "":
		add(`n.status = ?`, f.Status)
	case !f.All:
		add(`n.status IN ('open', 'doing')`)
	}
	if tag := strings.ToLower(strings.TrimSpace(f.Tag)); tag != "" {
		add(`EXISTS (SELECT 1 FROM note_tags WHERE note_id = n.id AND tag = ?)`, tag)
	}
	for _, l := range []struct {
		kind string
		id   int64
	}{{"account", f.Account}, {"card", f.Card}, {"goal", f.Goal}} {
		if l.id != 0 {
			add(`EXISTS (SELECT 1 FROM note_links WHERE note_id = n.id AND kind = ? AND ref_id = ?)`, l.kind, l.id)
		}
	}
	if f.DueBy != "" {
		add(`n.target <> '' AND n.target <= ?`, f.DueBy)
	}
	if f.Overdue {
		add(`n.target <> '' AND n.target < ?`, now.Format(transactions.DateLayout))
	}
	query := `SELECT ` + columns + ` FROM notes n`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	rows, err := s.db.Query(query+` ORDER BY n.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []Note
	for rows.Next() {
		n, err := scan(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	if q := strings.ToLower(strings.TrimSpace(f.Search)); q != "" {
		var kept []Note
		for _, n := range all {
			// A body that cannot be read is not a match, and not an error here:
			// the row still lists, and refresh is what reports the file.
			body, _ := s.Body(n)
			if strings.Contains(strings.ToLower(n.Title), q) || strings.Contains(strings.ToLower(body), q) {
				kept = append(kept, n)
			}
		}
		all = kept
	}
	if err := s.score(all, now); err != nil {
		return nil, err
	}
	if f.Priority != "" || f.MinScore > 0 {
		var kept []Note
		for _, n := range all {
			if (f.Priority == "" || n.Level == f.Priority) && n.Score >= f.MinScore {
				kept = append(kept, n)
			}
		}
		all = kept
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Target != b.Target {
			// A note with a day sorts above one without.
			return b.Target == "" || (a.Target != "" && a.Target < b.Target)
		}
		return a.ID < b.ID
	})
	return all, nil
}

func (s *Store) Get(id int64) (Note, error) {
	n, err := scan(s.db.QueryRow(`SELECT `+columns+` FROM notes n WHERE n.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return n, ErrNotFound
	}
	if err != nil {
		return n, err
	}
	ns := []Note{n}
	if err := s.score(ns, time.Now()); err != nil {
		return n, err
	}
	return ns[0], nil
}

// score fills Score and Level on every note, one activity query for the lot.
func (s *Store) score(ns []Note, now time.Time) error {
	ids := make([]int64, len(ns))
	for i, n := range ns {
		ids[i] = n.ID
	}
	act, err := s.Activity(ids, now)
	if err != nil {
		return err
	}
	for i := range ns {
		ns[i].Score = Score(ns[i], act[ns[i].ID], now)
		ns[i].Level = Level(ns[i].Score)
	}
	return nil
}

// Activity is how busy each note's linked accounts, cards and goals have been:
// distinct transactions on any of them in the last 30 and 90 days, one query
// for every id asked about. A transaction on a linked account that also names
// a linked goal counts once. Notes with no links, or no activity, have no
// entry. This reads the transactions table directly, the way goals does for
// its progress.
func (s *Store) Activity(ids []int64, now time.Time) (map[int64]Activity, error) {
	out := map[int64]Activity{}
	if len(ids) == 0 {
		return out, nil
	}
	since := func(days int) string { return now.AddDate(0, 0, -days).Format(transactions.DateLayout) }
	args := []any{since(30), since(90)}
	marks := make([]string, len(ids))
	for i, id := range ids {
		marks[i] = "?"
		args = append(args, id)
	}
	rows, err := s.db.Query(`
		SELECT l.note_id,
		       COUNT(DISTINCT CASE WHEN t.date >= ? THEN t.id END),
		       COUNT(DISTINCT t.id)
		FROM note_links l
		JOIN transactions t ON t.date >= ? AND (
		     (l.kind = 'account' AND t.account_id = l.ref_id)
		  OR (l.kind = 'card'    AND t.card_id    = l.ref_id)
		  OR (l.kind = 'goal'    AND t.goal_id    = l.ref_id))
		WHERE l.note_id IN (`+strings.Join(marks, ", ")+`)
		GROUP BY l.note_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var a Activity
		if err := rows.Scan(&id, &a.Tx30, &a.Tx90); err != nil {
			return nil, err
		}
		out[id] = a
	}
	return out, rows.Err()
}

// ResolveLinks turns the codes and ids a note names into rows, refusing any
// that is not there — a note about an account that does not exist is a typo,
// not a link. It runs before any transaction is open: the database has one
// connection, and a lookup through another store while a tx holds it would
// wait forever.
func (s *Store) ResolveLinks(n *Note) error {
	n.accountIDs, n.cardIDs, n.goalIDs = nil, nil, nil
	var accs, crds []string
	var gls []int64

	as := accounts.NewStore(s.db)
	for _, code := range n.Accounts {
		a, err := as.ByCode(code)
		if errors.Is(err, accounts.ErrNotFound) {
			return fmt.Errorf("no account matching %q", code)
		}
		if err != nil {
			return err
		}
		if !slices.Contains(n.accountIDs, a.ID) {
			accs, n.accountIDs = append(accs, a.Code), append(n.accountIDs, a.ID)
		}
	}
	cs := cards.NewStore(s.db)
	for _, code := range n.Cards {
		c, err := cs.ByCode(code)
		if errors.Is(err, cards.ErrNotFound) {
			return fmt.Errorf("no credit card matching %q", code)
		}
		if err != nil {
			return err
		}
		if !slices.Contains(n.cardIDs, c.ID) {
			crds, n.cardIDs = append(crds, c.Code), append(n.cardIDs, c.ID)
		}
	}
	gs := goals.NewStore(s.db)
	for _, id := range n.Goals {
		g, err := gs.Get(id)
		if errors.Is(err, goals.ErrNotFound) {
			return fmt.Errorf("no goal matching %q", strconv.FormatInt(id, 10))
		}
		if err != nil {
			return err
		}
		if !slices.Contains(n.goalIDs, g.ID) {
			gls, n.goalIDs = append(gls, g.ID), append(n.goalIDs, g.ID)
		}
	}
	slices.Sort(accs)
	slices.Sort(crds)
	slices.Sort(gls)
	n.Accounts, n.Cards, n.Goals = accs, crds, gls
	return nil
}

// Create writes the row and the file in one go. The row goes first — the id
// is what names the file — and the file inside the same transaction, so a
// file that cannot be written leaves no row and a row that cannot be
// committed leaves no file.
func (s *Store) Create(n *Note, body string) error {
	n.Tags = transactions.NormalizeTags(n.Tags)
	if err := n.Validate(); err != nil {
		return err
	}
	if err := s.ResolveLinks(n); err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// path is UNIQUE and the real name needs the id, so the insert carries a
	// placeholder no other row can be holding.
	res, err := tx.Exec(
		`INSERT INTO notes (title, priority, status, target, target_phrase, path) VALUES (?, ?, ?, ?, ?, ?)`,
		n.Title, n.Priority, n.Status, n.Target, n.TargetPhrase,
		"pending-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err != nil {
		return err
	}
	if n.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	n.Path = FileName(n.ID, n.Title)
	mtime, hash, err := s.write(*n, body)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			os.Remove(s.Path(*n))
		}
	}()
	if _, err := tx.Exec(`UPDATE notes SET path = ?, mtime = ?, body_hash = ? WHERE id = ?`,
		n.Path, mtime, hash, n.ID); err != nil {
		return err
	}
	if err := writeTags(tx, n.ID, n.Tags); err != nil {
		return err
	}
	if err := writeLinks(tx, *n); err != nil {
		return err
	}
	if err := logs.Record(tx, logs.Actor, "created", "note", n.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	got, err := s.Get(n.ID)
	if err != nil {
		return err
	}
	*n = got
	return nil
}

// Update rewrites the row and the file. The file keeps its name whatever
// happened to the title: a rename is a broken link for anything else that
// learned the path. Every update counts as an edit, since the callers only
// come here with something changed.
func (s *Store) Update(n Note, body string) error {
	n.Tags = transactions.NormalizeTags(n.Tags)
	if err := n.Validate(); err != nil {
		return err
	}
	old, err := s.Get(n.ID)
	if err != nil {
		return err
	}
	if err := s.ResolveLinks(&n); err != nil {
		return err
	}
	n.Path = old.Path

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`UPDATE notes SET title = ?, priority = ?, status = ?, target = ?, target_phrase = ?,
			edit_count = edit_count + 1, updated_at = datetime('now') WHERE id = ?`,
		n.Title, n.Priority, n.Status, n.Target, n.TargetPhrase, n.ID); err != nil {
		return err
	}
	for _, table := range []string{"note_tags", "note_links"} {
		if _, err := tx.Exec(`DELETE FROM `+table+` WHERE note_id = ?`, n.ID); err != nil {
			return err
		}
	}
	if err := writeTags(tx, n.ID, n.Tags); err != nil {
		return err
	}
	if err := writeLinks(tx, n); err != nil {
		return err
	}
	mtime, hash, err := s.write(n, body)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE notes SET mtime = ?, body_hash = ? WHERE id = ?`, mtime, hash, n.ID); err != nil {
		return err
	}
	if err := logs.RecordEdit(tx, logs.Actor, "note", n.ID, logs.Diff(
		logs.F("title", old.Title, n.Title),
		logs.F("priority", old.Priority, n.Priority),
		logs.F("status", old.Status, n.Status),
		logs.F("target", old.Target, n.Target),
		logs.F("tags", old.Tags, n.Tags),
		logs.F("accounts", old.Accounts, n.Accounts),
		logs.F("cards", old.Cards, n.Cards),
		logs.F("goals", old.Goals, n.Goals),
		logs.F("body", short(old.BodyHash), short(hash)),
	)); err != nil {
		return err
	}
	return tx.Commit()
}

// short is enough of a hash to say "the body moved" in the trail without
// pasting the whole digest.
func short(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}

// Delete takes the row — tags and links go with it — then the file. The row
// goes first so a file that will not go leaves nothing pointing at it; the
// error names the file, and sync would adopt it as a new note if it stayed.
func (s *Store) Delete(id int64) error {
	n, err := s.Get(id)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM notes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return ErrNotFound
	}
	if err := logs.Record(tx, logs.Actor, "deleted", "note", id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := os.Remove(s.Path(n)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("note %d deleted, but its file could not be removed: %w", id, err)
	}
	return nil
}

// Touch counts one read. Reads do not log: the trail is for what changed.
func (s *Store) Touch(id int64) error {
	res, err := s.db.Exec(
		`UPDATE notes SET read_count = read_count + 1, last_read_at = datetime('now') WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return ErrNotFound
	}
	return nil
}

// AllTags is every tag in use, for a picker or a completion.
func (s *Store) AllTags() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT tag FROM note_tags ORDER BY tag`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}

func writeTags(tx *sql.Tx, id int64, tags []string) error {
	for _, tag := range tags {
		if _, err := tx.Exec(`INSERT INTO note_tags (note_id, tag) VALUES (?, ?)`, id, tag); err != nil {
			return err
		}
	}
	return nil
}

// writeLinks writes what ResolveLinks found. Nothing here reads the codes:
// only the ids the lookup filled are trusted.
func writeLinks(tx *sql.Tx, n Note) error {
	for _, l := range []struct {
		kind string
		ids  []int64
	}{{"account", n.accountIDs}, {"card", n.cardIDs}, {"goal", n.goalIDs}} {
		for _, id := range l.ids {
			if _, err := tx.Exec(
				`INSERT INTO note_links (note_id, kind, ref_id) VALUES (?, ?, ?)`, n.ID, l.kind, id); err != nil {
				return err
			}
		}
	}
	return nil
}
