package desktop

import (
	"os"
	"path/filepath"
	"testing"
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
