package backup

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// providerSuite is what every provider must do, run against each one.
func providerSuite(t *testing.T, open func(t *testing.T) Provider) {
	src := func(t *testing.T, content string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "src")
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	read := func(t *testing.T, p Provider, name string) string {
		t.Helper()
		r, err := p.Get(name)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		b, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	t.Run("put then get", func(t *testing.T) {
		p := open(t)
		if err := p.Put("pecunia-20260916T140500Z.tar.gz", src(t, "ledger")); err != nil {
			t.Fatal(err)
		}
		if got := read(t, p, "pecunia-20260916T140500Z.tar.gz"); got != "ledger" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("list is sorted by name with sizes", func(t *testing.T) {
		p := open(t)
		for _, n := range []string{"pecunia-20260916T140500Z.tar.gz", "pecunia-20260915T140500Z.tar.gz"} {
			if err := p.Put(n, src(t, "12345")); err != nil {
				t.Fatal(err)
			}
		}
		objs, err := p.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(objs) != 2 {
			t.Fatalf("got %d objects: %+v", len(objs), objs)
		}
		if !sort.SliceIsSorted(objs, func(i, j int) bool { return objs[i].Name < objs[j].Name }) {
			t.Fatalf("not sorted: %+v", objs)
		}
		if objs[0].Name != "pecunia-20260915T140500Z.tar.gz" || objs[0].Size != 5 {
			t.Fatalf("first %+v", objs[0])
		}
	})

	t.Run("list of nothing is empty", func(t *testing.T) {
		objs, err := open(t).List()
		if err != nil || len(objs) != 0 {
			t.Fatalf("got %+v, %v", objs, err)
		}
	})

	t.Run("delete removes one", func(t *testing.T) {
		p := open(t)
		p.Put("a.tar.gz", src(t, "a"))
		p.Put("b.tar.gz", src(t, "b"))
		if err := p.Delete("a.tar.gz"); err != nil {
			t.Fatal(err)
		}
		objs, _ := p.List()
		if len(objs) != 1 || objs[0].Name != "b.tar.gz" {
			t.Fatalf("got %+v", objs)
		}
	})

	t.Run("get of a missing name says so", func(t *testing.T) {
		_, err := open(t).Get("nope.tar.gz")
		if err == nil || !strings.Contains(err.Error(), "nope.tar.gz") {
			t.Fatalf("err %v", err)
		}
	})
}

func TestLocalProvider(t *testing.T) {
	providerSuite(t, func(t *testing.T) Provider {
		return &Local{Dir: filepath.Join(t.TempDir(), "backups")}
	})

	t.Run("creates the directory 0700", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "deep", "backups")
		p := &Local{Dir: dir}
		f := filepath.Join(t.TempDir(), "src")
		os.WriteFile(f, []byte("x"), 0o600)
		if err := p.Put("a.tar.gz", f); err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(dir)
		if err != nil || st.Mode().Perm() != 0o700 {
			t.Fatalf("dir %v, %v", st.Mode(), err)
		}
		st, _ = os.Stat(filepath.Join(dir, "a.tar.gz"))
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("file mode %v", st.Mode().Perm())
		}
	})

	t.Run("list ignores other files", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o600)
		os.Mkdir(filepath.Join(dir, "sub"), 0o700)
		objs, err := (&Local{Dir: dir}).List()
		if err != nil || len(objs) != 0 {
			t.Fatalf("got %+v, %v", objs, err)
		}
	})
}

func TestOpenProvider(t *testing.T) {
	p, err := OpenProvider(Config{Provider: "local", Local: LocalConfig{Dir: "/x"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*Local); !ok {
		t.Fatalf("got %T", p)
	}
	p, err = OpenProvider(Config{Provider: "s3", S3: S3Config{Bucket: "b", Region: "r", AccessKey: "a", SecretKey: "s"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*S3); !ok {
		t.Fatalf("got %T", p)
	}
	p, err = OpenProvider(Config{Provider: "dropbox", Dropbox: DropboxConfig{AppKey: "k", RefreshToken: "rt"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*Dropbox); !ok {
		t.Fatalf("got %T", p)
	}
	if _, err := OpenProvider(Config{Provider: "gdrive"}); err == nil {
		t.Fatal("gdrive accepted")
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "src")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
