//go:build linux

package system

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const unitName = "octopus-autopay-recurring"

// IsScreenAvailable consults loginctl. If neither LockedHint nor Active can be
// read (no systemd, headless, etc.), assumes the screen is available — the
// gate is a usability nicety, not a security boundary.
func IsScreenAvailable() (bool, string) {
	user := os.Getenv("USER")
	if user == "" {
		return true, ""
	}
	out, err := exec.Command("loginctl", "show-user", user, "-p", "Display", "--value").Output()
	if err != nil {
		return true, ""
	}
	sessionID := strings.TrimSpace(string(out))
	if sessionID == "" {
		return true, ""
	}
	out, err = exec.Command("loginctl", "show-session", sessionID, "-p", "LockedHint", "-p", "Active").Output()
	if err != nil {
		return true, ""
	}
	props := parseEqualsKV(string(out))
	if props["LockedHint"] == "yes" {
		return false, "sessione bloccata"
	}
	if props["Active"] == "no" {
		return false, "sessione non attiva"
	}
	return true, ""
}

func parseEqualsKV(s string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		i := strings.Index(line, "=")
		if i > 0 {
			m[line[:i]] = strings.TrimSpace(line[i+1:])
		}
	}
	return m
}

// Notify shells out to notify-send. Silently no-ops if the command is missing.
func Notify(title, body string) {
	_ = exec.Command("notify-send", title, body).Run()
}

func systemdDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

// InstallSchedule writes a systemd-user .service + .timer pair and enables the
// timer. The timer fires 1 minute after session start and every 6h thereafter,
// catching up missed runs after suspend (Persistent=true).
func InstallSchedule(execPath string) error {
	dir, err := systemdDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	servicePath := filepath.Join(dir, unitName+".service")
	timerPath := filepath.Join(dir, unitName+".timer")

	service := fmt.Sprintf(`[Unit]
Description=Octopus Energy autopay (recurring)

[Service]
Type=oneshot
ExecStart=%s
`, execPath)

	timer := `[Unit]
Description=Octopus Energy autopay schedule

[Timer]
OnStartupSec=1m
OnUnitActiveSec=6h
Persistent=true

[Install]
WantedBy=timers.target
`

	if err := os.WriteFile(servicePath, []byte(service), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(timerPath, []byte(timer), 0o644); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload: %w", err)
	}
	if out, err := exec.Command("systemctl", "--user", "enable", "--now", unitName+".timer").CombinedOutput(); err != nil {
		return fmt.Errorf("enable timer: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// UninstallSchedule disables the timer and removes both unit files. Idempotent.
func UninstallSchedule() error {
	dir, err := systemdDir()
	if err != nil {
		return err
	}
	_ = exec.Command("systemctl", "--user", "disable", "--now", unitName+".timer").Run()
	for _, n := range []string{unitName + ".service", unitName + ".timer"} {
		_ = os.Remove(filepath.Join(dir, n))
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}
