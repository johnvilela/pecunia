package main

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"time"

	"github.com/charmbracelet/huh"

	"pecunia/internal/backup"
	"pecunia/internal/core"
	"pecunia/internal/db"
	"pecunia/internal/notes"
)

const backupHelp = `Back the database and the notes up to somewhere else.

Usage:
  pecunia backup [command] [flags]

Commands:
  (none)            where backups go, how often, what is there
  setup             choose a provider and fill it in (a form, or the flags below)
  run               make one archive and send it now
  list              the archives the provider holds, oldest first
  restore [NAME]    put an archive back in place of the live data (the newest
                    when NAME is left out; -y skips the confirmation)
  schedule [EVERY]  run on a timer: 2/day, 3/week, daily, weekly — or off

Setup flags (all optional; with none, a form asks):
  --provider P      local or s3
  --dir PATH        local: the directory
  --bucket B        s3: the bucket
  --prefix P        s3: key prefix inside the bucket (default pecunia)
  --region R        s3: the region (AWS; optional with an endpoint)
  --endpoint URL    s3: an S3-compatible service — MinIO, R2, B2
  --access-key K    s3: the access key
  --secret-key K    s3: the secret key
  --every EVERY     schedule, as above
  --keep N          archives to keep on the provider; 0 keeps them all
  --passphrase P    encrypt the archives (age); empty leaves them plain

An archive is pecunia-<moment>.tar.gz: a consistent snapshot of pecunia.db
and every file under the notes directory. With a passphrase it is an age
file (.tar.gz.age) that the age tool can open too. The settings live in
backup.toml beside the database, 0600; PECUNIA_BACKUP_PASSPHRASE,
PECUNIA_BACKUP_S3_ACCESS_KEY and PECUNIA_BACKUP_S3_SECRET_KEY override the
secrets in it. The schedule is a systemd user timer that runs
"pecunia backup run"; without systemd, schedule prints the crontab line.
Start with pecunia backup setup.
`

func runBackup(args []string) error {
	if len(args) == 0 {
		return backupStatus()
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "-h", "--help", "help":
		fmt.Fprint(out, backupHelp)
		return nil
	case "setup":
		return backupSetup(rest)
	case "run":
		return backupRun()
	case "list":
		return backupList()
	case "restore":
		return backupRestore(rest)
	case "schedule":
		return backupSchedule(rest)
	}
	return fmt.Errorf("unknown command %q — setup, run, list, restore or schedule", sub)
}

// target is where the archives go, in words.
func target(cfg backup.Config) string {
	switch cfg.Provider {
	case "local":
		return cfg.Local.Dir
	case "s3":
		t := "s3://" + cfg.S3.Bucket
		if cfg.S3.Prefix != "" {
			t += "/" + cfg.S3.Prefix
		}
		if cfg.S3.Endpoint != "" {
			t += " at " + cfg.S3.Endpoint
		}
		return t
	}
	return cfg.Provider
}

func backupStatus() error {
	cfg, err := backup.Load()
	if errors.Is(err, backup.ErrNoConfig) {
		fmt.Fprintln(out, "no backup configured — run pecunia backup setup")
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "provider   %s — %s\n", cfg.Provider, target(cfg))
	keep := "keep all"
	if cfg.Keep > 0 {
		keep = fmt.Sprintf("keep %d", cfg.Keep)
	}
	enc := "not encrypted"
	if cfg.Passphrase != "" {
		enc = "encrypted"
	}
	fmt.Fprintf(out, "archives   %s, %s\n", keep, enc)
	fmt.Fprintf(out, "schedule   %s\n", scheduleLine(cfg))
	return nil
}

// scheduleLine is the schedule and whether the timer that runs it is there.
func scheduleLine(cfg backup.Config) string {
	if cfg.Every == "" {
		return "no schedule — pecunia backup schedule 1/day"
	}
	e, err := backup.ParseEvery(cfg.Every)
	if err != nil {
		return cfg.Every + " (" + err.Error() + ")"
	}
	line := fmt.Sprintf("every %s (OnCalendar=%s)", e, e.OnCalendar())
	if !backup.HaveSystemd() {
		return line + ", no systemd — crontab: " + e.Cron() + " pecunia backup run"
	}
	installed, err := backup.TimerInstalled()
	switch {
	case err != nil:
		return line + ", timer: " + err.Error()
	case installed:
		return line + ", timer installed"
	}
	return line + ", timer NOT installed — pecunia backup schedule " + e.String()
}

