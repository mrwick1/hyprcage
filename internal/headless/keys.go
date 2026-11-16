package headless

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

// keyDef is what Input.dispatchKeyEvent needs for one key.
type keyDef struct {
	key, code string
	vk        int
	text      string // typed text without Ctrl/Alt/Meta; "" for none
}

// namedKeys uses the xkb names that wl accepts, lower-cased.
var namedKeys = map[string]keyDef{
	"return":    {"Enter", "Enter", 13, "\r"},
	"kp_enter":  {"Enter", "NumpadEnter", 13, "\r"},
	"tab":       {"Tab", "Tab", 9, "\t"},
	"escape":    {"Escape", "Escape", 27, ""},
	"backspace": {"Backspace", "Backspace", 8, ""},
	"delete":    {"Delete", "Delete", 46, ""},
	"insert":    {"Insert", "Insert", 45, ""},
	"home":      {"Home", "Home", 36, ""},
	"end":       {"End", "End", 35, ""},
	"page_up":   {"PageUp", "PageUp", 33, ""},
	"page_down": {"PageDown", "PageDown", 34, ""},
	"left":      {"ArrowLeft", "ArrowLeft", 37, ""},
	"up":        {"ArrowUp", "ArrowUp", 38, ""},
	"right":     {"ArrowRight", "ArrowRight", 39, ""},
	"down":      {"ArrowDown", "ArrowDown", 40, ""},
	"menu":      {"ContextMenu", "ContextMenu", 93, ""},
	"print":     {"PrintScreen", "PrintScreen", 44, ""},
	"pause":     {"Pause", "Pause", 19, ""},
	"caps_lock": {"CapsLock", "CapsLock", 20, ""},
	"num_lock":  {"NumLock", "NumLock", 144, ""},
	"space":     {" ", "Space", 32, " "},
}

// punct maps a punctuation character to its key.
var punct = map[rune]struct {
	code string
	vk   int
}{
	';': {"Semicolon", 186}, '=': {"Equal", 187}, ',': {"Comma", 188}, '-': {"Minus", 189},
	'.': {"Period", 190}, '/': {"Slash", 191}, '`': {"Backquote", 192}, '[': {"BracketLeft", 219},
	'\\': {"Backslash", 220}, ']': {"BracketRight", 221}, '\'': {"Quote", 222}, '+': {"Equal", 187},
}

// keyNameChars are the xkb names of punctuation keys.
var keyNameChars = map[string]rune{
	"plus": '+', "minus": '-', "comma": ',', "period": '.', "slash": '/', "semicolon": ';',
	"apostrophe": '\'', "bracketleft": '[', "bracketright": ']', "backslash": '\\', "grave": '`', "equal": '=',
}

// lookupKey resolves the last token of a combination.
func lookupKey(tok string, shift bool) (keyDef, error) {
	r := []rune(tok)
	if len(r) == 1 {
		return charKey(r[0], shift), nil
	}
	n := strings.ToLower(tok)
	if k, ok := namedKeys[n]; ok {
		return k, nil
	}
	if c, ok := keyNameChars[n]; ok {
		return charKey(c, shift), nil
	}
	if len(n) >= 2 && n[0] == 'f' {
		var f int
		if _, err := fmt.Sscanf(n[1:], "%d", &f); err == nil && fmt.Sprint(f) == n[1:] && f >= 1 && f <= 24 {
			return keyDef{key: fmt.Sprintf("F%d", f), code: fmt.Sprintf("F%d", f), vk: 111 + f}, nil
		}
	}
	if len(n) == 4 && strings.HasPrefix(n, "kp_") && n[3] >= '0' && n[3] <= '9' {
		d := rune(n[3])
		return keyDef{key: string(d), code: "Numpad" + string(d), vk: 96 + int(d-'0'), text: string(d)}, nil
	}
	return keyDef{}, fmt.Errorf("headless: unknown key name %q (expected a single character or an xkb keysym name such as Return or F5)", tok)
}

func charKey(r rune, shift bool) keyDef {
	switch {
	case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
		u := unicode.ToUpper(r)
		k := unicode.ToLower(r)
		if shift {
			k = u
		}
		return keyDef{key: string(k), code: "Key" + string(u), vk: int(u), text: string(k)}
	case r >= '0' && r <= '9':
		return keyDef{key: string(r), code: "Digit" + string(r), vk: int(r), text: string(r)}
	}
	if p, ok := punct[r]; ok {
		return keyDef{key: string(r), code: p.code, vk: p.vk, text: string(r)}
	}
	return keyDef{key: string(r), text: string(r)}
}

// Key presses and releases a combination such as "ctrl+a" or "Return".
func (b *Browser) Key(combo string) error {
	tokens := strings.Split(combo, "+")
	for i, t := range tokens {
		tokens[i] = strings.TrimSpace(t)
		if tokens[i] == "" {
			return fmt.Errorf("headless: malformed key combination %q", combo)
		}
	}
	mask := 0
	for _, t := range tokens[:len(tokens)-1] {
		m, ok := modifierBits[strings.ToLower(t)]
		if !ok {
			return fmt.Errorf("headless: unknown modifier %q in %q (expected ctrl, shift, alt or super)", t, combo)
		}
		mask |= m
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	mask |= b.pointer.mods
	k, err := lookupKey(tokens[len(tokens)-1], mask&modShift != 0)
	if err != nil {
		return err
	}
	text := k.text
	if mask&(modCtrl|modAlt|modMeta) != 0 {
		text = ""
	}
	typ := "rawKeyDown"
	down := map[string]any{"key": k.key, "code": k.code, "windowsVirtualKeyCode": k.vk, "modifiers": mask}
	if text != "" {
		typ = "keyDown"
		down["text"], down["unmodifiedText"] = text, text
	}
	down["type"] = typ
	up := map[string]any{"type": "keyUp", "key": k.key, "code": k.code, "windowsVirtualKeyCode": k.vk, "modifiers": mask}
	if _, err := b.conn.Call(context.Background(), b.session, "Input.dispatchKeyEvent", down); err != nil {
		return err
	}
	_, err = b.conn.Call(context.Background(), b.session, "Input.dispatchKeyEvent", up)
	return err
}
