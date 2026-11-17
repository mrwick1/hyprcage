package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hexadecimil/offstage/internal/agentrule"
)

func TestSessionStartKeepsTheAgentRule(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "run"))
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(root, ".claude", "CLAUDE.md")
	start := func() string {
		var out bytes.Buffer
		if code := Run([]string{"session-start"}, strings.NewReader(`{"session_id":"s1","source":"startup"}`), &out, &out); code != ExitOK {
			t.Fatalf("exit %d: %s", code, out.String())
		}
		return out.String()
	}
	if out := start(); out != "" {
		t.Errorf("the hook printed %q; its output goes into the agent's context", out)
	}
	if ok, _ := agentrule.Present(md); !ok {
		t.Fatal("rule not added")
	}
	cfg := filepath.Join(root, "config", "offstage", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("[report]\nagent_rule = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	start()
	if ok, _ := agentrule.Present(md); ok {
		t.Error("rule kept with agent_rule = false")
	}
}
