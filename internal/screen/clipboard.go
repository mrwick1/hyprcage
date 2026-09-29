package screen

import (
	"bytes"
	"os"
	"os/exec"
	"strings"

	"github.com/hexadecimil/hyprcage/internal/registry"
)

// innerEnv points a client at the screen's cage and tags it with the
// screen, so that destroy stops it (wl-copy keeps serving in background).
func innerEnv(rec *registry.Screen, base []string) []string {
	var env []string
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if k == "WAYLAND_DISPLAY" || k == "HYPRLAND_INSTANCE_SIGNATURE" || k == "HYPRCAGE_SCREEN" {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "WAYLAND_DISPLAY="+rec.InnerDisplay, "HYPRCAGE_SCREEN="+rec.Name)
}

func withInner(rec *registry.Screen) error {
	if rec.InnerDisplay != "" {
		return nil
	}
	inner, err := registry.ReadInner(rec.Name)
	if err != nil {
		return errf(CodeDead, "", "screen %s has no inner socket", rec.Name)
	}
	rec.InnerDisplay = inner["WAYLAND_DISPLAY"]
	return nil
}

// ClipboardSet puts text on the screen's clipboard.
func ClipboardSet(rec *registry.Screen, text string) error {
	if err := withInner(rec); err != nil {
		return err
	}
	cmd := exec.Command("wl-copy")
	cmd.Env = innerEnv(rec, os.Environ())
	cmd.Stdin = strings.NewReader(text)
	// A file, not a pipe: the forked wl-copy keeps its stderr open, and
	// Wait would block on a pipe until the clipboard is replaced.
	errFile, err := os.CreateTemp("", "hyprcage-wl-copy-*")
	if err != nil {
		return err
	}
	defer os.Remove(errFile.Name())
	defer errFile.Close()
	cmd.Stderr = errFile
	if err := cmd.Run(); err != nil {
		out, _ := os.ReadFile(errFile.Name())
		return errf(CodeCapture, "is wl-clipboard installed?", "wl-copy: %v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// ClipboardGet reads the screen's clipboard; an empty clipboard is "".
func ClipboardGet(rec *registry.Screen) (string, error) {
	if err := withInner(rec); err != nil {
		return "", err
	}
	cmd := exec.Command("wl-paste", "--no-newline")
	cmd.Env = innerEnv(rec, os.Environ())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if strings.Contains(stderr.String(), "Nothing is copied") {
			return "", nil
		}
		return "", errf(CodeCapture, "is wl-clipboard installed?", "wl-paste: %v: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	return string(out), nil
}
