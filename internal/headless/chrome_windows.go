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

// localState stops a blank-password test of the user's Windows account.
// Chrome's password manager runs LogonUser with an empty password unless
// Local State says the password has not changed since its last test
// (password_manager_util_win.cc). Each failed test counts toward the
// account lockout, so a fresh profile per screen can lock the user out.
const localState = `{"password_manager":{"os_password_blank":false,"os_password_last_changed":"9223372036854775807"}}`

// seedProfile writes Local State into a new profile before Chrome starts.
func seedProfile(dir string) error {
	p := filepath.Join(dir, "Local State")
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	return os.WriteFile(p, []byte(localState), 0o600)
}
