package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
)

// chromeProcs lists the processes whose command line holds profile.
func chromeProcs(profile string) []string {
	var found []string
	dirs, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	for _, f := range dirs {
		if b, err := os.ReadFile(f); err == nil && strings.Contains(string(b), profile) {
			found = append(found, filepath.Base(filepath.Dir(f)))
		}
	}
	return found
}
