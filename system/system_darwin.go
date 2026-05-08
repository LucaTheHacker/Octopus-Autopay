//go:build darwin

package system

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const launchdLabel = "com.octopus.autopay.recurring"

// IsScreenAvailable reports whether the macOS console session is unlocked AND
// the display is on. A false return is paired with a short Italian reason.
func IsScreenAvailable() (bool, string) {
	if locked, _ := isScreenLocked(); locked {
		return false, "schermo bloccato"
	}
	if !displayOn() {
		return false, "display spento"
	}
	return true, ""
}

func isScreenLocked() (bool, error) {
	out, err := exec.Command("ioreg", "-n", "Root", "-d1", "-a").Output()
	if err != nil {
		return false, err
	}
	// The XML plist output has "<key>CGSSessionScreenIsLocked</key>" followed
	// by either <true/> or <false/>. We look for the key + true on subsequent
	// non-key lines.
	s := string(out)
	idx := strings.Index(s, "CGSSessionScreenIsLocked")
	if idx < 0 {
		return false, nil
	}
	tail := s[idx:]
	if i := strings.Index(tail, "<true/>"); i > 0 && i < 200 {
		return true, nil
	}
	return false, nil
}

func displayOn() bool {
	out, err := exec.Command("pmset", "-g", "powerstate", "IODisplayWrangler").Output()
	if err != nil {
		return true // best-effort; assume on
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "IODisplayWrangler") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			if st, err := strconv.Atoi(fields[2]); err == nil {
				return st == 4
			}
		}
	}
	return true
}

// Notify shows a Notification Center toast via osascript. Best-effort.
func Notify(title, body string) {
	clean := func(s string) string {
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		return s
	}
	script := fmt.Sprintf(`display notification "%s" with title "%s"`, clean(body), clean(title))
	_ = exec.Command("osascript", "-e", script).Run()
}

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"), nil
}

// InstallSchedule writes a per-user LaunchAgent that runs execPath at every
// login (RunAtLoad) and every 6 hours while logged in (StartInterval=21600).
// StartInterval catches up missed slots after wake/resume.
func InstallSchedule(execPath string) error {
	p, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>StartInterval</key><integer>21600</integer>
  <key>ProcessType</key><string>Background</string>
</dict>
</plist>
`, launchdLabel, escapeXML(execPath))
	if err := os.WriteFile(p, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("write plist %s: %w", p, err)
	}
	target := "gui/" + strconv.Itoa(os.Getuid())
	_ = exec.Command("launchctl", "bootout", target+"/"+launchdLabel).Run()
	if out, err := exec.Command("launchctl", "bootstrap", target, p).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// UninstallSchedule unloads the LaunchAgent and removes the plist. Idempotent.
func UninstallSchedule() error {
	p, err := plistPath()
	if err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+launchdLabel).Run()
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist %s: %w", p, err)
	}
	return nil
}

func escapeXML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		`'`, "&apos;",
	)
	return r.Replace(s)
}
