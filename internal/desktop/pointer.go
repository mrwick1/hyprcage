package desktop

import (
	"image"
	"math"

	"github.com/hexadecimil/hyprcage/internal/hypr"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// wlPointer is the pointer part of *wl.Client, with an explicit extent.
type wlPointer interface {
	MoveIn(x, y, w, h int) error
	PressButton(b wl.Button, pressed bool) error
	Scroll(axis wl.Axis, steps int) error
	HoldModifiers(mods []string) error
}

// Pointer runs pointer input on the desktop and puts the cursor back.
type Pointer struct {
	C  *Conn
	D  hypr.ConfigDriver
	in wlPointer // C.CL outside tests
}

// logical takes global logical coordinates and sends them relative to the
// layout box, with the box as the extent (task 1, item 2: wl_output gives
// an integer scale, so OutputSize is wrong at a fractional scale). It keeps
// the buttons that are down.
type logical struct {
	wlPointer
	box  image.Rectangle
	down map[wl.Button]bool
}

func (l *logical) Move(x, y int) error {
	return l.MoveIn(x-l.box.Min.X, y-l.box.Min.Y, l.box.Dx(), l.box.Dy())
}

func (l *logical) PressButton(b wl.Button, pressed bool) error {
	if pressed {
		l.down[b] = true
	}
	err := l.wlPointer.PressButton(b, pressed)
	if err == nil && !pressed {
		delete(l.down, b)
	}
	return err
}

// layout is the bounding box of the enabled monitors in logical pixels.
func layout(mons []hypr.Monitor) image.Rectangle {
	var box image.Rectangle
	for _, m := range mons {
		if m.Disabled {
			continue
		}
		w := int(math.Round(float64(m.Width) / m.Scale))
		h := int(math.Round(float64(m.Height) / m.Scale))
		box = box.Union(image.Rect(m.X, m.Y, m.X+w, m.Y+h))
	}
	return box
}

// prepare checks the session and that addr (when not empty) is on a visible
// workspace, and returns the input in logical coordinates.
func (p Pointer) prepare(addr string) (*logical, error) {
	if err := (Desktop{H: p.C.H}).session(); err != nil {
		return nil, err
	}
	mons, err := p.C.H.Monitors()
	if err != nil {
		return nil, screen.Errf(screen.CodeHyprland, "", "%v", err)
	}
	if addr != "" {
		w, err := p.C.Window(addr)
		if err != nil {
			return nil, err
		}
		if !visible(w.Workspace.ID, mons) {
			return nil, screen.Errf(screen.CodeWindowHidden, "switch with desktop_workspace first", "window %s is on workspace %d, which is not shown", addr, w.Workspace.ID)
		}
	}
	box := layout(mons)
	if box.Empty() {
		return nil, screen.Errf(screen.CodeHyprland, "", "no monitor")
	}
	in := p.in
	if in == nil {
		in = p.C.CL
	}
	return &logical{wlPointer: in, box: box, down: map[wl.Button]bool{}}, nil
}

// Do reads cursorpos, checks that addr (when not empty) is visible, runs fn,
// then restores the cursor with MoveCursorCmd, also when fn fails or
// panics. It releases every pressed button before the restore.
func (p Pointer) Do(addr string, fn func(in screen.PointerInput) error) (err error) {
	l, err := p.prepare(addr)
	if err != nil {
		return err
	}
	pos, err := p.C.H.CursorPos()
	if err != nil {
		return screen.Errf(screen.CodeHyprland, "", "%v", err)
	}
	defer func() {
		for b := range l.down {
			_ = l.wlPointer.PressButton(b, false)
		}
		if rerr := p.C.H.Command(p.D.MoveCursorCmd(pos.X, pos.Y)); rerr != nil && err == nil {
			err = screen.Errf(screen.CodeHyprland, "", "cursor not restored: %v", rerr)
		}
	}()
	return fn(l)
}

// Hover moves the cursor to (x, y) and leaves it there.
func (p Pointer) Hover(addr string, x, y int) error {
	l, err := p.prepare(addr)
	if err != nil {
		return err
	}
	return l.Move(x, y)
}
