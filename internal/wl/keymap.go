package wl

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// The virtual keyboard needs a keymap before any key event. offstage builds
// one itself (the wtype method): one keycode per distinct keysym, which makes
// typing independent of the human's layout and covers the whole of Unicode.
//
// Which physical key carries a keysym matters to Chromium and Electron: they
// derive the DOM keyCode of anything that is not an ASCII letter or digit
// from the physical key, and a key whose keyCode says Escape, BackSpace, Tab
// or Enter is acted on as such, not inserted. So a keysym is put on the key
// that carries it in the US layout when there is one ("/" on Slash), and
// otherwise on a key that is printable there, never on a control key.

// Fixed keycodes of the four modifier keys, always present in the keymap:
// their US positions.
const (
	kcControl = 8 + 29  // KEY_LEFTCTRL
	kcShift   = 8 + 42  // KEY_LEFTSHIFT
	kcAlt     = 8 + 56  // KEY_LEFTALT
	kcSuper   = 8 + 125 // KEY_LEFTMETA
)

// printableKeys are the evdev codes of the keys that are printable in the US
// layout, the pool a keysym without a key of its own is drawn from. Space is
// left out: a keyCode of 32 activates buttons and scrolls pages.
var printableKeys = []int{
	16, 17, 18, 19, 20, 21, 22, 23, 24, 25, // q..p
	30, 31, 32, 33, 34, 35, 36, 37, 38, // a..l
	44, 45, 46, 47, 48, 49, 50, // z..m
	2, 3, 4, 5, 6, 7, 8, 9, 10, 11, // 1..0
	12, 13, 26, 27, 39, 40, 41, 43, 51, 52, 53, // - = [ ] ; ' ` \ , . /
	86,                                     // the 102nd key of ISO keyboards
	71, 72, 73, 75, 76, 77, 79, 80, 81, 82, // keypad 7 8 9 4 5 6 1 2 3 0
	83, 55, 74, 78, 98, // keypad . * - + /
}

// maxKeysyms is the number of distinct keysyms one generated keymap holds:
// the whole pool.
var maxKeysyms = len(printableKeys)

// usKey is the evdev code of the key that carries a keysym in the US layout,
// for the keysyms that have one: ASCII, and the names Key accepts.
var usKey = func() map[string]int {
	m := map[string]int{}
	rows := []struct {
		plain, shifted string
		first          int
	}{
		{"1234567890-=", "!@#$%^&*()_+", 2},
		{"qwertyuiop[]", "QWERTYUIOP{}", 16},
		{"asdfghjkl;'`", "ASDFGHJKL:\"~", 30},
		{"\\zxcvbnm,./", "|ZXCVBNM<>?", 43},
	}
	for _, r := range rows {
		for i, ch := range r.plain {
			m[keysymForRune(ch)] = r.first + i
		}
		for i, ch := range r.shifted {
			m[keysymForRune(ch)] = r.first + i
		}
	}
	m[keysymForRune(' ')] = 57
	for name, code := range map[string]int{
		"Return": 28, "Escape": 1, "Tab": 15, "BackSpace": 14, "Delete": 111, "Insert": 110,
		"Home": 102, "End": 107, "Page_Up": 104, "Page_Down": 109,
		"Left": 105, "Right": 106, "Up": 103, "Down": 108,
		"F1": 59, "F2": 60, "F3": 61, "F4": 62, "F5": 63, "F6": 64, "F7": 65, "F8": 66, "F9": 67, "F10": 68,
		"F11": 87, "F12": 88, "F13": 183, "F14": 184, "F15": 185, "F16": 186, "F17": 187, "F18": 188,
		"F19": 189, "F20": 190, "F21": 191, "F22": 192, "F23": 193, "F24": 194,
		"space": 57, "plus": 13, "minus": 12, "comma": 51, "period": 52, "slash": 53, "semicolon": 39,
		"apostrophe": 40, "bracketleft": 26, "bracketright": 27, "backslash": 43, "grave": 41, "equal": 13,
		"Menu": 127, "Print": 99, "Pause": 119, "Caps_Lock": 58, "Num_Lock": 69,
		"KP_Enter": 96, "KP_0": 82, "KP_1": 79, "KP_2": 80, "KP_3": 81, "KP_4": 75,
		"KP_5": 76, "KP_6": 77, "KP_7": 71, "KP_8": 72, "KP_9": 73,
	} {
		m[name] = code
	}
	return m
}()

