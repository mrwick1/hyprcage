//go:build linux

package osfeat

import "os"

// dialogs keeps GTK and Qt file dialogs in the screen. Without it, GTK 4 and
// apps with the Qt portal theme ask xdg-desktop-portal, which opens the
// dialog on the human's desktop. Electron apps still use the portal: no
// environment variable stops them.
type dialogs struct{}

func (dialogs) Env() map[string]string {
	env := map[string]string{
		"GTK_USE_PORTAL": "0",          // GTK 3
		"GDK_DEBUG":      "no-portals", // GTK 4
	}
	if v := os.Getenv("GDK_DEBUG"); v != "" {
		env["GDK_DEBUG"] = v + ",no-portals"
	}
	if os.Getenv("QT_QPA_PLATFORMTHEME") == "xdgdesktopportal" {
		env["QT_QPA_PLATFORMTHEME"] = "" // empty: Qt picks no platform theme
	}
	return env
}
