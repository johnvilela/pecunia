package main

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"kakei/internal/core"
)

const upgradeHelp = `Usage:
  kakei upgrade [-y]

Checks GitHub for a newer release, shows the changelog of every version
you'd jump over, asks before replacing this binary, then migrates the
database with the new build.

Flags:
  -y, --yes   upgrade without asking (the changelog still prints)
`

// Test seams: the real values point at GitHub and this binary.
var (
	releasesURL = "https://api.github.com/repos/johnvilela/kakei/releases"
	selfExe     = os.Executable
)

type release struct {
	TagName    string  `json:"tag_name"`
	Body       string  `json:"body"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func runUpgrade(args []string) error {
	if len(args) > 0 && isHelpFlag(args[0]) {
		fmt.Fprint(out, upgradeHelp)
		return nil
	}
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var yes bool
	fs.BoolVar(&yes, "y", false, "skip the confirmation")
	fs.BoolVar(&yes, "yes", false, "skip the confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(releasesURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetching releases: %s", resp.Status)
	}
	var all []release
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		return err
	}

	pending := releasesSince(all, version)
	if len(pending) == 0 {
		fmt.Fprintf(out, "kakei %s is up to date\n", version)
		return nil
	}
	latest := pending[0]
	for _, r := range pending {
		fmt.Fprintf(out, "## %s\n%s\n\n", r.TagName, strings.TrimSpace(r.Body))
	}

	if !yes {
		ok, err := core.Confirm(
			fmt.Sprintf("Upgrade kakei v%s → %s?", version, latest.TagName),
			"Replaces this binary and migrates the database.",
			"Yes, upgrade")
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	}

	name := fmt.Sprintf("kakei_%s_%s_%s.tar.gz",
		strings.TrimPrefix(latest.TagName, "v"), runtime.GOOS, runtime.GOARCH)
	var url string
	for _, a := range latest.Assets {
		if a.Name == name {
			url = a.BrowserDownloadURL
			break
		}
	}
	if url == "" {
		return fmt.Errorf("release %s has no build for %s/%s", latest.TagName, runtime.GOOS, runtime.GOARCH)
	}

	exe, err := selfExe()
	if err != nil {
		return err
	}
	// A symlinked install must replace the real file, not the link.
	target, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}

	dl, err := client.Get(url)
	if err != nil {
		return err
	}
	defer dl.Body.Close()
	if dl.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: %s", name, dl.Status)
	}

	// The temp file sits next to the target so the rename stays on one
	// filesystem, and rename-over is the only safe way to replace a running
	// binary (writing into it fails with ETXTBSY).
	tmp, err := os.CreateTemp(filepath.Dir(target), ".kakei.tmp*")
	if err != nil {
		return permHint(err)
	}
	tmp.Close()
	if err := untarBinary(dl.Body, tmp.Name()); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		os.Remove(tmp.Name())
		return permHint(err)
	}
	fmt.Fprintf(out, "upgraded kakei v%s → %s\n", version, latest.TagName)

	cmd := exec.Command(target, "migrate")
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// The swap already succeeded, and db.Open migrates on every run, so
		// a failed exec here only defers the schema update — never lose the
		// upgrade over it.
		fmt.Fprintf(out, "could not run migrations now (%v) — they will apply on the next kakei run\n", err)
	}
	return nil
}

// runMigrate opens the database, which is what applies pending migrations —
// upgrade execs it on the new binary so the fresh schema lands immediately.
func runMigrate() error {
	return withConn(func(*sql.DB) error {
		fmt.Fprintln(out, "database schema is up to date")
		return nil
	})
}

func permHint(err error) error {
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("%w — try again with sudo if kakei lives in a root-owned directory", err)
	}
	return err
}

// releasesSince returns the published releases newer than current, newest first.
func releasesSince(all []release, current string) []release {
	var pending []release
	for _, r := range all {
		if r.Draft || r.Prerelease || !versionLess(current, r.TagName) {
			continue
		}
		pending = append(pending, r)
	}
	sort.Slice(pending, func(i, j int) bool { return versionLess(pending[j].TagName, pending[i].TagName) })
	return pending
}

// versionLess reports whether semver a sorts before b; a leading "v" is fine.
func versionLess(a, b string) bool {
	pa, pb := versionParts(a), versionParts(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func versionParts(v string) [3]int {
	var p [3]int
	for i, s := range strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3) {
		p[i], _ = strconv.Atoi(s)
	}
	return p
}

// untarBinary pulls the "kakei" entry out of a gzipped tarball into dst,
// executable. dst may already exist (a reserved temp file); it is truncated.
func untarBinary(r io.Reader, dst string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return errors.New("no kakei binary in the release tarball")
		}
		if err != nil {
			return err
		}
		if hdr.Name != "kakei" {
			continue
		}
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		return os.Chmod(dst, 0o755)
	}
}
