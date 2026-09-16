package notes

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pecunia/internal/db"
	"pecunia/internal/logs"
)

// Dir is where the note files live. A dev build keeps them beside its own
// database and ignores PECUNIA_NOTES, the same way db.Path ignores PECUNIA_DB
// there, so a dev binary can never reach the real notes. Otherwise
// PECUNIA_NOTES when set, else notes/ beside the database.
func Dir() (string, error) {
	if db.DevDB != "" {
		return strings.TrimSuffix(db.DevDB, ".db") + ".notes", nil
	}
	if p := os.Getenv("PECUNIA_NOTES"); p != "" {
		return p, nil
	}
	path, err := db.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "notes"), nil
}

// Path is where a note's file is.
func (s *Store) Path(n Note) string { return filepath.Join(s.dir, n.Path) }

// Dir is the directory the files live in.
func (s *Store) Dir() string { return s.dir }

// Body reads a note's body back from its file.
func (s *Store) Body(n Note) (string, error) {
	doc, err := ParseFile(s.Path(n))
	if err != nil {
		return "", err
	}
	return doc.Body, nil
}

// write puts the canonical file down and reports what the index keeps of it.
// 0600, like the database: a note can say anything.
func (s *Store) write(n Note, body string) (mtime int64, hash string, err error) {
	path := s.Path(n)
	if err := os.WriteFile(path, Render(n, body), 0o600); err != nil {
		return 0, "", err
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0, "", err
	}
	return st.ModTime().UnixNano(), hashBody(body), nil
}

// hashBody is what says whether a body moved: the trimmed text, so a saved
// file that only gained a trailing newline is not an edit.
func hashBody(body string) string {
	sum := sha256.Sum256([]byte(strings.TrimRight(body, " \t\n")))
	return hex.EncodeToString(sum[:])
}

// missing is what a row says when its file is gone.
const missing = "file missing — pecunia n sync"

// refresh brings each row up to date with its file. A file whose mtime is what
// the row remembers is left alone; one that moved is read again and its row
// rewritten — never the file, which is the owner's. A file that is gone or
// will not parse marks the row with the reason and leaves it as it was, so a
// typo in the front matter costs a warning on the list, not the note.
func (s *Store) refresh(ns []Note, now time.Time) {
	for i := range ns {
		n := &ns[i]
		st, err := os.Stat(s.Path(*n))
		switch {
		case os.IsNotExist(err):
			n.Problem = missing
			continue
		case err != nil:
			n.Problem = err.Error()
			continue
		case st.ModTime().UnixNano() == n.Mtime:
			continue
		}
		if err := s.reindex(n, now, st.ModTime().UnixNano()); err != nil {
			n.Problem = err.Error()
		}
	}
}

// reindex reads the file behind n into its row and reloads n from it.
func (s *Store) reindex(n *Note, now time.Time, mtime int64) error {
	fresh, doc, err := s.read(*n, now)
	if err != nil {
		return err
	}
	if _, err := s.index(*n, fresh, doc.Body, mtime); err != nil {
		return err
	}
	got, err := s.row(n.ID)
	if err != nil {
		return err
	}
	*n = got
	return nil
}

// read parses the file behind n into a note ready to store, links resolved,
// every error naming the file.
func (s *Store) read(n Note, now time.Time) (Note, Doc, error) {
	doc, err := ParseFile(s.Path(n))
	if err != nil {
		return Note{}, doc, err
	}
	fresh, err := doc.Note(now)
	if err != nil {
		return Note{}, doc, fmt.Errorf("%s: %w", n.Path, err)
	}
	fresh.ID, fresh.Path = n.ID, n.Path
	if err := s.ResolveLinks(&fresh); err != nil {
		return Note{}, doc, fmt.Errorf("%s: %w", n.Path, err)
	}
	return fresh, doc, nil
}

