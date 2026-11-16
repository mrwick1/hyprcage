package mcpserver

import (
	"os/exec"
	"strings"
)

// chromeProcs lists the chrome.exe processes whose command line holds profile.
func chromeProcs(profile string) []string {
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"Get-CimInstance Win32_Process -Filter \"Name='chrome.exe'\" | ForEach-Object { \"$($_.ProcessId)`t$($_.CommandLine)\" }").Output()
	if err != nil {
		panic("chromeProcs: " + err.Error()) // an empty list would pass the check for no leftovers
	}
	var found []string
	for _, line := range strings.Split(string(out), "\n") {
		if pid, cmd, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok && strings.Contains(cmd, profile) {
			found = append(found, pid)
		}
	}
	return found
}