// keyDelay separates a press from its release, and one key from the next.
const keyDelay = 4 * time.Millisecond

// xkb modifier masks, as the modifiers request expects them.
const (
	modShift   = 1
	modControl = 4
	modMod1    = 8 // alt
	modMod4    = 64
)

// keymapFormatXKBV1 is wl_keyboard.keymap_format.xkb_v1.
const keymapFormatXKBV1 = 1

// keymap is a generated xkb keymap: an ordered set of keysym names, each on
// its own key.
type keymap struct {
	order []string
	codes []int          // evdev code of each keysym, parallel to order
	index map[string]int // keysym name -> 1 based slot
	taken map[int]bool   // evdev codes in use
}

func newKeymap() *keymap {
	return &keymap{index: make(map[string]int), taken: make(map[int]bool)}
}

// add gives a keysym a key, returning its slot and false when no key is
// left. The keysym's own US key when it has one and it is free, else the
// first free key of the printable pool.
func (k *keymap) add(sym string) (int, bool) {
	if slot, ok := k.index[sym]; ok {
		return slot, true
	}
	code := usKey[sym]
	if code == 0 || k.taken[code] {
		code = 0
		for _, c := range printableKeys {
			if !k.taken[c] {
				code = c
				break
			}
		}
		if code == 0 {
			return 0, false
		}
	}
	k.taken[code] = true
	k.order = append(k.order, sym)
	k.codes = append(k.codes, code)
	slot := len(k.order)
	k.index[sym] = slot
	return slot, true
}

// keycode returns the xkb keycode (evdev code + 8) of a keysym already in
// the keymap.
func (k *keymap) keycode(sym string) int {
	slot, ok := k.index[sym]
	if !ok {
		return 0
	}
	return k.codes[slot-1] + 8
}

// render writes the keymap in the xkb text format.
func (k *keymap) render() string {
	var b strings.Builder
	b.WriteString("xkb_keymap {\n")
	b.WriteString("xkb_keycodes {\n")
	b.WriteString("minimum = 8;\n")
	b.WriteString("maximum = 255;\n")
	for i := range k.order {
		fmt.Fprintf(&b, "<K%d> = %d;\n", i+1, k.codes[i]+8)
	}
	fmt.Fprintf(&b, "<LCTL> = %d;\n", kcControl)
	fmt.Fprintf(&b, "<LFSH> = %d;\n", kcShift)
	fmt.Fprintf(&b, "<LALT> = %d;\n", kcAlt)
	fmt.Fprintf(&b, "<LWIN> = %d;\n", kcSuper)
	b.WriteString("};\n")
	b.WriteString("xkb_types { include \"complete\" };\n")
	b.WriteString("xkb_compatibility { include \"complete\" };\n")
	b.WriteString("xkb_symbols {\n")
	for i, sym := range k.order {
		fmt.Fprintf(&b, "key <K%d> { [ %s ] };\n", i+1, sym)
	}
	b.WriteString("key <LCTL> { [ Control_L ] };\n")
	b.WriteString("key <LFSH> { [ Shift_L ] };\n")
	b.WriteString("key <LALT> { [ Alt_L ] };\n")
	b.WriteString("key <LWIN> { [ Super_L ] };\n")
	b.WriteString("modifier_map Control { <LCTL> };\n")
	b.WriteString("modifier_map Shift { <LFSH> };\n")
	b.WriteString("modifier_map Mod1 { <LALT> };\n")
	b.WriteString("modifier_map Mod4 { <LWIN> };\n")
	b.WriteString("};\n")
	b.WriteString("};\n")
	return b.String()
}

