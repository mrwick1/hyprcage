// Package desktop acts on the human's own windows through Hyprland IPC. It
// is not silent: send_shortcut moves keyboard focus to the target and back
// for every key. The human accepted that on 2026-09-29.
package desktop

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/hexadecimil/hyprcage/internal/hypr"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/shellq"
)

// Name is the screen argument that targets the human's desktop.
const Name = "desktop"

// IsDesktop reports whether a screen argument names the desktop.
func IsDesktop(name string) bool { return name == Name }

// Instance returns the desktop's Hyprland instance: HYPRCAGE_DESKTOP_INSTANCE
// when set, otherwise fallback. It refuses a signature with a path separator
// and an instance that does not answer.
func Instance(fallback *hypr.Instance) (*hypr.Instance, error) {
	sig := os.Getenv("HYPRCAGE_DESKTOP_INSTANCE")
	if sig == "" {
		return fallback, nil
	}
	if strings.Contains(sig, "/") || strings.Contains(sig, "..") {
		return nil, screen.Errf(screen.CodeInvalidName, "give a signature from $XDG_RUNTIME_DIR/hypr", "HYPRCAGE_DESKTOP_INSTANCE=%q is not a signature", sig)
	}
	rt, err := hypr.RuntimeDir()
	if err != nil {
		return nil, screen.Errf(screen.CodeHyprland, "", "%v", err)
	}
	inst := &hypr.Instance{Signature: sig, Dir: filepath.Join(rt, "hypr", sig)}
	if !inst.Alive() {
		return nil, screen.Errf(screen.CodeHyprland, "", "HYPRCAGE_DESKTOP_INSTANCE=%s does not answer", sig)
	}
	return inst, nil
}

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

// lockedFn is Locked on /proc. Tests replace it.
var lockedFn = func() bool { return Locked("/proc") }

// Unlocked refuses with session_locked while hyprlock runs.
func Unlocked() error {
	if lockedFn() {
		return screen.Errf(screen.CodeLocked, "wait until the human unlocks", "the desktop is locked")
	}
	return nil
}

// session checks for a Hyprland session that is not locked.
func (d Desktop) session() error {
	if d.H == nil {
		return screen.Errf(screen.CodeHyprland, "", "no Hyprland session")
	}
	return Unlocked()
}

// ready checks the session and the window address before a window command.
func (d Desktop) ready(addr string) error {
	if err := d.session(); err != nil {
		return err
	}
	return CheckAddress(addr)
}

// command sends one command and names a Hyprland failure.
func (d Desktop) command(cmd string) error {
	if err := d.H.Command(cmd); err != nil {
		return screen.Errf(screen.CodeHyprland, "", "%v", err)
	}
	return nil
}

type key struct{ mods, key string }

// send presses keys in a window and counts the keys sent when one fails.
func (d Desktop) send(addr string, keys []key) error {
	for i, k := range keys {
		if err := d.H.Command(d.D.SendShortcutCmd(k.mods, k.key, addr)); err != nil {
			return screen.Errf(screen.CodeHyprland, "", "%d of %d keys sent: %v", i, len(keys), err)
		}
	}
	return nil
}

// Windows lists the human's windows, hyprcage's own mirrors left out.
func (d Desktop) Windows() ([]hypr.Client, error) {
	if err := d.session(); err != nil {
		return nil, err
	}
	all, err := d.H.Clients()
	if err != nil {
		return nil, screen.Errf(screen.CodeHyprland, "", "%v", err)
	}
	out := []hypr.Client{}
	for _, c := range all {
		if !own(c) {
			out = append(out, c)
		}
	}
	return out, nil
}

// own reports whether c is hyprcage's own window: a mirror. An agent
// screen's cage runs on the headless backend and has no window here.
func own(c hypr.Client) bool { return c.Class == "hyprcage-mirror" }

// Focus gives keyboard focus to a window. This one moves the human's focus
// on purpose.
func (d Desktop) Focus(addr string) error {
	if err := d.ready(addr); err != nil {
		return err
	}
	return d.command(d.D.FocusWindowCmd(addr))
}

// Move sends a window to a workspace without following it.
func (d Desktop) Move(addr string, ws int) error {
	if err := d.ready(addr); err != nil {
		return err
	}
	if ws < 1 {
		return screen.Errf(screen.CodeUnsupported, "workspaces start at 1", "workspace %d", ws)
	}
	return d.command(d.D.MoveWindowCmd(addr, ws))
}

