package headless

import (
	"os"
	"path/filepath"
)

// installedChrome looks for Chrome in the standard install folders.
func installedChrome() string {
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
		if base := os.Getenv(env); base != "" {
			p := filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe")
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}
