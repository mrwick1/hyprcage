// Package notify posts low-urgency desktop notifications, best effort
// (cahier N8): plain notify-send, no desktop-shell integration.
package notify

import "os/exec"

// Send posts a notification; silently does nothing without notify-send.
func Send(summary, body string) {
	if _, err := exec.LookPath("notify-send"); err != nil {
		return
	}
	_ = exec.Command("notify-send", "-u", "low", "-a", "offstage", summary, body).Run()
}
