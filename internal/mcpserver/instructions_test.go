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
		for _, w := range []string{"doubt", "suggestion"} {
			if !strings.Contains(text, w) {
				t.Errorf("%s instructions do not mention %s", name, w)
			}
		}
	}
}

// Bare's own initialize text does not reach the agent, so offstage teaches the loop.
func TestInstructionsTeachTheBareLoop(t *testing.T) {
	for name, text := range map[string]string{"linux": instructions, "off linux": instructionsOffLinux} {
		for _, w := range []string{"Refs end at navigation", "never the click receipt alone", "before you retry a side effect", "report_issue, area bare"} {
			if !strings.Contains(text, w) {
				t.Errorf("%s instructions do not say %q", name, w)
			}
		}
	}
}
