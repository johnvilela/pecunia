package backup

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Result is what one run did.
type Result struct {
	Name   string
	Size   int64
	Pruned []string // archives removed to stay within keep, oldest first
}

// Run makes one archive, sends it, and prunes what keep no longer covers.
// The archive is built in a temporary file first: the provider gets either
// the whole thing or nothing.
func Run(cfg Config, dbPath, notesDir string, at time.Time) (Result, error) {
	if err := cfg.Validate(); err != nil {
		return Result{}, err
	}
	p, err := OpenProvider(cfg)
	if err != nil {
		return Result{}, err
	}
	name := Name(at, cfg.Passphrase != "")
	tmp, err := os.CreateTemp("", "pecunia-backup-*")
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	var w io.Writer = tmp
	var closer io.Closer
	if cfg.Passphrase != "" {
		enc, err := Encrypt(tmp, cfg.Passphrase)
		if err != nil {
			return Result{}, err
		}
		w, closer = enc, enc
	}
	if err := Create(w, dbPath, notesDir); err != nil {
		return Result{}, err
	}
	if closer != nil {
		if err := closer.Close(); err != nil {
			return Result{}, err
		}
	}
	if err := tmp.Sync(); err != nil {
		return Result{}, err
	}
	st, err := tmp.Stat()
	if err != nil {
		return Result{}, err
	}
	if err := p.Put(name, tmp.Name()); err != nil {
		return Result{}, err
	}
	res := Result{Name: name, Size: st.Size()}
	if cfg.Keep > 0 {
		res.Pruned, err = Prune(p, cfg.Keep)
		if err != nil {
			return res, fmt.Errorf("sent %s, but pruning failed: %w", name, err)
		}
	}
	return res, nil
}

// Prune removes the oldest archives until keep remain. Only names Name made
// count — anything else in the same place is somebody else's and stays.
func Prune(p Provider, keep int) ([]string, error) {
	ours, err := archives(p)
	if err != nil {
		return nil, err
	}
	var pruned []string
	for len(ours) > keep {
		if err := p.Delete(ours[0].Name); err != nil {
			return pruned, err
		}
		pruned = append(pruned, ours[0].Name)
		ours = ours[1:]
	}
	return pruned, nil
}

// archives is List narrowed to our own names, oldest first.
func archives(p Provider) ([]Object, error) {
	objs, err := p.List()
	if err != nil {
		return nil, err
	}
	var ours []Object
	for _, o := range objs {
		if _, ok := Stamp(o.Name); ok {
			ours = append(ours, o)
		}
	}
	return ours, nil
}

// Archives lists what the provider holds of ours, oldest first.
func Archives(cfg Config) ([]Object, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	p, err := OpenProvider(cfg)
	if err != nil {
		return nil, err
	}
	return archives(p)
}

// Restored says what a restore put in place and where it moved what was there.
type Restored struct {
	Name     string
	OldDB    string // the database that was live, kept as .bak
	OldNotes string // the notes directory that was live, kept as .bak; empty when the archive had no notes
}

var errNoArchives = errors.New("no archives to restore from")

// Restore fetches an archive — the newest when name is empty — and puts its
// database and notes in place of the live ones, which move aside as .bak.
// Everything that can fail does so before the live data is touched: the
// download, the decryption, the unpacking, an integrity check on the copy.
func Restore(cfg Config, name, dbPath, notesDir string) (Restored, error) {
	if err := cfg.Validate(); err != nil {
		return Restored{}, err
	}
	p, err := OpenProvider(cfg)
	if err != nil {
		return Restored{}, err
	}
	if name == "" {
		ours, err := archives(p)
		if err != nil {
			return Restored{}, err
		}
		if len(ours) == 0 {
			return Restored{}, errNoArchives
		}
		name = ours[len(ours)-1].Name
	}
	if Encrypted(name) && cfg.Passphrase == "" {
		return Restored{}, fmt.Errorf("%s is encrypted — set the passphrase in backup.toml or PECUNIA_BACKUP_PASSPHRASE", name)
	}

	body, err := p.Get(name)
	if err != nil {
		return Restored{}, err
	}
	defer body.Close()
	var r io.Reader = body
	if Encrypted(name) {
		if r, err = Decrypt(body, cfg.Passphrase); err != nil {
			return Restored{}, err
		}
	}

	// Unpacked beside the database, so the final rename is on one filesystem.
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return Restored{}, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dbPath), ".pecunia-restore-")
	if err != nil {
		return Restored{}, err
	}
	defer os.RemoveAll(tmp)
	if err := Extract(r, tmp); err != nil {
		return Restored{}, fmt.Errorf("%s: %w", name, err)
	}
	if err := check(filepath.Join(tmp, dbEntry)); err != nil {
		return Restored{}, fmt.Errorf("%s: %w", name, err)
	}

	stamp := time.Now().UTC().Format(stamp)
	res := Restored{Name: name, OldDB: fmt.Sprintf("%s.%s.bak", dbPath, stamp)}
	// The -wal and -shm go with the file they belong to: left behind, SQLite
	// would replay the old WAL into the restored database.
	for _, ext := range []string{"", "-wal", "-shm"} {
		if err := os.Rename(dbPath+ext, res.OldDB+ext); err != nil && !os.IsNotExist(err) {
			return Restored{}, err
		}
	}
	if err := os.Rename(filepath.Join(tmp, dbEntry), dbPath); err != nil {
		return Restored{}, err
	}
	if _, err := os.Stat(filepath.Join(tmp, notesEntry)); err == nil {
		if _, err := os.Stat(notesDir); err == nil {
			res.OldNotes = fmt.Sprintf("%s.%s.bak", notesDir, stamp)
			if err := os.Rename(notesDir, res.OldNotes); err != nil {
				return res, err
			}
		}
		if err := os.Rename(filepath.Join(tmp, notesEntry), notesDir); err != nil {
			return res, err
		}
	}
	return res, nil
}

// check opens the unpacked database and asks SQLite whether it is whole.
func check(path string) error {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer conn.Close()
	var verdict string
	if err := conn.QueryRow("PRAGMA integrity_check").Scan(&verdict); err != nil {
		return fmt.Errorf("integrity check: %w", err)
	}
	if verdict != "ok" {
		return fmt.Errorf("integrity check: %s", verdict)
	}
	return nil
}