// runtimeTempFile creates an unlinked file in $XDG_RUNTIME_DIR (or the system
// temporary directory), suitable for passing as a file descriptor.
func runtimeTempFile(pattern string) (*os.File, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	var f *os.File
	var err error
	if dir != "" {
		f, err = os.CreateTemp(dir, pattern)
	}
	if dir == "" || err != nil {
		f, err = os.CreateTemp(os.TempDir(), pattern)
		if err != nil {
			return nil, fmt.Errorf("wl: temporary file: %w", err)
		}
	}
	// The compositor only ever sees the descriptor.
	os.Remove(f.Name())
	return f, nil
}

// keyboardErr explains why there is no virtual keyboard.
func (c *Client) keyboardErr() error {
	if c.vkMgr == 0 {
		return missingErr(ifaceZwpVirtualKeyboardManagerV1)
	}
	return missingErr(ifaceWlSeat)
}

// sendKeymap uploads a keymap on the virtual keyboard. Re-uploading on the
// same object is legitimate (it is how wtype handles long texts).
func (c *Client) sendKeymap(km *keymap) error {
	if c.keyboard == 0 {
		return c.keyboardErr()
	}
	data := km.render()
	f, err := runtimeTempFile("offstage-keymap-*")
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append([]byte(data), 0)); err != nil {
		return fmt.Errorf("wl: writing the keymap: %w", err)
	}
	c.send(c.keyboard, reqZwpVirtualKeyboardV1Keymap, uint32(keymapFormatXKBV1), int(f.Fd()), uint32(len(data)+1))
	return c.flush()
}

// keysymForRune maps one character to the keysym name used in the keymap.
func keysymForRune(r rune) string {
	switch r {
	case '\n':
		return "Return"
	case '\t':
		return "Tab"
	}
	return fmt.Sprintf("U%04X", r)
}

// Type types arbitrary Unicode text through the virtual keyboard.
func (c *Client) Type(text string) error {
	if err := c.ensureInput(); err != nil {
		return err
	}
	if c.keyboard == 0 {
		return c.keyboardErr()
	}
	runes := []rune(text)
	for i := 0; i < len(runes); {
		km := newKeymap()
		codes := make([]int, 0, maxKeysyms)
		j := i
		for ; j < len(runes); j++ {
			sym := keysymForRune(runes[j])
			if _, ok := km.add(sym); !ok {
				break
			}
			codes = append(codes, km.keycode(sym))
		}
		if err := c.sendKeymap(km); err != nil {
			return err
		}
		for _, kc := range codes {
			if err := c.tapKey(kc); err != nil {
				return err
			}
		}
		if err := c.Roundtrip(); err != nil {
			return err
		}
		i = j
	}
	return nil
}

// tapKey presses and releases one xkb keycode. The protocol carries evdev
// codes, which are the xkb keycode minus 8.
func (c *Client) tapKey(keycode int) error {
	if err := c.keyEvent(keycode, true); err != nil {
		return err
	}
	time.Sleep(keyDelay)
	if err := c.keyEvent(keycode, false); err != nil {
		return err
	}
	time.Sleep(keyDelay)
	return nil
}

func (c *Client) keyEvent(keycode int, pressed bool) error {
	var state uint32
	if pressed {
		state = 1
	}
	c.send(c.keyboard, reqZwpVirtualKeyboardV1Key, c.now(), uint32(keycode-8), state)
	return c.flush()
}

// comboModifier is one modifier of a key combination.
type comboModifier struct {
	mask    uint32
	keycode int
}

