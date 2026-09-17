package backup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Object is one archive as the provider holds it.
type Object struct {
	Name string
	Size int64
	At   time.Time
}

// Provider is somewhere archives go. Put takes a path rather than a reader:
// the archive is already a file by the time it is sent, and S3 wants its size
// and hash before the first byte.
type Provider interface {
	Put(name, src string) error
	Get(name string) (io.ReadCloser, error)
	List() ([]Object, error) // sorted by name
	Delete(name string) error
}

// OpenProvider is the one the config names.
func OpenProvider(cfg Config) (Provider, error) {
	switch cfg.Provider {
	case "local":
		return &Local{Dir: cfg.Local.Dir}, nil
	case "s3":
		return &S3{cfg.S3}, nil
	case "dropbox":
		return NewDropbox(cfg.Dropbox), nil
	case "gdrive":
		return NewGDrive(cfg.GDrive), nil
	}
	return nil, fmt.Errorf("provider %q — one of %s", cfg.Provider, strings.Join(Providers, ", "))
}

// Local is a directory: an external drive, a mount, a place another tool
// syncs. Also what the tests and a dry run use.
type Local struct {
	Dir string
}

func (l *Local) Put(name, src string) error {
	if err := os.MkdirAll(l.Dir, 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	// Written beside its final name and renamed in, so a reader never sees
	// half an archive.
	tmp := filepath.Join(l.Dir, "."+name+".part")
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, filepath.Join(l.Dir, name))
}

func (l *Local) Get(name string) (io.ReadCloser, error) {
	f, err := os.Open(filepath.Join(l.Dir, name))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("no %s in %s", name, l.Dir)
	}
	return f, err
}

// List is every archive in the directory: files whose name Stamp accepts.
func (l *Local) List() ([]Object, error) {
	entries, err := os.ReadDir(l.Dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var objs []Object
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, ok := Stamp(e.Name()); !ok && !isArchive(e.Name()) {
			continue
		}
		st, err := e.Info()
		if err != nil {
			return nil, err
		}
		objs = append(objs, Object{Name: e.Name(), Size: st.Size(), At: st.ModTime()})
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].Name < objs[j].Name })
	return objs, nil
}

func (l *Local) Delete(name string) error {
	return os.Remove(filepath.Join(l.Dir, name))
}

// isArchive is the looser test List uses: anything ending the way Name ends,
// so an archive renamed by hand still shows.
func isArchive(name string) bool {
	return filepath.Ext(name) == ".gz" || filepath.Ext(name) == ageSuffix
}