func backupSetup(args []string) error {
	fs := flag.NewFlagSet("backup setup", flag.ContinueOnError)
	fs.SetOutput(out)
	// Start from what is there, so a single flag changes one thing.
	cfg, err := backup.Load()
	if err != nil && !errors.Is(err, backup.ErrNoConfig) {
		return err
	}
	fs.StringVar(&cfg.Provider, "provider", cfg.Provider, "")
	fs.StringVar(&cfg.Local.Dir, "dir", cfg.Local.Dir, "")
	fs.StringVar(&cfg.S3.Bucket, "bucket", cfg.S3.Bucket, "")
	fs.StringVar(&cfg.S3.Prefix, "prefix", cfg.S3.Prefix, "")
	fs.StringVar(&cfg.S3.Region, "region", cfg.S3.Region, "")
	fs.StringVar(&cfg.S3.Endpoint, "endpoint", cfg.S3.Endpoint, "")
	fs.StringVar(&cfg.S3.AccessKey, "access-key", cfg.S3.AccessKey, "")
	fs.StringVar(&cfg.S3.SecretKey, "secret-key", cfg.S3.SecretKey, "")
	fs.StringVar(&cfg.Every, "every", cfg.Every, "")
	fs.IntVar(&cfg.Keep, "keep", cfg.Keep, "")
	fs.StringVar(&cfg.Passphrase, "passphrase", cfg.Passphrase, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(args) == 0 {
		if cfg, err = setupForm(cfg); err != nil {
			return err
		}
	}
	if cfg.Provider == "s3" && cfg.S3.Prefix == "" && !flagSet(fs, "prefix") {
		cfg.S3.Prefix = "pecunia"
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := backup.Save(cfg); err != nil {
		return err
	}
	path, _ := backup.ConfigPath()
	fmt.Fprintf(out, "saved %s — archives go to %s\n", path, target(cfg))
	if cfg.Every != "" {
		return installSchedule(cfg)
	}
	fmt.Fprintln(out, "no schedule yet — pecunia backup schedule 1/day, or pecunia backup run for one now")
	return nil
}

func flagSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// setupForm asks for everything: the provider first, then its own fields.
func setupForm(cfg backup.Config) (backup.Config, error) {
	if cfg.Provider == "" {
		cfg.Provider = "local"
	}
	keep := strconv.Itoa(cfg.Keep)
	digits := func(s string) error {
		if _, err := strconv.Atoi(s); err != nil {
			return errors.New("a whole number")
		}
		return nil
	}
	every := func(s string) error {
		if s == "" {
			return nil
		}
		_, err := backup.ParseEvery(s)
		return err
	}
	first := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Where").Options(
			huh.NewOption("a directory (external drive, a synced folder)", "local"),
			huh.NewOption("an S3 bucket (AWS, MinIO, R2, B2)", "s3"),
		).Value(&cfg.Provider),
		huh.NewInput().Title("How often").Description("2/day, 3/week, daily, weekly — or empty for no timer").Value(&cfg.Every).Validate(every),
		huh.NewInput().Title("Keep").Description("archives to keep; 0 keeps them all").Value(&keep).Validate(digits),
		huh.NewInput().Title("Passphrase").Description("encrypts the archives; empty leaves them plain").EchoMode(huh.EchoModePassword).Value(&cfg.Passphrase),
	).Title("Backup")).WithTheme(huh.ThemeCharm())
	if err := first.Run(); err != nil {
		return cfg, formErr(err)
	}
	cfg.Keep, _ = strconv.Atoi(keep)

	var fields []huh.Field
	switch cfg.Provider {
	case "local":
		fields = []huh.Field{huh.NewInput().Title("Directory").Value(&cfg.Local.Dir)}
	case "s3":
		if cfg.S3.Prefix == "" {
			cfg.S3.Prefix = "pecunia"
		}
		fields = []huh.Field{
			huh.NewInput().Title("Bucket").Value(&cfg.S3.Bucket),
			huh.NewInput().Title("Prefix").Description("key prefix inside the bucket").Value(&cfg.S3.Prefix),
			huh.NewInput().Title("Endpoint").Description("empty for AWS; the URL of a MinIO, R2 or B2 otherwise").Value(&cfg.S3.Endpoint),
			huh.NewInput().Title("Region").Description("AWS: required; elsewhere: what the service says, or empty").Value(&cfg.S3.Region),
			huh.NewInput().Title("Access key").Value(&cfg.S3.AccessKey),
			huh.NewInput().Title("Secret key").EchoMode(huh.EchoModePassword).Value(&cfg.S3.SecretKey),
		}
	}
	second := huh.NewForm(huh.NewGroup(fields...).Title(cfg.Provider)).WithTheme(huh.ThemeCharm())
	if err := second.Run(); err != nil {
		return cfg, formErr(err)
	}
	return cfg, nil
}