// Type sends text to a window key by key. Every rune is mapped first, so a
// rune that cannot be typed sends nothing at all.
func (d Desktop) Type(addr, text string) error {
	if err := d.ready(addr); err != nil {
		return err
	}
	var keys []key
	for _, r := range text {
		mods, k, err := KeyFor(r)
		if err != nil {
			return err
		}
		keys = append(keys, key{mods, k})
	}
	return d.send(addr, keys)
}

// Key sends key combinations such as ctrl+l to a window. Every combination
// is parsed first.
func (d Desktop) Key(addr string, combos []string) error {
	if err := d.ready(addr); err != nil {
		return err
	}
	var keys []key
	for _, c := range combos {
		mods, k, err := ParseCombo(c)
		if err != nil {
			return err
		}
		keys = append(keys, key{mods, k})
	}
	return d.send(addr, keys)
}

var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Launch runs argv on the desktop through the driver's Exec, on workspace
// "<ws> silent" (ws 0: the active workspace) and without initial focus.
// Every argument is quoted, so none is parsed as shell syntax. Exec gives no
// pid: Launch waits up to wait for a window that was not there before and
// returns its pid and address, or 0 and "" when none appears. byWindow
// reports a window matched without the exec rules: its pid had a window
// before (a single-instance app), or it mapped on another workspace. Launch
// moves such a window to ws without following it.
//
// ponytail: the match is by new address only, so an unrelated window that
// maps during the wait can be picked.
func (d Desktop) Launch(argv []string, env map[string]string, cwd string, ws int, wait time.Duration) (pid int, addr string, byWindow bool, err error) {
	if err := d.session(); err != nil {
		return 0, "", false, err
	}
	if len(argv) == 0 {
		return 0, "", false, screen.Errf(screen.CodeUnsupported, "", "command must not be empty")
	}
	var parts []string
	if cwd != "" {
		parts = append(parts, "cd", shellq.Quote(cwd), "&&")
	}
	if len(env) > 0 {
		parts = append(parts, "env")
		for _, k := range slices.Sorted(maps.Keys(env)) {
			if !envKeyRe.MatchString(k) {
				return 0, "", false, screen.Errf(screen.CodeUnsupported, "", "%q is not an environment variable name", k)
			}
			parts = append(parts, k+"="+shellq.Quote(env[k]))
		}
	}
	parts = append(parts, shellq.Join(argv))
	if ws == 0 {
		mons, err := d.H.Monitors()
		if err != nil {
			return 0, "", false, screen.Errf(screen.CodeHyprland, "", "%v", err)
		}
		for _, m := range mons {
			if m.Focused {
				ws = m.ActiveWorkspace.ID
			}
		}
	}
	if ws < 1 {
		return 0, "", false, screen.Errf(screen.CodeUnsupported, "workspaces start at 1", "workspace %d", ws)
	}
	before, err := d.H.Clients()
	if err != nil {
		return 0, "", false, screen.Errf(screen.CodeHyprland, "", "%v", err)
	}
	seen, pids := map[string]bool{}, map[int]bool{}
	for _, c := range before {
		seen[c.Address], pids[c.PID] = true, true
	}
	rules := hypr.ExecRules{Workspace: fmt.Sprintf("%d silent", ws), NoInitialFocus: true}
	if err := d.D.Exec(strings.Join(parts, " "), rules); err != nil {
		return 0, "", false, screen.Errf(screen.CodeHyprland, "", "%v", err)
	}
	for deadline := time.Now().Add(wait); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		all, err := d.H.Clients()
		if err != nil {
			continue
		}
		for _, c := range all {
			if seen[c.Address] || own(c) {
				continue
			}
			if c.Workspace.ID != ws {
				if err := d.command(d.D.MoveWindowCmd(c.Address, ws)); err != nil {
					return c.PID, c.Address, true, err
				}
				return c.PID, c.Address, true, nil
			}
			return c.PID, c.Address, pids[c.PID], nil
		}
	}
	return 0, "", false, nil
}

// Workspace switches the human's visible workspace.
func (d Desktop) Workspace(ws int) error {
	if err := d.session(); err != nil {
		return err
	}
	if ws < 1 {
		return screen.Errf(screen.CodeUnsupported, "workspaces start at 1", "workspace %d", ws)
	}
	return d.command(d.D.WorkspaceCmd(ws))
}
