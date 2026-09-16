package notes

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"pecunia/internal/db"
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
