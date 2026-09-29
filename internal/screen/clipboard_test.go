package screen

import (
	"slices"
	"testing"

	"github.com/hexadecimil/hyprcage/internal/registry"
)

func TestInnerEnv(t *testing.T) {
	rec := &registry.Screen{Name: "hc-a", InnerDisplay: "wayland-7"}
	base := []string{"WAYLAND_DISPLAY=wayland-1", "HYPRLAND_INSTANCE_SIGNATURE=x", "HOME=/h"}
	got := innerEnv(rec, base)
	for _, want := range []string{"WAYLAND_DISPLAY=wayland-7", "HYPRCAGE_SCREEN=hc-a", "HOME=/h"} {
		if !slices.Contains(got, want) {
			t.Errorf("env lacks %s: %v", want, got)
		}
	}
	for _, bad := range []string{"WAYLAND_DISPLAY=wayland-1", "HYPRLAND_INSTANCE_SIGNATURE=x"} {
		if slices.Contains(got, bad) {
			t.Errorf("env keeps %s", bad)
		}
	}
}
