package backup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const unitName = "pecunia-backup"

// Systemctl runs "systemctl --user" with the arguments; swapped in tests.
var Systemctl = func(args ...string) error {
	cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl --user %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// Units render the service and the timer. The service carries PECUNIA_DB and
// PECUNIA_NOTES when the installing shell had them, since the timer's
// environment is not the shell's; it runs the very binary that installed it.
func Units(exe string, e Every) (service, timer string) {
	var env strings.Builder
	for _, k := range []string{"PECUNIA_DB", "PECUNIA_NOTES"} {
		if v := os.Getenv(k); v != "" {
			fmt.Fprintf(&env, "Environment=%s=%s\n", k, v)
		}
	}
	service = fmt.Sprintf(`[Unit]
Description=pecunia backup

[Service]
Type=oneshot
%sExecStart=%s backup run
`, env.String(), exe)
	timer = fmt.Sprintf(`[Unit]
Description=pecunia backup, %s

[Timer]
OnCalendar=%s
Persistent=true

[Install]
WantedBy=timers.target
`, e, e.OnCalendar())
	return service, timer
}

// unitDir is where a user's own units go: ~/.config/systemd/user.
func unitDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "systemd", "user"), nil
}

// InstallTimer writes the units and starts the timer.
func InstallTimer(exe string, e Every) error {
	dir, err := unitDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	service, timer := Units(exe, e)
	if err := os.WriteFile(filepath.Join(dir, unitName+".service"), []byte(service), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, unitName+".timer"), []byte(timer), 0o644); err != nil {
		return err
	}
	if err := Systemctl("daemon-reload"); err != nil {
		return err
	}
	return Systemctl("enable", "--now", unitName+".timer")
}

// UninstallTimer stops the timer and removes the units. Nothing installed is
// nothing to do.
func UninstallTimer() error {
	dir, err := unitDir()
	if err != nil {
		return err
	}
	timerPath := filepath.Join(dir, unitName+".timer")
	if _, err := os.Stat(timerPath); os.IsNotExist(err) {
		return nil
	}
	if err := Systemctl("disable", "--now", unitName+".timer"); err != nil {
		return err
	}
	for _, f := range []string{timerPath, filepath.Join(dir, unitName+".service")} {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return Systemctl("daemon-reload")
}

// TimerInstalled says whether the timer unit is on disk.
func TimerInstalled() (bool, error) {
	dir, err := unitDir()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(dir, unitName+".timer"))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// HaveSystemd says whether there is a systemctl to install a timer with;
// swapped in tests.
var HaveSystemd = func() bool {
	_, err := exec.LookPath("systemctl")
	return err == nil
}
