package agentrule

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallCreatesAppendsReplacesAndRemoves(t *testing.T) {
	p := filepath.Join(t.TempDir(), "CLAUDE.md")
	if st, err := Install(p); err != nil || st != Added {
		t.Fatalf("create: %v %v", st, err)
	}
	if data, _ := os.ReadFile(p); string(data) != block {
		t.Fatalf("new file:\n%s", data)
	}

	own := "# Mine\n\nKeep this.\n"
	if err := os.WriteFile(p, []byte(own), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o640); err != nil {
		t.Fatal(err)
	}
	if st, err := Install(p); err != nil || st != Added {
		t.Fatalf("append: %v %v", st, err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != own+"\n"+block {
		t.Fatalf("appended:\n%s", data)
	}
	if st, _ := os.Stat(p); runtime.GOOS != "windows" && st.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	if st, err := Install(p); err != nil || st != Unchanged {
		t.Fatalf("again: %v %v", st, err)
	}

	old := own + "\n" + startPrefix + "0 -->\nold rule\n" + endMarker + "\n\nAfter.\n"
	if err := os.WriteFile(p, []byte(old), 0o640); err != nil {
		t.Fatal(err)
	}
	if st, err := Install(p); err != nil || st != Updated {
		t.Fatalf("update: %v %v", st, err)
	}
	data, _ = os.ReadFile(p)
	if string(data) != own+"\n"+block+"\nAfter.\n" {
		t.Fatalf("updated:\n%s", data)
	}

	if st, err := Remove(p); err != nil || st != Removed {
		t.Fatalf("remove: %v %v", st, err)
	}
	data, _ = os.ReadFile(p)
	if string(data) != own+"\nAfter.\n" {
		t.Fatalf("removed:\n%q", data)
	}
	if st, err := Remove(p); err != nil || st != Unchanged {
		t.Fatalf("remove again: %v %v", st, err)
	}
}

func TestSymlinksAreSkipped(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(target, []byte("shared\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "CLAUDE.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	for name, f := range map[string]func(string) (Status, error){"install": Install, "remove": Remove} {
		if st, err := f(link); err != nil || st != Symlink {
			t.Errorf("%s: %v %v", name, st, err)
		}
	}
	if data, _ := os.ReadFile(target); string(data) != "shared\n" {
		t.Errorf("target changed: %s", data)
	}
	if ok, st := Present(link); ok || st != Symlink {
		t.Errorf("present before: %v %v", ok, st)
	}
	_ = os.WriteFile(target, []byte("shared\n\n"+block), 0o644) // added by hand
	if ok, st := Present(link); !ok || st != Symlink {
		t.Errorf("present after a hand edit: %v %v", ok, st)
	}
}

func TestBlockAsksForBackgroundReports(t *testing.T) {
	for _, want := range []string{"report_issue", "Do not ask the human", "doubt", "suggestion", "Never report the app under test"} {
		if !strings.Contains(block, want) {
			t.Errorf("block lacks %q", want)
		}
	}
}

func TestPathsFollowTheAgentDirectories(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // the home directory on Windows
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	if got := Paths(); len(got) != 0 {
		t.Errorf("no agent directory: %v", got)
	}
	_ = os.Mkdir(filepath.Join(home, ".claude"), 0o700)
	codex := filepath.Join(home, "cx")
	_ = os.Mkdir(codex, 0o700)
	t.Setenv("CODEX_HOME", codex)
	got := Paths()
	if len(got) != 2 || got[0] != filepath.Join(home, ".claude", "CLAUDE.md") || got[1] != filepath.Join(codex, "AGENTS.md") {
		t.Errorf("paths %v", got)
	}
}

func TestPresent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "CLAUDE.md")
	if ok, _ := Present(p); ok {
		t.Error("missing file reports the rule")
	}
	_, _ = Install(p)
	if ok, _ := Present(p); !ok {
		t.Error("installed rule not found")
	}
}
