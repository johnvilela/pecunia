package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"pecunia/internal/db"
)

// Config is backup.toml: where the archives go, how often, how many to keep,
// and whether they are encrypted. Secrets live in the file too — the timer
// runs unattended, so there is nobody to ask — which is why the file is 0600
// and why the environment can override them for anyone who would rather keep
// them elsewhere.
type Config struct {
	Provider   string // "local" or "s3"
	Every      string // "2/day", "1/week"; empty means no timer
	Keep       int    // archives to keep on the provider; 0 keeps them all
	Passphrase string // encrypts the archive when set
	S3         S3Config
	Local      LocalConfig
	Dropbox    DropboxConfig
	GDrive     GDriveConfig
}

type S3Config struct {
	Bucket    string
	Prefix    string // key prefix inside the bucket, "pecunia" by default
	Region    string
	Endpoint  string // an S3-compatible service: MinIO, R2, B2; AWS when empty
	AccessKey string
	SecretKey string
}

type LocalConfig struct {
	Dir string
}

// DropboxConfig is an app the owner made at dropbox.com/developers and the
// refresh token "pecunia backup setup" got by sending them through its
// consent page once. Short-lived access tokens are minted from it on every
// run and never stored.
type DropboxConfig struct {
	Folder       string // where the archives go, "/Apps/pecunia" by default
	AppKey       string
	AppSecret    string // optional: PKCE covers a CLI, the secret is only sent when set
	RefreshToken string
}

// Providers are the ones this build knows, in the order they are offered.
// GDriveConfig is an OAuth client of type "Desktop app" the owner made in
// the Google Cloud console with the Drive API on, and the refresh token the
// consent flow got. The scope is drive.file: pecunia sees only what it made.
type GDriveConfig struct {
	Folder       string // a folder name at the top of My Drive, "pecunia" by default
	ClientID     string
	ClientSecret string
	RefreshToken string
}

var Providers = []string{"local", "s3", "dropbox", "gdrive"}

var ErrNoConfig = errors.New("no backup configured — run pecunia backup setup")

// ConfigPath is where backup.toml lives: beside the database, so a dev build
// keeps its own beside its own database and never reads the real one.
func ConfigPath() (string, error) {
	if db.DevDB != "" {
		return strings.TrimSuffix(db.DevDB, ".db") + ".backup.toml", nil
	}
	path, err := db.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "backup.toml"), nil
}

// Load reads backup.toml and lays the environment over its secrets. The
// result is syntactically sound and no more: Validate says whether it can run.
func Load() (Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return Config{}, err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, ErrNoConfig
	}
	if err != nil {
		return Config{}, err
	}
	cfg, err := Parse(raw)
	var le *lineError
	if errors.As(err, &le) {
		return Config{}, fmt.Errorf("%s:%d: %s", filepath.Base(path), le.n, le.msg)
	}
	if err != nil {
		return Config{}, err
	}
	if v := os.Getenv("PECUNIA_BACKUP_PASSPHRASE"); v != "" {
		cfg.Passphrase = v
	}
	if v := os.Getenv("PECUNIA_BACKUP_S3_ACCESS_KEY"); v != "" {
		cfg.S3.AccessKey = v
	}
	if v := os.Getenv("PECUNIA_BACKUP_S3_SECRET_KEY"); v != "" {
		cfg.S3.SecretKey = v
	}
	if v := os.Getenv("PECUNIA_BACKUP_DROPBOX_REFRESH_TOKEN"); v != "" {
		cfg.Dropbox.RefreshToken = v
	}
	if v := os.Getenv("PECUNIA_BACKUP_GDRIVE_REFRESH_TOKEN"); v != "" {
		cfg.GDrive.RefreshToken = v
	}
	return cfg, nil
}

// Save writes backup.toml, 0600, creating the directory if needed.
func Save(cfg Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, Render(cfg), 0o600); err != nil {
		return err
	}
	// WriteFile's mode applies only to a new file; an existing one keeps its own.
	return os.Chmod(path, 0o600)
}

