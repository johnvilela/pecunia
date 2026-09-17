package backup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pecunia/internal/db"
)

const sampleTOML = `# pecunia backup
provider = "s3"
every = "2/day"
keep = 14
passphrase = "hunter \"two\""

[s3]
bucket = "my-bucket"
prefix = "pecunia"
region = "us-east-1"
endpoint = "https://s3.example.com"
access_key = "AKIA"
secret_key = "s3cr3t"

[local]
dir = "/mnt/usb/pecunia"

[dropbox]
folder = "/Apps/pecunia"
app_key = "key"
app_secret = "sec"
refresh_token = "rt"
`

func TestParseConfig(t *testing.T) {
	t.Run("reads every field", func(t *testing.T) {
		cfg, err := Parse([]byte(sampleTOML))
		if err != nil {
			t.Fatal(err)
		}
		want := Config{
			Provider: "s3", Every: "2/day", Keep: 14, Passphrase: `hunter "two"`,
			S3:      S3Config{Bucket: "my-bucket", Prefix: "pecunia", Region: "us-east-1", Endpoint: "https://s3.example.com", AccessKey: "AKIA", SecretKey: "s3cr3t"},
			Local:   LocalConfig{Dir: "/mnt/usb/pecunia"},
			Dropbox: DropboxConfig{Folder: "/Apps/pecunia", AppKey: "key", AppSecret: "sec", RefreshToken: "rt"},
		}
		if cfg != want {
			t.Fatalf("got %+v\nwant %+v", cfg, want)
		}
	})

	t.Run("render then parse is the identity", func(t *testing.T) {
		cfg, err := Parse([]byte(sampleTOML))
		if err != nil {
			t.Fatal(err)
		}
		again, err := Parse(Render(cfg))
		if err != nil {
			t.Fatalf("%v\n%s", err, Render(cfg))
		}
		if again != cfg {
			t.Fatalf("got %+v\nwant %+v", again, cfg)
		}
	})

	t.Run("keep is 0 and encryption off when left out", func(t *testing.T) {
		cfg, err := Parse([]byte("provider = \"local\"\n[local]\ndir = \"/tmp/x\"\n"))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Keep != 0 || cfg.Passphrase != "" || cfg.Every != "" {
			t.Fatalf("got %+v", cfg)
		}
	})

	bad := []struct{ name, in, want string }{
		{"unknown key", "provider = \"s3\"\nprovder = \"x\"\n", "line 2: unknown key \"provder\""},
		{"unknown section", "[gdrive]\ntoken = \"x\"\n", "line 1: unknown section \"gdrive\""},
		{"unknown key in section", "[s3]\nbuckt = \"x\"\n", "line 2: unknown key \"s3.buckt\""},
		{"duplicate key", "keep = 1\nkeep = 2\n", "line 2: duplicate key \"keep\""},
		{"number for a string", "provider = 3\n", "line 1: provider wants a quoted string"},
		{"string for a number", "keep = \"3\"\n", "line 1: keep wants a number"},
		{"unclosed string", "provider = \"s3\n", "line 1: unclosed string"},
		{"no equals", "provider\n", "line 1: want key = value"},
		{"unclosed section", "[s3\n", "line 1: unclosed section"},
	}
	for _, c := range bad {
		t.Run("rejects "+c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.in))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"no provider", "every = \"1/day\"\n", "provider"},
		{"unknown provider", "provider = \"gdrive\"\n", "provider \"gdrive\" — one of local, s3, dropbox"},
		{"dropbox without app key", "provider = \"dropbox\"\n[dropbox]\nrefresh_token = \"rt\"\n", "dropbox.app_key"},
		{"dropbox without refresh token", "provider = \"dropbox\"\n[dropbox]\napp_key = \"k\"\n", "dropbox.refresh_token"},
		{"dropbox folder must start with a slash", "provider = \"dropbox\"\n[dropbox]\napp_key = \"k\"\nrefresh_token = \"rt\"\nfolder = \"pecunia\"\n", "dropbox.folder"},
		{"local without dir", "provider = \"local\"\n", "local.dir"},
		{"s3 without bucket", "provider = \"s3\"\n[s3]\nregion = \"x\"\naccess_key = \"a\"\nsecret_key = \"b\"\n", "s3.bucket"},
		{"s3 without keys", "provider = \"s3\"\n[s3]\nbucket = \"b\"\nregion = \"x\"\n", "s3.access_key"},
		{"bad schedule", "provider = \"local\"\nevery = \"0/day\"\n[local]\ndir = \"/x\"\n", "every"},
		{"negative keep", "provider = \"local\"\nkeep = -1\n[local]\ndir = \"/x\"\n", "keep"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := Parse([]byte(c.in))
			if err != nil {
				t.Fatal(err)
			}
			err = cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want it to mention %q", err, c.want)
			}
		})
	}

	t.Run("dropbox with a key and a token is enough", func(t *testing.T) {
		cfg, _ := Parse([]byte("provider = \"dropbox\"\n[dropbox]\napp_key = \"k\"\nrefresh_token = \"rt\"\n"))
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("s3 wants no region when an endpoint is set", func(t *testing.T) {
		cfg, _ := Parse([]byte("provider = \"s3\"\n[s3]\nbucket = \"b\"\nendpoint = \"https://x\"\naccess_key = \"a\"\nsecret_key = \"b\"\n"))
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("s3 without endpoint wants a region", func(t *testing.T) {
		cfg, _ := Parse([]byte("provider = \"s3\"\n[s3]\nbucket = \"b\"\naccess_key = \"a\"\nsecret_key = \"b\"\n"))
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "s3.region") {
			t.Fatalf("err %v", err)
		}
	})
}

func TestConfigPath(t *testing.T) {
	t.Run("beside the database", func(t *testing.T) {
		t.Setenv("PECUNIA_DB", "/home/x/.config/pecunia/pecunia.db")
		got, err := ConfigPath()
		if err != nil || got != "/home/x/.config/pecunia/backup.toml" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("a dev build keeps its own", func(t *testing.T) {
		old := db.DevDB
		db.DevDB = "/repo/pecunia.dev.db"
		t.Cleanup(func() { db.DevDB = old })
		t.Setenv("PECUNIA_DB", "/home/x/.config/pecunia/pecunia.db")
		got, err := ConfigPath()
		if err != nil || got != "/repo/pecunia.dev.backup.toml" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
}

func TestLoadSave(t *testing.T) {
	t.Run("missing file is ErrNoConfig", func(t *testing.T) {
		t.Setenv("PECUNIA_DB", filepath.Join(t.TempDir(), "pecunia.db"))
		_, err := Load()
		if !errors.Is(err, ErrNoConfig) {
			t.Fatalf("err %v, want ErrNoConfig", err)
		}
	})

	t.Run("save writes 0600 and load reads it back", func(t *testing.T) {
		t.Setenv("PECUNIA_DB", filepath.Join(t.TempDir(), "pecunia.db"))
		cfg := Config{Provider: "local", Every: "1/day", Keep: 3, Local: LocalConfig{Dir: "/x"}}
		if err := Save(cfg); err != nil {
			t.Fatal(err)
		}
		path, _ := ConfigPath()
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v, want 0600", st.Mode().Perm())
		}
		got, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if got != cfg {
			t.Fatalf("got %+v\nwant %+v", got, cfg)
		}
	})

	t.Run("the environment overrides the secrets", func(t *testing.T) {
		t.Setenv("PECUNIA_DB", filepath.Join(t.TempDir(), "pecunia.db"))
		cfg := Config{Provider: "s3", S3: S3Config{Bucket: "b", Region: "r", AccessKey: "file-ak", SecretKey: "file-sk"}, Passphrase: "file-pw"}
		if err := Save(cfg); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PECUNIA_BACKUP_PASSPHRASE", "env-pw")
		t.Setenv("PECUNIA_BACKUP_S3_ACCESS_KEY", "env-ak")
		t.Setenv("PECUNIA_BACKUP_S3_SECRET_KEY", "env-sk")
		t.Setenv("PECUNIA_BACKUP_DROPBOX_REFRESH_TOKEN", "env-rt")
		got, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if got.Passphrase != "env-pw" || got.S3.AccessKey != "env-ak" || got.S3.SecretKey != "env-sk" || got.Dropbox.RefreshToken != "env-rt" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("a broken file names the line", func(t *testing.T) {
		t.Setenv("PECUNIA_DB", filepath.Join(t.TempDir(), "pecunia.db"))
		path, _ := ConfigPath()
		os.MkdirAll(filepath.Dir(path), 0o700)
		os.WriteFile(path, []byte("provider = \"local\"\nkeep = x\n"), 0o600)
		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "backup.toml:2") {
			t.Fatalf("err %v", err)
		}
	})
}
