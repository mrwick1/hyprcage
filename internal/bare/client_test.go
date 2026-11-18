package bare

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnvironment(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "human")
	t.Setenv("DISPLAY", ":0")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "human")
	t.Setenv("GDK_SCALE", "2")
	t.Setenv("BARE_MCP_REPORT", "offstage")
	for _, display := range []string{"", "cage-socket"} {
		env := strings.Join(Environment(display), "\n") + "\n"
		if !strings.Contains(env, "WAYLAND_DISPLAY="+display+"\n") || strings.Contains(env, "human") || strings.Contains(env, "DISPLAY=:0") || strings.Contains(env, "GDK_SCALE=") || strings.Contains(env, "BARE_MCP_REPORT") {
			t.Fatal(env)
		}
	}
}

func TestFindRequiresBothExecutables(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	if _, err := Find(); err == nil {
		t.Fatal("found missing browser")
	}
	for _, name := range []string{"bare-mcp", "bare-drive"} {
		if err := os.WriteFile(filepath.Join(dir, name+suffix), nil, 0755); err != nil {
			t.Fatal(err)
		}
		if name == "bare-mcp" {
			if _, err := Find(); err == nil {
				t.Fatal("found browser without driver")
			}
		}
	}
	if _, err := Find(); err != nil {
		t.Fatal(err)
	}
}