// Validate says whether the config is enough to run a backup with.
func (c Config) Validate() error {
	switch c.Provider {
	case "":
		return errors.New("no provider — one of " + strings.Join(Providers, ", "))
	case "local":
		if c.Local.Dir == "" {
			return errors.New("local.dir is empty")
		}
	case "s3":
		switch {
		case c.S3.Bucket == "":
			return errors.New("s3.bucket is empty")
		case c.S3.AccessKey == "":
			return errors.New("s3.access_key is empty (or set PECUNIA_BACKUP_S3_ACCESS_KEY)")
		case c.S3.SecretKey == "":
			return errors.New("s3.secret_key is empty (or set PECUNIA_BACKUP_S3_SECRET_KEY)")
		case c.S3.Endpoint == "" && c.S3.Region == "":
			return errors.New("s3.region is empty — needed without an endpoint")
		}
	case "dropbox":
		switch {
		case c.Dropbox.AppKey == "":
			return errors.New("dropbox.app_key is empty")
		case c.Dropbox.RefreshToken == "":
			return errors.New("dropbox.refresh_token is empty — run pecunia backup setup --provider dropbox (or set PECUNIA_BACKUP_DROPBOX_REFRESH_TOKEN)")
		case c.Dropbox.Folder != "" && !strings.HasPrefix(c.Dropbox.Folder, "/"):
			return errors.New("dropbox.folder must start with /")
		}
	case "gdrive":
		switch {
		case c.GDrive.ClientID == "":
			return errors.New("gdrive.client_id is empty")
		case c.GDrive.ClientSecret == "":
			return errors.New("gdrive.client_secret is empty")
		case c.GDrive.RefreshToken == "":
			return errors.New("gdrive.refresh_token is empty — run pecunia backup setup --provider gdrive (or set PECUNIA_BACKUP_GDRIVE_REFRESH_TOKEN)")
		case strings.Contains(c.GDrive.Folder, "/"):
			return errors.New("gdrive.folder is a folder name at the top of My Drive, not a path")
		}
	default:
		return fmt.Errorf("provider %q — one of %s", c.Provider, strings.Join(Providers, ", "))
	}
	if c.Every != "" {
		if _, err := ParseEvery(c.Every); err != nil {
			return fmt.Errorf("every: %w", err)
		}
	}
	if c.Keep < 0 {
		return errors.New("keep is negative")
	}
	return nil
}

// Render writes the canonical file: every key, in order, whether or not it is
// set, so the owner sees what there is to fill in.
func Render(c Config) []byte {
	var b strings.Builder
	b.WriteString("# pecunia backup — see pecunia backup -h\n")
	str := func(k, v string) { fmt.Fprintf(&b, "%s = %s\n", k, strconv.Quote(v)) }
	str("provider", c.Provider)
	str("every", c.Every)
	fmt.Fprintf(&b, "keep = %d\n", c.Keep)
	str("passphrase", c.Passphrase)
	b.WriteString("\n[s3]\n")
	str("bucket", c.S3.Bucket)
	str("prefix", c.S3.Prefix)
	str("region", c.S3.Region)
	str("endpoint", c.S3.Endpoint)
	str("access_key", c.S3.AccessKey)
	str("secret_key", c.S3.SecretKey)
	b.WriteString("\n[local]\n")
	str("dir", c.Local.Dir)
	b.WriteString("\n[dropbox]\n")
	str("folder", c.Dropbox.Folder)
	str("app_key", c.Dropbox.AppKey)
	str("app_secret", c.Dropbox.AppSecret)
	str("refresh_token", c.Dropbox.RefreshToken)
	b.WriteString("\n[gdrive]\n")
	str("folder", c.GDrive.Folder)
	str("client_id", c.GDrive.ClientID)
	str("client_secret", c.GDrive.ClientSecret)
	str("refresh_token", c.GDrive.RefreshToken)
	return []byte(b.String())
}

