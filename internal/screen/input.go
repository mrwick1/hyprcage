package screen

import (
	"fmt"
	"strings"
	"time"

	"github.com/hexadecimil/hyprcage/internal/wl"
)

// Gesture timings (cahier §4.4).
const (
	clickHold      = 30 * time.Millisecond
	doubleClickGap = 80 * time.Millisecond
	dragSteps      = 12
	dragDuration   = 250 * time.Millisecond
)

// ParseButton maps "left", "right", "middle" (empty = left) to a button.
func ParseButton(s string) (wl.Button, error) {
	switch strings.ToLower(s) {
	case "", "left":
		return wl.ButtonLeft, nil
	case "right":
		return wl.ButtonRight, nil
	case "middle":
		return wl.ButtonMiddle, nil
	}
	return 0, fmt.Errorf("unknown button %q (left, right, middle)", s)
}

// PointerInput is the pointer part of *wl.Client.
type PointerInput interface {
	Move(x, y int) error
	PressButton(b wl.Button, pressed bool) error
	Scroll(axis wl.Axis, steps int) error
	HoldModifiers(mods []string) error
}

// Click moves to (x, y) and clicks count times with the given button.
func Click(cl PointerInput, x, y int, b wl.Button, count int, modifiers []string) error {
	if count <= 0 {
		count = 1
	}
	if err := cl.Move(x, y); err != nil {
		return err
	}
	if len(modifiers) > 0 {
		if err := cl.HoldModifiers(modifiers); err != nil {
			return err
		}
		defer cl.HoldModifiers(nil)
	}
	for i := 0; i < count; i++ {
		if i > 0 {
			time.Sleep(doubleClickGap)
		}
		if err := cl.PressButton(b, true); err != nil {
			return err
		}
		time.Sleep(clickHold)
		if err := cl.PressButton(b, false); err != nil {
			return err
		}
	}
	return nil
}

// Drag presses at (x1, y1), moves in dragSteps interpolated steps and
// releases at (x2, y2).
func Drag(cl PointerInput, x1, y1, x2, y2 int, duration time.Duration) error {
	if duration <= 0 {
		duration = dragDuration
	}
	if err := cl.Move(x1, y1); err != nil {
		return err
	}
	if err := cl.PressButton(wl.ButtonLeft, true); err != nil {
		return err
	}
	step := duration / dragSteps
	for i := 1; i <= dragSteps; i++ {
		time.Sleep(step)
		x := x1 + (x2-x1)*i/dragSteps
		y := y1 + (y2-y1)*i/dragSteps
		if err := cl.Move(x, y); err != nil {
			return err
		}
	}
	time.Sleep(clickHold)
	return cl.PressButton(wl.ButtonLeft, false)
}

// ScrollAt moves to (x, y) and scrolls amount wheel clicks in a direction.
func ScrollAt(cl PointerInput, x, y int, direction string, amount int) error {
	if amount <= 0 {
		amount = 3
	}
	if err := cl.Move(x, y); err != nil {
		return err
	}
	switch strings.ToLower(direction) {
	case "down":
		return cl.Scroll(wl.AxisVertical, amount)
	case "up":
		return cl.Scroll(wl.AxisVertical, -amount)
	case "right":
		return cl.Scroll(wl.AxisHorizontal, amount)
	case "left":
		return cl.Scroll(wl.AxisHorizontal, -amount)
	}
	return fmt.Errorf("unknown direction %q (up, down, left, right)", direction)
}

// Keys presses combinations one after the other.
func Keys(cl *wl.Client, combos []string) error {
	for _, combo := range combos {
		if err := cl.Key(combo); err != nil {
			return fmt.Errorf("key %q: %w", combo, err)
		}
	}
	return nil
}
