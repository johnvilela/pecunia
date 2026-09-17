package backup

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
)

// The archive is a tar.gz with pecunia.db at its root and the note files
// under notes/. Nothing else — not backup.toml, which holds the credentials
// that would then travel to the very place they open.
const (
	dbEntry    = "pecunia.db"
	notesEntry = "notes"
	suffix     = ".tar.gz"
	ageSuffix  = ".age"
	stamp      = "20060102T150405Z"
)

// Name is the archive's file name for a run at t: the moment, UTC, so the
// names sort into time order wherever they land.
func Name(t time.Time, encrypted bool) string {
	name := "pecunia-" + t.UTC().Format(stamp) + suffix
	if encrypted {
		name += ageSuffix
	}
	return name
}

// Stamp reads the moment back out of a name Name made; anything else in the
// bucket — a stray file, another tool's — is not ours and reports false.
func Stamp(name string) (time.Time, bool) {
	rest, ok := strings.CutPrefix(name, "pecunia-")
	if !ok {
		return time.Time{}, false
	}
	rest = strings.TrimSuffix(rest, ageSuffix)
	rest, ok = strings.CutSuffix(rest, suffix)
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(stamp, rest)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// Encrypted says whether a name Name made carries the encryption suffix.
func Encrypted(name string) bool { return strings.HasSuffix(name, ageSuffix) }

// Create writes the archive: a consistent snapshot of the database (a copy of
// the file alone could miss what sits in the -wal beside it) and every file
// under notesDir, which may not exist.
func Create(w io.Writer, dbPath, notesDir string) error {
	if _, err := os.Stat(dbPath); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "pecunia-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	snap := filepath.Join(tmp, dbEntry)
	if err := snapshot(dbPath, snap); err != nil {
		return err
	}

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	if err := addFile(tw, snap, dbEntry); err != nil {
		return err
	}
	err = filepath.WalkDir(notesDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(notesDir, p)
		if err != nil {
			return err
		}
		return addFile(tw, p, path.Join(notesEntry, filepath.ToSlash(rel)))
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// snapshot copies the database as SQLite sees it — through the WAL — into a
// fresh, compacted file. VACUUM INTO takes a read transaction, so a write in
// flight elsewhere neither blocks it nor lands half in the copy.
func snapshot(from, to string) error {
	conn, err := sql.Open("sqlite", from)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.Exec("VACUUM INTO ?", to); err != nil {
		return fmt.Errorf("snapshot %s: %w", from, err)
	}
	return nil
}

func addFile(tw *tar.Writer, p, name string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	hdr := &tar.Header{Name: name, Mode: 0o600, Size: st.Size(), ModTime: st.ModTime()}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

// Extract unpacks an archive Create made into dir: pecunia.db at its root and
// notes/ beside it. Entries it does not recognise are refused rather than
// written, and so is a name that would climb out of dir.
func Extract(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("not a pecunia archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	sawDB := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := path.Clean(hdr.Name)
		switch {
		case name == dbEntry:
			sawDB = true
		case strings.HasPrefix(name, notesEntry+"/") && !strings.Contains(name, ".."):
		default:
			return fmt.Errorf("archive entry %q is not one pecunia makes", hdr.Name)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		dst := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, tr)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	if !sawDB {
		return errors.New("no pecunia.db in the archive")
	}
	return nil
}

// Encrypt wraps w so that what is written to it comes out as an age file
// locked with the passphrase. Close finishes the file. Anyone with the
// passphrase and the age tool can open it without pecunia.
func Encrypt(w io.Writer, passphrase string) (io.WriteCloser, error) {
	r, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, err
	}
	return age.Encrypt(w, r)
}

// Decrypt opens an age file Encrypt made.
func Decrypt(r io.Reader, passphrase string) (io.Reader, error) {
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, err
	}
	out, err := age.Decrypt(r, id)
	if errors.Is(err, age.ErrIncorrectIdentity) {
		return nil, errors.New("wrong passphrase")
	}
	return out, err
}
