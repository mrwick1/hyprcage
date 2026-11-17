package headless

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hexadecimil/offstage/internal/wl"
)

// CDP modifier bits.
const (
	modAlt   = 1
	modCtrl  = 2
	modMeta  = 4
	modShift = 8
)

// Same names as wl accepts.
var modifierBits = map[string]int{
	"ctrl": modCtrl, "control": modCtrl, "shift": modShift, "alt": modAlt,
	"super": modMeta, "meta": modMeta, "win": modMeta, "logo": modMeta,
}

// multiClickGap is the longest gap between presses that count as one
// multi-click.
const multiClickGap = 500 * time.Millisecond

type pointerState struct {
	x, y      int
	mods      int // held with HoldModifiers
	buttons   int // pressed buttons, CDP bitmask
	lastBtn   wl.Button
	lastAt    time.Time
	lastX     int
	lastY     int
	lastCount int
}

func buttonInfo(b wl.Button) (name string, bit int, err error) {
	switch b {
	case wl.ButtonLeft:
		return "left", 1, nil
	case wl.ButtonRight:
		return "right", 2, nil
	case wl.ButtonMiddle:
		return "middle", 4, nil
	}
	return "", 0, fmt.Errorf("headless: unknown button %d", int(b))
}

// mouse sends one mouse event. The caller holds mu.
func (b *Browser) mouse(params map[string]any) error {
	p := &b.pointer
	params["x"], params["y"], params["modifiers"], params["buttons"] = p.x, p.y, p.mods, p.buttons
	_, err := b.conn.Call(context.Background(), b.session, "Input.dispatchMouseEvent", params)
	return err
}

// Move puts the pointer at (x, y).
func (b *Browser) Move(x, y int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pointer.x, b.pointer.y = x, y
	return b.mouse(map[string]any{"type": "mouseMoved", "button": "none"})
}

// PressButton presses or releases a button at the pointer.
func (b *Browser) PressButton(btn wl.Button, pressed bool) error {
	name, bit, err := buttonInfo(btn)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	p := &b.pointer
	if !pressed {
		p.buttons &^= bit
		return b.mouse(map[string]any{"type": "mouseReleased", "button": name, "clickCount": max(p.lastCount, 1)})
	}
	now := time.Now()
	if p.lastCount > 0 && p.lastBtn == btn && p.lastX == p.x && p.lastY == p.y && now.Sub(p.lastAt) <= multiClickGap {
		p.lastCount++
	} else {
		p.lastCount = 1
	}
	p.lastBtn, p.lastX, p.lastY, p.lastAt = btn, p.x, p.y, now
	p.buttons |= bit
	return b.mouse(map[string]any{"type": "mousePressed", "button": name, "clickCount": p.lastCount})
}

// Scroll turns the wheel by steps clicks: positive is down or right.
func (b *Browser) Scroll(axis wl.Axis, steps int) error {
	if axis != wl.AxisVertical && axis != wl.AxisHorizontal {
		return fmt.Errorf("headless: unknown scroll axis %d", int(axis))
	}
	if steps == 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	dx, dy := 0, 0
	if axis == wl.AxisVertical {
		dy = steps * 100
	} else {
		dx = steps * 100
	}
	return b.mouse(map[string]any{"type": "mouseWheel", "deltaX": dx, "deltaY": dy})
}

// HoldModifiers keeps the named modifiers down for the mouse and key
// events that follow. An empty list releases them.
func (b *Browser) HoldModifiers(mods []string) error {
	mask := 0
	for _, t := range mods {
		m, ok := modifierBits[strings.ToLower(strings.TrimSpace(t))]
		if !ok {
			return fmt.Errorf("headless: unknown modifier %q (expected ctrl, shift, alt or super)", t)
		}
		mask |= m
	}
	b.mu.Lock()
	b.pointer.mods = mask
	b.mu.Unlock()
	return nil
}

// Type inserts text at the focus. \n presses Return and \t presses Tab as
// real keys, so key handlers see them, as on a Wayland screen.
func (b *Browser) Type(text string) error {
	for text != "" {
		i := strings.IndexAny(text, "\n\t")
		if i < 0 {
			i = len(text)
		}
		if i > 0 {
			b.mu.Lock()
			_, err := b.conn.Call(context.Background(), b.session, "Input.insertText", map[string]any{"text": text[:i]})
			b.mu.Unlock()
			if err != nil {
				return err
			}
		}
		if i == len(text) {
			return nil
		}
		key := "Return"
		if text[i] == '\t' {
			key = "Tab"
		}
		if err := b.Key(key); err != nil {
			return err
		}
		text = text[i+1:]
	}
	return nil
}
