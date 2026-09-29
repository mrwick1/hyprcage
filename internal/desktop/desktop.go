// Package desktop acts on the human's own windows through Hyprland IPC. It
// is not silent: send_shortcut moves keyboard focus to the target and back
// for every key. The human accepted that on 2026-09-29.
package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hexadecimil/hyprcage/internal/hypr"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

// Locked reports whether hyprlock runs, by the comm of every process under
// procRoot ("/proc" outside tests).
func Locked(procRoot string) bool {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return false
	}
	for _, e := range entries {
		comm, err := os.ReadFile(filepath.Join(procRoot, e.Name(), "comm"))
		if err == nil && string(bytes.TrimSpace(comm)) == "hyprlock" {
			return true
		}
	}
	return false
}

var addrRe = regexp.MustCompile(`^0x[0-9a-f]+$`)

// CheckAddress accepts only a Hyprland window address such as 0x55ebac116320.
// Addresses go into Lua and IPC strings, so anything else is refused.
func CheckAddress(a string) error {
	if !addrRe.MatchString(a) {
		return screen.Errf(screen.CodeAddress, "take the address from desktop_windows", "%q is not a window address", a)
	}
	return nil
}

// ponytail: US layout only. A rune outside this map is refused, not guessed;
// add a layout table when a second keyboard layout is in use.
var usShifted = map[rune]string{
	'!': "1", '@': "2", '#': "3", '$': "4", '%': "5", '^': "6", '&': "7", '*': "8", '(': "9", ')': "0",
	'_': "minus", '+': "equal", '{': "bracketleft", '}': "bracketright", '|': "backslash",
	':': "semicolon", '"': "apostrophe", '<': "comma", '>': "period", '?': "slash", '~': "grave",
}

var usPlain = map[rune]string{
	' ': "space", '\n': "Return", '\t': "Tab", '-': "minus", '=': "equal", '[': "bracketleft",
	']': "bracketright", '\\': "backslash", ';': "semicolon", '\'': "apostrophe", ',': "comma",
	'.': "period", '/': "slash", '`': "grave",
}

// KeyFor maps a rune to the send_shortcut modifiers and key name.
func KeyFor(r rune) (string, string, error) {
	switch {
	case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return "", string(r), nil
	case r >= 'A' && r <= 'Z':
		return "SHIFT", string(r + 'a' - 'A'), nil
	}
	if k, ok := usPlain[r]; ok {
		return "", k, nil
	}
	if k, ok := usShifted[r]; ok {
		return "SHIFT", k, nil
	}
	return "", "", screen.Errf(screen.CodeUnsupported, "type it on an agent screen, or use desktop_key", "cannot type %q on the desktop (US layout only)", r)
}

var keyRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
var modNames = map[string]string{"ctrl": "CTRL", "shift": "SHIFT", "alt": "ALT", "super": "SUPER"}

// ParseCombo turns "ctrl+shift+t" into ("CTRL SHIFT", "t").
func ParseCombo(combo string) (string, string, error) {
	parts := strings.Split(combo, "+")
	key := parts[len(parts)-1]
	if !keyRe.MatchString(key) {
		return "", "", screen.Errf(screen.CodeUnsupported, "", "bad key in %q", combo)
	}
	var mods []string
	for _, p := range parts[:len(parts)-1] {
		m, ok := modNames[strings.ToLower(p)]
		if !ok {
			return "", "", screen.Errf(screen.CodeUnsupported, "ctrl, shift, alt or super", "bad modifier %q in %q", p, combo)
		}
		mods = append(mods, m)
	}
	return strings.Join(mods, " "), key, nil
}

// Desktop acts on the human's Hyprland session.
type Desktop struct {
	H *hypr.Instance
	D hypr.ConfigDriver
}

func (d Desktop) ready(addr string) error {
	if d.H == nil {
		return screen.Errf(screen.CodeHyprland, "", "no Hyprland session")
	}
	if Locked("/proc") {
		return screen.Errf(screen.CodeLocked, "wait until the human unlocks", "the desktop is locked")
	}
	if addr == "" {
		return nil
	}
	return CheckAddress(addr)
}

// Windows lists the human's windows, hyprcage's own mirrors left out.
func (d Desktop) Windows() ([]hypr.Client, error) {
	if err := d.ready(""); err != nil {
		return nil, err
	}
	all, err := d.H.Clients()
	if err != nil {
		return nil, err
	}
	out := []hypr.Client{}
	for _, c := range all {
		if c.Class != "hyprcage-mirror" {
			out = append(out, c)
		}
	}
	return out, nil
}

// Focus gives keyboard focus to a window. This one moves the human's focus
// on purpose.
func (d Desktop) Focus(addr string) error {
	if err := d.ready(addr); err != nil {
		return err
	}
	return d.H.Command(d.D.FocusWindowCmd(addr))
}

// Move sends a window to a workspace without following it.
func (d Desktop) Move(addr string, ws int) error {
	if err := d.ready(addr); err != nil {
		return err
	}
	return d.H.Command(d.D.MoveWindowCmd(addr, ws))
}

// Type sends text to a window key by key. Every rune is mapped first, so a
// rune that cannot be typed sends nothing at all.
func (d Desktop) Type(addr, text string) error {
	if err := d.ready(addr); err != nil {
		return err
	}
	type k struct{ mods, key string }
	var keys []k
	for _, r := range text {
		mods, key, err := KeyFor(r)
		if err != nil {
			return err
		}
		keys = append(keys, k{mods, key})
	}
	for _, x := range keys {
		if err := d.H.Command(d.D.SendShortcutCmd(x.mods, x.key, addr)); err != nil {
			return err
		}
	}
	return nil
}

// Key sends key combinations such as ctrl+l to a window. Every combination
// is parsed first.
func (d Desktop) Key(addr string, combos []string) error {
	if err := d.ready(addr); err != nil {
		return err
	}
	type k struct{ mods, key string }
	var keys []k
	for _, c := range combos {
		mods, key, err := ParseCombo(c)
		if err != nil {
			return err
		}
		keys = append(keys, k{mods, key})
	}
	for _, x := range keys {
		if err := d.H.Command(d.D.SendShortcutCmd(x.mods, x.key, addr)); err != nil {
			return err
		}
	}
	return nil
}
