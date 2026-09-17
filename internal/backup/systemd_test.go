package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnits(t *testing.T) {
	t.Setenv("PECUNIA_DB", "")
	t.Setenv("PECUNIA_NOTES", "")
	e, _ := ParseEvery("2/day")
	service, timer := Units("/usr/local/bin/pecunia", e)

	for _, want := range []string{
		"[Service]\n", "Type=oneshot\n", "ExecStart=/usr/local/bin/pecunia backup run\n",
	} {
		if !strings.Contains(service, want) {
			t.Errorf("service lacks %q:\n%s", want, service)
		}
	}
	if strings.Contains(service, "Environment=") {
		t.Errorf("service sets an environment nobody set:\n%s", service)
	}
	for _, want := range []string{
		"[Timer]\n", "OnCalendar=*-*-* 03,15:00:00\n", "Persistent=true\n", "WantedBy=timers.target\n",
	} {
		if !strings.Contains(timer, want) {
			t.Errorf("timer lacks %q:\n%s", want, timer)
		}
	}

	t.Run("carries PECUNIA_DB and PECUNIA_NOTES when set", func(t *testing.T) {
		t.Setenv("PECUNIA_DB", "/data/p.db")
		t.Setenv("PECUNIA_NOTES", "/data/notes")
		service, _ := Units("/bin/pecunia", e)
		if !strings.Contains(service, "Environment=PECUNIA_DB=/data/p.db\n") || !strings.Contains(service, "Environment=PECUNIA_NOTES=/data/notes\n") {
			t.Errorf("service:\n%s", service)
		}
	})
}

func TestInstallTimer(t *testing.T) {
	stub := func(t *testing.T) *[][]string {
		t.Helper()
		var calls [][]string
		old := Systemctl
		Systemctl = func(args ...string) error {
			calls = append(calls, args)
			return nil
		}
		t.Cleanup(func() { Systemctl = old })
		return &calls
	}

	t.Run("writes both units and enables the timer", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", home)
		t.Setenv("PECUNIA_DB", "")
		t.Setenv("PECUNIA_NOTES", "")
		calls := stub(t)
		e, _ := ParseEvery("1/week")
		if err := InstallTimer("/bin/pecunia", e); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(home, "systemd", "user")
		timer, err := os.ReadFile(filepath.Join(dir, "pecunia-backup.timer"))
		if err != nil || !strings.Contains(string(timer), "OnCalendar=Mon *-*-* 03:00:00") {
			t.Fatalf("timer %q, %v", timer, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "pecunia-backup.service")); err != nil {
			t.Fatal(err)
		}
		want := "daemon-reload | enable --now pecunia-backup.timer"
		var got []string
		for _, c := range *calls {
			got = append(got, strings.Join(c, " "))
		}
		if strings.Join(got, " | ") != want {
			t.Fatalf("Systemctl calls %q, want %q", strings.Join(got, " | "), want)
		}
	})

	t.Run("uninstall disables and removes", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", home)
		calls := stub(t)
		e, _ := ParseEvery("1/day")
		InstallTimer("/bin/pecunia", e)
		*calls = nil
		if err := UninstallTimer(); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(home, "systemd", "user")
		for _, f := range []string{"pecunia-backup.timer", "pecunia-backup.service"} {
			if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
				t.Errorf("%s still there", f)
			}
		}
		got := strings.Join((*calls)[0], " ")
		if got != "disable --now pecunia-backup.timer" {
			t.Fatalf("first call %q", got)
		}
	})

	t.Run("uninstall with nothing installed is fine", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		stub(t)
		if err := UninstallTimer(); err != nil {
			t.Fatal(err)
		}
	})
}
