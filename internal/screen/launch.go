package screen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/sysd"
)

var launchSeq int

// Launch runs command inside the screen (cahier F2): cage's Wayland socket,
// Wayland-first toolkit hints, the human's Hyprland signature removed, in a
// scope of the screen's slice. Waiting for the window needs the wl package.
// It returns the pid and the path of the application's log file.
func Launch(c *Ctx, rec *registry.Screen, command []string, cwd string, extraEnv map[string]string) (int, string, error) {
	if len(command) == 0 {
		return 0, "", fmt.Errorf("empty command")
	}
	if rec.InnerDisplay == "" {
		inner, err := registry.ReadInner(rec.Name)
		if err != nil {
			return 0, "", errf(CodeDead, "", "screen %s has no inner socket", rec.Name)
		}
		rec.InnerDisplay, rec.InnerX11 = inner["WAYLAND_DISPLAY"], inner["DISPLAY"]
	}
	env := map[string]string{
		"WAYLAND_DISPLAY":              rec.InnerDisplay,
		"XDG_SESSION_TYPE":             "wayland",
		"GDK_BACKEND":                  "wayland,x11",
		"QT_QPA_PLATFORM":              "wayland;xcb",
		"MOZ_ENABLE_WAYLAND":           "1",
		"ELECTRON_OZONE_PLATFORM_HINT": "auto",
		"SDL_VIDEODRIVER":              "wayland",
		"HYPRCAGE_SCREEN":              rec.Name,
		// Qt and GTK build their accessibility tree only when asked.
		"QT_LINUX_ACCESSIBILITY_ALWAYS_ON": "1",
		"ACCESSIBILITY_ENABLED":            "1",
		"GNOME_ACCESSIBILITY":              "1",
	}
	if rec.InnerX11 != "" {
		env["DISPLAY"] = rec.InnerX11
	}
	// Many Wayland toolkits misbehave without a UTF-8 locale; default to one
	// when the environment has none, overridable through env.
	if os.Getenv("LANG") == "" && os.Getenv("LC_ALL") == "" && os.Getenv("LC_CTYPE") == "" {
		env["LANG"] = "C.UTF-8"
	}
	for k, v := range extraEnv {
		env[k] = v
	}
	// HiDPI hints in the environment describe the human's outputs (Omarchy
	// sets GDK_SCALE=2 for instance); the caged application must follow the
	// scale of the cage output, which is 1. They can still be passed in env.
	drop := map[string]bool{
		"HYPRLAND_INSTANCE_SIGNATURE": true, "DISPLAY": rec.InnerX11 == "",
		"GDK_SCALE": true, "GDK_DPI_SCALE": true, "QT_SCALE_FACTOR": true,
	}
	var full []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if drop[k] {
			continue
		}
		if _, ok := env[k]; ok {
			continue
		}
		full = append(full, kv)
	}
	for k, v := range env {
		full = append(full, k+"="+v)
	}

	launchSeq++
	argv := command
	if sysd.Available() {
		argv = sysd.ScopeArgs(rec.Name, fmt.Sprintf("app-%d-%d", os.Getpid(), launchSeq), command)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = full
	cmd.Dir = cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if f := appLog(rec.Name, launchSeq); f != nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer f.Close()
	}
	if err := cmd.Start(); err != nil {
		return 0, "", err
	}
	go func() { _ = cmd.Wait() }() // reap; systemd-run --scope lives as long as the app
	return cmd.Process.Pid, AppLogPath(rec.Name, launchSeq), nil
}

// CageLogPath is where cage's own stdout and stderr go: the place to look
// when a screen does not come up (a render node that cannot be opened, a
// renderer that fails to initialise).
func CageLogPath(screen string) string {
	return filepath.Join(LogDir(), screen+"-cage.log")
}

// MirrorLog opens the mirror's log for one line, appended.
func MirrorLog(screen string) *os.File {
	if err := os.MkdirAll(LogDir(), 0o700); err != nil {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(LogDir(), screen+"-mirror.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil
	}
	return f
}

func cageLog(screen string) *os.File {
	if err := os.MkdirAll(LogDir(), 0o700); err != nil {
		return nil
	}
	f, err := os.OpenFile(CageLogPath(screen), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil
	}
	return f
}

// LogDir is where launched applications' stdout and stderr go (cahier N8).
func LogDir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(base, "hyprcage", "log")
}

// AppLogPath returns the log file of the n-th launch on a screen by this process.
func AppLogPath(screen string, n int) string {
	return filepath.Join(LogDir(), fmt.Sprintf("%s-app-%d-%d.log", screen, os.Getpid(), n))
}

func appLog(screen string, n int) *os.File {
	if err := os.MkdirAll(LogDir(), 0o700); err != nil {
		return nil
	}
	f, err := os.OpenFile(AppLogPath(screen, n), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil
	}
	return f
}
