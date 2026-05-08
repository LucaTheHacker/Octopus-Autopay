//go:build windows

package system

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

const taskName = "OctopusAutopayRecurring"

// IsScreenAvailable detects the lock screen by checking whether LogonUI.exe is
// running. If LogonUI is up the user is at the lock screen; otherwise the
// session is unlocked. Display power state is not checked — Windows wakes the
// display on user input long before any payment UI matters.
func IsScreenAvailable() (bool, string) {
	out, err := exec.Command("tasklist", "/fi", "IMAGENAME eq LogonUI.exe", "/nh").Output()
	if err == nil && strings.Contains(strings.ToLower(string(out)), "logonui.exe") {
		return false, "schermo bloccato"
	}
	return true, ""
}

// Notify uses msg.exe to push a session-bound message. Best-effort; some
// Windows editions strip msg.exe (Home), in which case the call silently fails.
func Notify(title, body string) {
	_ = exec.Command("msg", "*", "/TIME:5", fmt.Sprintf("%s: %s", title, body)).Run()
}

// InstallSchedule registers a Task Scheduler task with two triggers: at user
// logon and every 6 hours while logged in. Existing task with the same name is
// replaced.
func InstallSchedule(execPath string) error {
	user := os.Getenv("USERDOMAIN") + "\\" + os.Getenv("USERNAME")
	if strings.HasPrefix(user, "\\") {
		user = os.Getenv("USERNAME")
	}
	xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>%s</UserId>
    </LogonTrigger>
    <CalendarTrigger>
      <StartBoundary>2024-01-01T09:00:00</StartBoundary>
      <Repetition>
        <Interval>PT6H</Interval>
        <StopAtDurationEnd>false</StopAtDurationEnd>
      </Repetition>
      <ScheduleByDay><DaysInterval>1</DaysInterval></ScheduleByDay>
    </CalendarTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>%s</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <Enabled>true</Enabled>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
  </Settings>
  <Actions Context="Author">
    <Exec><Command>%s</Command></Exec>
  </Actions>
</Task>
`, escapeXML(user), escapeXML(user), escapeXML(execPath))

	tmp := filepath.Join(os.TempDir(), taskName+".xml")
	if err := os.WriteFile(tmp, encodeUTF16LEWithBOM(xml), 0o644); err != nil {
		return err
	}
	defer os.Remove(tmp)

	_ = exec.Command("schtasks", "/delete", "/tn", taskName, "/f").Run()
	if out, err := exec.Command("schtasks", "/create", "/tn", taskName, "/xml", tmp).CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /create: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// UninstallSchedule deletes the Task Scheduler task. Idempotent.
func UninstallSchedule() error {
	_ = exec.Command("schtasks", "/delete", "/tn", taskName, "/f").Run()
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

// encodeUTF16LEWithBOM is what schtasks expects for /xml input.
func encodeUTF16LEWithBOM(s string) []byte {
	enc := utf16.Encode([]rune(s))
	out := make([]byte, 2+2*len(enc))
	out[0] = 0xff
	out[1] = 0xfe
	for i, r := range enc {
		binary.LittleEndian.PutUint16(out[2+2*i:], r)
	}
	return out
}
