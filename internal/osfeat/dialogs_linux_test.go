//go:build linux

package osfeat

import (
	"testing"

	"github.com/hexadecimil/offstage/internal/config"
)

func TestDialogsEnv(t *testing.T) {
	t.Setenv("GDK_DEBUG", "")
	t.Setenv("QT_QPA_PLATFORMTHEME", "qt6ct")
	env := Detect(config.Default()).LaunchEnv("hc-x")
	if env["GTK_USE_PORTAL"] != "0" || env["GDK_DEBUG"] != "no-portals" {
		t.Errorf("gtk: %v", env)
	}
	if _, ok := env["QT_QPA_PLATFORMTHEME"]; ok {
		t.Errorf("a theme without a portal must stay: %v", env)
	}

	t.Setenv("GDK_DEBUG", "events")
	t.Setenv("QT_QPA_PLATFORMTHEME", "xdgdesktopportal")
	env = Detect(config.Default()).LaunchEnv("hc-x")
	if env["GDK_DEBUG"] != "events,no-portals" {
		t.Errorf("GDK_DEBUG: %q", env["GDK_DEBUG"])
	}
	if v, ok := env["QT_QPA_PLATFORMTHEME"]; !ok || v != "" {
		t.Errorf("portal theme not cleared: %v", env)
	}
}

func TestDialogsHost(t *testing.T) {
	cfg := config.Default()
	cfg.Dialogs = "host"
	if s := Detect(cfg); s.Dialogs != nil || len(s.LaunchEnv("hc-x")) != 0 {
		t.Errorf("dialogs = host: %+v", s)
	}
}
