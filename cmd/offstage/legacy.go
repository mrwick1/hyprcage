package main

import (
	"os"
	"path/filepath"
	"strings"
)

// The project was called hyprcage before 0.10. These two steps let an
// existing install keep its settings, logs and browser logins.

// legacyEnv copies each HYPRCAGE_* variable to its OFFSTAGE_* name when the
// new name is not set.
func legacyEnv() {
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if rest, ok := strings.CutPrefix(k, "HYPRCAGE_"); ok {
			if _, set := os.LookupEnv("OFFSTAGE_" + rest); !set {
				os.Setenv("OFFSTAGE_"+rest, v)
			}
		}
	}
}

// legacyDirs moves the old config, state and data directories to the new
// name. A directory moves only when the new one does not exist yet.
func legacyDirs() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	for _, d := range []struct{ env, def string }{
		{"XDG_CONFIG_HOME", ".config"},
		{"XDG_STATE_HOME", ".local/state"},
		{"XDG_DATA_HOME", ".local/share"},
	} {
		base := os.Getenv(d.env)
		if base == "" {
			base = filepath.Join(home, d.def)
		}
		oldDir, newDir := filepath.Join(base, "hyprcage"), filepath.Join(base, "offstage")
		if _, err := os.Stat(newDir); err == nil {
			continue
		}
		if fi, err := os.Stat(oldDir); err == nil && fi.IsDir() {
			_ = os.Rename(oldDir, newDir)
		}
	}
}