// index writes what a file says into its row and remembers the file as read.
// Only a real change — a field, a tag, a link, the body — counts as an edit
// and reaches the trail; a save that moved nothing but the mtime just updates
// the mtime, so the file is not read again next time.
func (s *Store) index(old, fresh Note, body string, mtime int64) (changed bool, err error) {
	hash := hashBody(body)
	diff := logs.Diff(
		logs.F("title", old.Title, fresh.Title),
		logs.F("priority", old.Priority, fresh.Priority),
		logs.F("status", old.Status, fresh.Status),
		logs.F("target", old.Target, fresh.Target),
		logs.F("tags", nonNil(old.Tags), nonNil(fresh.Tags)),
		logs.F("accounts", nonNil(old.Accounts), nonNil(fresh.Accounts)),
		logs.F("cards", nonNil(old.Cards), nonNil(fresh.Cards)),
		logs.F("goals", nonNil(old.Goals), nonNil(fresh.Goals)),
		logs.F("body", short(old.BodyHash), short(hash)),
	)
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if len(diff) > 0 {
		if _, err := tx.Exec(
			`UPDATE notes SET title = ?, priority = ?, status = ?, target = ?, target_phrase = ?,
				edit_count = edit_count + 1, updated_at = datetime('now') WHERE id = ?`,
			fresh.Title, fresh.Priority, fresh.Status, fresh.Target, fresh.TargetPhrase, old.ID); err != nil {
			return false, err
		}
		for _, table := range []string{"note_tags", "note_links"} {
			if _, err := tx.Exec(`DELETE FROM `+table+` WHERE note_id = ?`, old.ID); err != nil {
				return false, err
			}
		}
		if err := writeTags(tx, old.ID, fresh.Tags); err != nil {
			return false, err
		}
		if err := writeLinks(tx, fresh); err != nil {
			return false, err
		}
		if err := logs.RecordEdit(tx, logs.Actor, "note", old.ID, diff); err != nil {
			return false, err
		}
	} else if old.TargetPhrase != fresh.TargetPhrase {
		// The day did not move but the words for it did — worth keeping,
		// not worth an edit.
		if _, err := tx.Exec(`UPDATE notes SET target_phrase = ? WHERE id = ?`, fresh.TargetPhrase, old.ID); err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec(`UPDATE notes SET mtime = ?, body_hash = ? WHERE id = ?`, mtime, hash, old.ID); err != nil {
		return false, err
	}
	return len(diff) > 0, tx.Commit()
}

// Report is what Sync did.
type Report struct {
	Reindexed []string          // files whose row was brought up to date
	Adopted   []string          // "old name → new name" for files that became notes
	Missing   []string          // rows whose file is gone
	Problems  map[string]string // file → why it could not be read
}

func (r Report) Empty() bool {
	return len(r.Reindexed) == 0 && len(r.Adopted) == 0 && len(r.Missing) == 0 && len(r.Problems) == 0
}

// Sync is the deliberate pass over the directory: every row's file is read
// again whatever its mtime says, indexed, and written back in canonical form
// when the file differs from it — which is how a phrase typed into the target
// becomes a day. Then every .md file no row knows is adopted as a new note,
// named the way pecunia names them, and the old file removed. Rows whose file
// is gone are reported and kept: dropping a note is the owner's call.
func (s *Store) Sync() (Report, error) {
	now := time.Now()
	r := Report{Problems: map[string]string{}}
	all, err := s.rows(`SELECT ` + columns + ` FROM notes n ORDER BY n.id`)
	if err != nil {
		return r, err
	}
	known := map[string]bool{}
	for _, n := range all {
		known[n.Path] = true
		st, err := os.Stat(s.Path(n))
		if os.IsNotExist(err) {
			r.Missing = append(r.Missing, n.Path)
			continue
		}
		if err != nil {
			r.Problems[n.Path] = err.Error()
			continue
		}
		fresh, doc, err := s.read(n, now)
		if err != nil {
			r.Problems[n.Path] = err.Error()
			continue
		}
		changed, err := s.index(n, fresh, doc.Body, st.ModTime().UnixNano())
		if err != nil {
			return r, err
		}
		current, err := os.ReadFile(s.Path(n))
		if err != nil {
			return r, err
		}
		if string(current) != string(Render(fresh, doc.Body)) {
			mtime, hash, err := s.write(fresh, doc.Body)
			if err != nil {
				return r, err
			}
			if _, err := s.db.Exec(`UPDATE notes SET mtime = ?, body_hash = ? WHERE id = ?`, mtime, hash, n.ID); err != nil {
				return r, err
			}
			changed = true
		}
		if changed {
			r.Reindexed = append(r.Reindexed, n.Path)
		}
	}

	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || known[name] || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, name))
		if err != nil {
			r.Problems[name] = err.Error()
			continue
		}
		doc, err := Parse(data)
		if err != nil {
			r.Problems[name] = err.Error()
			continue
		}
		fresh, err := doc.Note(now)
		if err != nil {
			r.Problems[name] = err.Error()
			continue
		}
		if err := s.Create(&fresh, doc.Body); err != nil {
			r.Problems[name] = err.Error()
			continue
		}
		// Create wrote a canonical copy under the note's own name; the file
		// it came from has nothing left to say — unless it already had that
		// name, in which case it just was overwritten in place.
		if fresh.Path != name {
			if err := os.Remove(filepath.Join(s.dir, name)); err != nil {
				r.Problems[name] = fmt.Sprintf("adopted as %s, but the old file could not be removed: %v", fresh.Path, err)
			}
		}
		r.Adopted = append(r.Adopted, name+" → "+fresh.Path)
	}
	return r, nil
}
