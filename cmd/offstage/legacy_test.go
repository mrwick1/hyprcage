package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyEnvKeepsTheNewName(t *testing.T) {
	t.Setenv("HYPRCAGE_RENDERER", "pixman")
	t.Setenv("HYPRCAGE_DRIVER", "lua")
	t.Setenv("OFFSTAGE_DRIVER", "classic")
	legacyEnv()
	if got := os.Getenv("OFFSTAGE_RENDERER"); got != "pixman" {
		t.Errorf("OFFSTAGE_RENDERER = %q, want pixman", got)
	}
	if got := os.Getenv("OFFSTAGE_DRIVER"); got != "classic" {
		t.Errorf("OFFSTAGE_DRIVER = %q, want classic", got)
	}
	os.Unsetenv("OFFSTAGE_RENDERER")
}

func TestLegacyDirsMovesOnlyWhenNewIsMissing(t *testing.T) {
	root := t.TempDir()
	cfg, state, data := filepath.Join(root, "c"), filepath.Join(root, "s"), filepath.Join(root, "d")
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("XDG_DATA_HOME", data)
	write := func(p string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(p), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(cfg, "hyprcage", "config.toml"))
	write(filepath.Join(data, "hyprcage", "profiles", "p"))
	write(filepath.Join(state, "hyprcage", "actions.jsonl"))
	write(filepath.Join(state, "offstage", "actions.jsonl"))
	legacyDirs()
	for _, p := range []string{
		filepath.Join(cfg, "offstage", "config.toml"),
		filepath.Join(data, "offstage", "profiles", "p"),
		filepath.Join(state, "hyprcage", "actions.jsonl"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(cfg, "hyprcage")); err == nil {
		t.Error("old config dir still exists after the move")
	}
}