func formErr(err error) error {
	if errors.Is(err, huh.ErrUserAborted) {
		return core.ErrCancelled
	}
	return err
}

// paths are the live database and notes directory.
func paths() (dbPath, notesDir string, err error) {
	if dbPath, err = db.Path(); err != nil {
		return
	}
	notesDir, err = notes.Dir()
	return
}

func backupRun() error {
	cfg, err := backup.Load()
	if err != nil {
		return err
	}
	dbPath, notesDir, err := paths()
	if err != nil {
		return err
	}
	res, err := backup.Run(cfg, dbPath, notesDir, time.Now())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "sent %s (%s) to %s\n", res.Name, size(res.Size), target(cfg))
	for _, name := range res.Pruned {
		fmt.Fprintf(out, "pruned %s\n", name)
	}
	return nil
}

func backupList() error {
	cfg, err := backup.Load()
	if err != nil {
		return err
	}
	objs, err := backup.Archives(cfg)
	if err != nil {
		return err
	}
	if len(objs) == 0 {
		fmt.Fprintf(out, "no archives at %s\n", target(cfg))
		return nil
	}
	for _, o := range objs {
		at, _ := backup.Stamp(o.Name)
		enc := ""
		if backup.Encrypted(o.Name) {
			enc = "  encrypted"
		}
		fmt.Fprintf(out, "%s  %s  %8s%s\n", o.Name, at.Local().Format("2006-01-02 15:04"), size(o.Size), enc)
	}
	return nil
}

func backupRestore(args []string) error {
	fs := flag.NewFlagSet("backup restore", flag.ContinueOnError)
	fs.SetOutput(out)
	var yes bool
	fs.BoolVar(&yes, "y", false, "skip the confirmation")
	fs.BoolVar(&yes, "yes", false, "skip the confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	name := fs.Arg(0)
	cfg, err := backup.Load()
	if err != nil {
		return err
	}
	dbPath, notesDir, err := paths()
	if err != nil {
		return err
	}
	if !yes {
		which := name
		if which == "" {
			which = "the newest archive"
		}
		ok, err := core.Confirm("Restore "+which+"?",
			"The live database and notes move aside as .bak; nothing is lost, but pecunia will show what the archive holds.", "Restore")
		if err != nil {
			return err
		}
		if !ok {
			return core.ErrCancelled
		}
	}
	res, err := backup.Restore(cfg, name, dbPath, notesDir)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "restored %s\n", res.Name)
	fmt.Fprintf(out, "the previous database is at %s\n", res.OldDB)
	if res.OldNotes != "" {
		fmt.Fprintf(out, "the previous notes are at %s\n", res.OldNotes)
	}
	return nil
}

func backupSchedule(args []string) error {
	cfg, err := backup.Load()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		fmt.Fprintln(out, scheduleLine(cfg))
		return nil
	}
	if args[0] == "off" {
		cfg.Every = ""
		if err := backup.Save(cfg); err != nil {
			return err
		}
		if backup.HaveSystemd() {
			if err := backup.UninstallTimer(); err != nil {
				return err
			}
		}
		fmt.Fprintln(out, "schedule off")
		return nil
	}
	e, err := backup.ParseEvery(args[0])
	if err != nil {
		return err
	}
	cfg.Every = e.String()
	if err := backup.Save(cfg); err != nil {
		return err
	}
	return installSchedule(cfg)
}

// installSchedule puts the timer in for the config's schedule, or prints the
// crontab line where there is no systemd to hold one.
func installSchedule(cfg backup.Config) error {
	e, err := backup.ParseEvery(cfg.Every)
	if err != nil {
		return err
	}
	exe, err := selfExe()
	if err != nil {
		return err
	}
	if !backup.HaveSystemd() {
		fmt.Fprintf(out, "every %s — no systemd here; add this line to your crontab:\n%s %s backup run\n", e, e.Cron(), exe)
		return nil
	}
	if err := backup.InstallTimer(exe, e); err != nil {
		return err
	}
	fmt.Fprintf(out, "every %s — timer installed (OnCalendar=%s)\n", e, e.OnCalendar())
	return nil
}

func size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return strconv.FormatInt(n, 10) + " B"
}
