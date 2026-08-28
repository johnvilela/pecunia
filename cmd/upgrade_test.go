package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runUpgradeIn(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var buf bytes.Buffer
	old := out
	out = &buf
	t.Cleanup(func() { out = old })

	err := runUpgrade(args)
	return buf.String(), err
}

func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.1.1", true},
		{"0.1.9", "0.2.0", true},
		{"0.9.0", "1.0.0", true},
		{"0.10.0", "0.9.0", false},
		{"v0.1.0", "v0.2.0", true},
		{"0.2.0", "v0.1.0", false}, // dev build ahead of the latest release
	}
	for _, c := range cases {
		if got := versionLess(c.a, c.b); got != c.want {
			t.Errorf("versionLess(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestReleasesSince(t *testing.T) {
	all := []release{
		{TagName: "v0.4.0", Draft: true},
		{TagName: "v0.3.0-rc1", Prerelease: true},
		{TagName: "v0.2.0"},
		{TagName: "v0.3.0"},
		{TagName: "v0.1.0"},
	}
	got := releasesSince(all, "0.1.0")
	if len(got) != 2 || got[0].TagName != "v0.3.0" || got[1].TagName != "v0.2.0" {
		t.Fatalf("releasesSince = %+v, want v0.3.0 then v0.2.0", got)
	}
	if len(releasesSince(all, "0.3.0")) != 0 {
		t.Error("up to date should leave nothing pending")
	}
}

// tarball builds a gzipped tar with a single "pecunia" entry.
func tarball(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "pecunia", Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUntarBinary(t *testing.T) {
	t.Run("extracts the pecunia entry with exec permissions", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "pecunia.new")
		if err := untarBinary(bytes.NewReader(tarball(t, []byte("binary"))), dst); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "binary" {
			t.Errorf("content = %q", got)
		}
		info, err := os.Stat(dst)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Errorf("mode = %v, want 0755", info.Mode().Perm())
		}
	})

	t.Run("a tarball without the binary is an error", func(t *testing.T) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		if err := tar.NewWriter(gz).Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(t.TempDir(), "pecunia.new")
		if err := untarBinary(bytes.NewReader(buf.Bytes()), dst); err == nil {
			t.Error("want error for a tarball with no pecunia entry")
		}
	})
}

// upgradeServer serves the releases JSON and one tarball asset.
func upgradeServer(t *testing.T, releases []release, asset []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(releases)
	})
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		w.Write(asset)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestUpgrade(t *testing.T) {
	t.Run("already up to date", func(t *testing.T) {
		srv := upgradeServer(t, []release{{TagName: "v" + version}}, nil)
		old := releasesURL
		releasesURL = srv.URL + "/releases"
		t.Cleanup(func() { releasesURL = old })

		got, err := runUpgradeIn(t, "-y")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "up to date") {
			t.Errorf("output = %q, want up to date", got)
		}
	})

	t.Run("upgrades, shows every skipped changelog and migrates", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "pecunia")
		if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(dir, "migrated")
		script := fmt.Sprintf("#!/bin/sh\n[ \"$1\" = migrate ] && echo done > %s\n", marker)

		releases := []release{
			{TagName: "v0.9.9", Body: "newest notes"},
			{TagName: "v0.9.8", Body: "older notes"},
		}
		srv := upgradeServer(t, releases, tarball(t, []byte(script)))
		assetName := fmt.Sprintf("pecunia_0.9.9_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
		releases[0].Assets = []asset{{Name: assetName, BrowserDownloadURL: srv.URL + "/asset"}}

		oldURL, oldExe := releasesURL, selfExe
		releasesURL = srv.URL + "/releases"
		selfExe = func() (string, error) { return target, nil }
		t.Cleanup(func() { releasesURL, selfExe = oldURL, oldExe })

		got, err := runUpgradeIn(t, "-y")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "newest notes") || !strings.Contains(got, "older notes") {
			t.Errorf("changelog missing pending release notes: %q", got)
		}
		swapped, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if string(swapped) != script {
			t.Error("target binary was not replaced")
		}
		if _, err := os.Stat(marker); err != nil {
			t.Error("new binary's migrate was not run")
		}
		leftovers, _ := filepath.Glob(filepath.Join(dir, "*.tmp*"))
		if len(leftovers) != 0 {
			t.Errorf("temp files left behind: %v", leftovers)
		}
	})

	t.Run("a failed migrate exec defers, never fails the finished upgrade", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "pecunia")
		if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}

		releases := []release{{TagName: "v0.9.9", Body: "notes"}}
		srv := upgradeServer(t, releases, tarball(t, []byte("#!/bin/sh\nexit 1\n")))
		assetName := fmt.Sprintf("pecunia_0.9.9_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
		releases[0].Assets = []asset{{Name: assetName, BrowserDownloadURL: srv.URL + "/asset"}}

		oldURL, oldExe := releasesURL, selfExe
		releasesURL = srv.URL + "/releases"
		selfExe = func() (string, error) { return target, nil }
		t.Cleanup(func() { releasesURL, selfExe = oldURL, oldExe })

		got, err := runUpgradeIn(t, "-y")
		if err != nil {
			t.Fatalf("upgrade must survive a migrate failure, got %v", err)
		}
		if !strings.Contains(got, "next pecunia run") {
			t.Errorf("output = %q, want a deferred-migration note", got)
		}
	})

	t.Run("missing asset for this platform is a clear error", func(t *testing.T) {
		srv := upgradeServer(t, []release{{TagName: "v9.9.9", Body: "notes"}}, nil)
		old := releasesURL
		releasesURL = srv.URL + "/releases"
		t.Cleanup(func() { releasesURL = old })

		_, err := runUpgradeIn(t, "-y")
		if err == nil || !strings.Contains(err.Error(), runtime.GOOS) {
			t.Errorf("err = %v, want asset-not-found naming the platform", err)
		}
	})
}

func TestMigrate(t *testing.T) {
	t.Setenv("PECUNIA_DB", filepath.Join(t.TempDir(), "pecunia.db"))

	var buf bytes.Buffer
	old := out
	out = &buf
	t.Cleanup(func() { out = old })

	if err := runMigrate(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "up to date") {
		t.Errorf("output = %q", buf.String())
	}
}
