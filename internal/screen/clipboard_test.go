package screen

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/hexadecimil/hyprcage/internal/registry"
)

func TestInnerEnv(t *testing.T) {
	rec := &registry.Screen{Name: "hc-a", InnerDisplay: "wayland-7"}
	base := []string{"WAYLAND_DISPLAY=wayland-1", "WAYLAND_SOCKET=5", "HYPRLAND_INSTANCE_SIGNATURE=x", "HOME=/h"}
	got := innerEnv(rec, base)
	for _, want := range []string{"WAYLAND_DISPLAY=wayland-7", "HYPRCAGE_SCREEN=hc-a", "HOME=/h"} {
		if !slices.Contains(got, want) {
			t.Errorf("env lacks %s: %v", want, got)
		}
	}
	for _, bad := range []string{"WAYLAND_DISPLAY=wayland-1", "WAYLAND_SOCKET=5", "HYPRLAND_INSTANCE_SIGNATURE=x"} {
		if slices.Contains(got, bad) {
			t.Errorf("env keeps %s", bad)
		}
	}
}

func TestWithInnerEmptyDisplayIsDead(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	rec := &registry.Screen{Name: "hc-a"}
	if err := os.MkdirAll(filepath.Dir(registry.InnerPath(rec.Name)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registry.InnerPath(rec.Name), []byte("OTHER=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var se *Error
	if err := withInner(rec); !errors.As(err, &se) || se.Code != CodeDead {
		t.Fatalf("want screen_dead, got %v", err)
	}
}
