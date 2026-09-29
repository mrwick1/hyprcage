package hypr

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestWaylandDisplayFromLockFile(t *testing.T) {
	rt := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", rt)
	t.Setenv("WAYLAND_DISPLAY", "wayland-env")
	l, err := net.Listen("unix", filepath.Join(rt, "wayland-9"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	inst := &Instance{Signature: "sig", Dir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(inst.Dir, "hyprland.lock"), []byte("4945\nwayland-9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if d, err := inst.WaylandDisplay(); err != nil || d != "wayland-9" {
		t.Fatalf("got %q %v, want wayland-9", d, err)
	}
}

func TestWaylandDisplayWithoutLockFileUsesEnv(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("WAYLAND_DISPLAY", "wayland-env")
	inst := &Instance{Signature: "sig", Dir: t.TempDir()}
	if d, err := inst.WaylandDisplay(); err != nil || d != "wayland-env" {
		t.Fatalf("got %q %v, want wayland-env", d, err)
	}
}