var comboModifiers = map[string]comboModifier{
	"ctrl":    {modControl, kcControl},
	"control": {modControl, kcControl},
	"shift":   {modShift, kcShift},
	"alt":     {modMod1, kcAlt},
	"super":   {modMod4, kcSuper},
	"meta":    {modMod4, kcSuper},
	"win":     {modMod4, kcSuper},
	"logo":    {modMod4, kcSuper},
}

// keysymNames is the embedded table of xkbcommon names accepted as the last
// token of a combination.
var keysymNames = []string{
	"Return", "Escape", "Tab", "BackSpace", "Delete", "Insert",
	"Home", "End", "Page_Up", "Page_Down",
	"Left", "Right", "Up", "Down",
	"F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12",
	"F13", "F14", "F15", "F16", "F17", "F18", "F19", "F20", "F21", "F22", "F23", "F24",
	"space", "plus", "minus", "comma", "period", "slash", "semicolon",
	"apostrophe", "bracketleft", "bracketright", "backslash", "grave", "equal",
	"Menu", "Print", "Pause", "Caps_Lock", "Num_Lock",
	"KP_Enter", "KP_0", "KP_1", "KP_2", "KP_3", "KP_4",
	"KP_5", "KP_6", "KP_7", "KP_8", "KP_9",
}

var keysymByLower = func() map[string]string {
	m := make(map[string]string, len(keysymNames))
	for _, n := range keysymNames {
		m[strings.ToLower(n)] = n
	}
	return m
}()

// keysymForToken resolves the last token of a combination: a single
// character becomes a Unicode keysym, anything else must be a known name.
func keysymForToken(tok string) (string, error) {
	r := []rune(tok)
	if len(r) == 1 {
		return fmt.Sprintf("U%04X", r[0]), nil
	}
	if n, ok := keysymByLower[strings.ToLower(tok)]; ok {
		return n, nil
	}
	return "", fmt.Errorf("wl: unknown key name %q (expected a single character or an xkb keysym name such as Return or F5)", tok)
}

// Key presses and releases a combination such as "ctrl+l" or "Return".
func (c *Client) Key(combo string) error {
	if err := c.ensureInput(); err != nil {
		return err
	}
	if c.keyboard == 0 {
		return c.keyboardErr()
	}
	tokens := strings.Split(combo, "+")
	for i, t := range tokens {
		tokens[i] = strings.TrimSpace(t)
		if tokens[i] == "" {
			return fmt.Errorf("wl: malformed key combination %q", combo)
		}
	}
	var mods []comboModifier
	var mask uint32
	for _, t := range tokens[:len(tokens)-1] {
		m, ok := comboModifiers[strings.ToLower(t)]
		if !ok {
			return fmt.Errorf("wl: unknown modifier %q in %q (expected ctrl, shift, alt or super)", t, combo)
		}
		mods = append(mods, m)
		mask |= m.mask
	}
	sym, err := keysymForToken(tokens[len(tokens)-1])
	if err != nil {
		return err
	}

	km := newKeymap()
	if _, ok := km.add(sym); !ok {
		return fmt.Errorf("wl: cannot map keysym %s", sym)
	}
	if err := c.sendKeymap(km); err != nil {
		return err
	}

	if mask != 0 {
		c.send(c.keyboard, reqZwpVirtualKeyboardV1Modifiers, mask, uint32(0), uint32(0), uint32(0))
		if err := c.flush(); err != nil {
			return err
		}
	}
	for _, m := range mods {
		if err := c.keyEvent(m.keycode, true); err != nil {
			return err
		}
		time.Sleep(keyDelay)
	}
	if err := c.tapKey(km.keycode(sym)); err != nil {
		return err
	}
	for i := len(mods) - 1; i >= 0; i-- {
		if err := c.keyEvent(mods[i].keycode, false); err != nil {
			return err
		}
		time.Sleep(keyDelay)
	}
	if mask != 0 {
		c.send(c.keyboard, reqZwpVirtualKeyboardV1Modifiers, uint32(0), uint32(0), uint32(0), uint32(0))
	}
	return c.Roundtrip()
}
