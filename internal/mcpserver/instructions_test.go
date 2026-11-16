package mcpserver

import (
	"strings"
	"testing"
)

// Beta testers rely on the agent filing reports on every platform.
func TestInstructionsAskForReports(t *testing.T) {
	for name, text := range map[string]string{"linux": instructions, "off linux": instructionsOffLinux} {
		if !strings.Contains(text, "call report_issue") {
			t.Errorf("%s instructions do not ask for report_issue", name)
		}
	}
}