// Parse reads the subset of TOML the file is written in: [section] headers,
// key = "string" and key = integer, # comments. Anything else is refused with
// its line number — an unknown key is almost always a typo, and a typo that
// silently did nothing would be worse than a refusal.
func Parse(raw []byte) (Config, error) {
	var c Config
	fail := func(n int, format string, args ...any) (Config, error) {
		return c, &lineError{n, fmt.Sprintf(format, args...)}
	}
	section := ""
	seen := map[string]bool{}
	for i, line := range strings.Split(string(raw), "\n") {
		n := i + 1
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			end := strings.Index(line, "]")
			if end < 0 {
				return fail(n, "unclosed section")
			}
			section = strings.TrimSpace(line[1:end])
			if section != "s3" && section != "local" && section != "dropbox" && section != "gdrive" {
				return fail(n, "unknown section %q", section)
			}
			continue
		}
		key, rest, ok := strings.Cut(line, "=")
		if !ok {
			return fail(n, "want key = value")
		}
		key = strings.TrimSpace(key)
		full := key
		if section != "" {
			full = section + "." + key
		}
		if seen[full] {
			return fail(n, "duplicate key %q", full)
		}
		seen[full] = true
		val, err := parseValue(strings.TrimSpace(rest))
		if err != nil {
			return fail(n, "%v", err)
		}
		if err := c.set(full, val); err != nil {
			return fail(n, "%v", err)
		}
	}
	return c, nil
}

// lineError is a parse failure that knows its line, so Load can put the
// file name in front of it the way a compiler does.
type lineError struct {
	n   int
	msg string
}

func (e *lineError) Error() string { return fmt.Sprintf("line %d: %s", e.n, e.msg) }

// set puts one value where it goes, or says why it cannot.
func (c *Config) set(key string, val any) error {
	str := func(dst *string) error {
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("%s wants a quoted string", key)
		}
		*dst = s
		return nil
	}
	switch key {
	case "provider":
		return str(&c.Provider)
	case "every":
		return str(&c.Every)
	case "passphrase":
		return str(&c.Passphrase)
	case "keep":
		i, ok := val.(int)
		if !ok {
			return fmt.Errorf("%s wants a number", key)
		}
		c.Keep = i
		return nil
	case "s3.bucket":
		return str(&c.S3.Bucket)
	case "s3.prefix":
		return str(&c.S3.Prefix)
	case "s3.region":
		return str(&c.S3.Region)
	case "s3.endpoint":
		return str(&c.S3.Endpoint)
	case "s3.access_key":
		return str(&c.S3.AccessKey)
	case "s3.secret_key":
		return str(&c.S3.SecretKey)
	case "local.dir":
		return str(&c.Local.Dir)
	case "dropbox.folder":
		return str(&c.Dropbox.Folder)
	case "dropbox.app_key":
		return str(&c.Dropbox.AppKey)
	case "dropbox.app_secret":
		return str(&c.Dropbox.AppSecret)
	case "dropbox.refresh_token":
		return str(&c.Dropbox.RefreshToken)
	case "gdrive.folder":
		return str(&c.GDrive.Folder)
	case "gdrive.client_id":
		return str(&c.GDrive.ClientID)
	case "gdrive.client_secret":
		return str(&c.GDrive.ClientSecret)
	case "gdrive.refresh_token":
		return str(&c.GDrive.RefreshToken)
	}
	return fmt.Errorf("unknown key %q", key)
}

// parseValue reads a quoted string (with \" and \\ escapes, a trailing
// comment allowed after the closing quote) or an integer.
func parseValue(s string) (any, error) {
	if strings.HasPrefix(s, `"`) {
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			switch s[i] {
			case '\\':
				if i+1 >= len(s) {
					return nil, errors.New("unclosed string")
				}
				i++
				switch s[i] {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte(s[i])
				}
			case '"':
				if rest := strings.TrimSpace(s[i+1:]); rest != "" && !strings.HasPrefix(rest, "#") {
					return nil, fmt.Errorf("unexpected %q after the string", rest)
				}
				return b.String(), nil
			default:
				b.WriteByte(s[i])
			}
		}
		return nil, errors.New("unclosed string")
	}
	if i := strings.Index(s, "#"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil, fmt.Errorf("%q is neither a quoted string nor a number", s)
	}
	return n, nil
}
