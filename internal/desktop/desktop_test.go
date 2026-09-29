package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hexadecimil/hyprcage/internal/hypr"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

func TestLocked(t *testing.T) {
	root := t.TempDir()
	mk := func(pid, comm string) {
		_ = os.MkdirAll(filepath.Join(root, pid), 0o755)
		_ = os.WriteFile(filepath.Join(root, pid, "comm"), []byte(comm+"\n"), 0o644)
	}
	mk("10", "kitty")
	if Locked(root) {
		t.Fatal("no hyprlock: want unlocked")
	}
	mk("11", "hyprlock")
	if !Locked(root) {
		t.Fatal("hyprlock runs: want locked")
	}
}

func TestCheckAddress(t *testing.T) {
	for _, ok := range []string{"0x55ebac116320", "0xabc"} {
		if err := CheckAddress(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "55eb", "0x", "0xZZ", `0x1" }) hl.exec_cmd("rm -rf ~`, "0x1 ,x", "address:0x1"} {
		if CheckAddress(bad) == nil {
			t.Errorf("%q: want invalid_address", bad)
		}
	}
}

func TestKeyFor(t *testing.T) {
	cases := map[rune][2]string{
		'a': {"", "a"}, 'Z': {"SHIFT", "z"}, '7': {"", "7"}, ' ': {"", "space"},
		'\n': {"", "Return"}, '\t': {"", "Tab"}, '!': {"SHIFT", "1"}, '_': {"SHIFT", "minus"},
		'.': {"", "period"}, '?': {"SHIFT", "slash"}, '"': {"SHIFT", "apostrophe"},
	}
	for r, want := range cases {
		mods, key, err := KeyFor(r)
		if err != nil || mods != want[0] || key != want[1] {
			t.Errorf("KeyFor(%q) = %q %q %v, want %v", r, mods, key, err, want)
		}
	}
	for _, r := range []rune{'é', '😀', '€'} {
		if _, _, err := KeyFor(r); err == nil {
			t.Errorf("KeyFor(%q): want unsupported_input", r)
		}
	}
}

func TestParseCombo(t *testing.T) {
	cases := map[string][2]string{
		"Return": {"", "Return"}, "ctrl+l": {"CTRL", "l"}, "ctrl+shift+t": {"CTRL SHIFT", "t"},
		"super+alt+F4": {"SUPER ALT", "F4"},
	}
	for in, want := range cases {
		mods, key, err := ParseCombo(in)
		if err != nil || mods != want[0] || key != want[1] {
			t.Errorf("ParseCombo(%q) = %q %q %v", in, mods, key, err)
		}
	}
	for _, bad := range []string{"", "ctrl+", "hyper+x", `x"y`} {
		if _, _, err := ParseCombo(bad); err == nil {
			t.Errorf("ParseCombo(%q): want an error", bad)
		}
	}
}

// deadDesktop points at a socket that does not exist, so any command that
// reaches Hyprland fails with hyprland_unreachable.
func deadDesktop(t *testing.T, locked bool) Desktop {
	t.Setenv("HYPRCAGE_DRIVER", "lua")
	old := lockedFn
	lockedFn = func() bool { return locked }
	t.Cleanup(func() { lockedFn = old })
	h := &hypr.Instance{Signature: "none", Dir: t.TempDir()}
	return Desktop{H: h, D: h.Driver()}
}

func wantCode(t *testing.T, what string, err error, code screen.Code) {
	t.Helper()
	var se *screen.Error
	if !errors.As(err, &se) || se.Code != code {
		t.Errorf("%s: got %v, want %s", what, err, code)
	}
}

func TestNothingReachesHyprland(t *testing.T) {
	d := deadDesktop(t, false)
	for _, a := range []string{"", "0xZZ", `0x1"`} {
		wantCode(t, "focus "+a, d.Focus(a), screen.CodeAddress)
		wantCode(t, "move "+a, d.Move(a, 2), screen.CodeAddress)
		wantCode(t, "type "+a, d.Type(a, "hi"), screen.CodeAddress)
		wantCode(t, "key "+a, d.Key(a, []string{"Return"}), screen.CodeAddress)
	}
	wantCode(t, "type aé", d.Type("0xabc", "aé"), screen.CodeUnsupported)
	wantCode(t, "key bad", d.Key("0xabc", []string{"Return", "hyper+x"}), screen.CodeUnsupported)
	wantCode(t, "move ws 0", d.Move("0xabc", 0), screen.CodeUnsupported)
	wantCode(t, "move ws -1", d.Move("0xabc", -1), screen.CodeUnsupported)
	// A valid call does reach the dead socket, which proves the check above.
	wantCode(t, "focus valid", d.Focus("0xabc"), screen.CodeHyprland)
	_, err := d.Windows()
	wantCode(t, "windows", err, screen.CodeHyprland)
}

func TestLockedRefusesAll(t *testing.T) {
	d := deadDesktop(t, true)
	_, err := d.Windows()
	wantCode(t, "windows", err, screen.CodeLocked)
	wantCode(t, "focus", d.Focus("0xabc"), screen.CodeLocked)
	wantCode(t, "move", d.Move("0xabc", 2), screen.CodeLocked)
	wantCode(t, "type", d.Type("0xabc", "a"), screen.CodeLocked)
	wantCode(t, "key", d.Key("0xabc", []string{"a"}), screen.CodeLocked)
}

func TestPartialTypeCountsKeys(t *testing.T) {
	d := deadDesktop(t, false)
	err := d.Type("0xabc", "ab")
	wantCode(t, "type", err, screen.CodeHyprland)
	if err == nil || !strings.Contains(err.Error(), "0 of 2 keys sent") {
		t.Errorf("type: %v, want the sent count", err)
	}
}
