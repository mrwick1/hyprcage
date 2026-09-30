package perceive

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/hexadecimil/hyprcage/internal/desktop"
	"github.com/hexadecimil/hyprcage/internal/hypr"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// inserter is a Source that inserts text into a node without focus or keys.
// Only AT-SPI implements it (EditableText.InsertText at the caret, or at the end).
type inserter interface {
	Insert(ctx context.Context, key, text string) error
}

// cdpInput is a Source that sends input as CDP events, without the pointer.
// Only CDP implements it.
type cdpInput interface {
	Mouse(ctx context.Context, key, op string, count int, direction string) error
	InsertText(ctx context.Context, key, text string) error
	Keys(ctx context.Context, combos []string) error
}

// desktopInput is the fallback input of one desktop window: the pointer
// with a cursor restore, and send_shortcut for keys.
type desktopInput struct {
	p    desktop.Pointer
	d    desktop.Desktop
	addr string
}

func (w desktopInput) click(x, y, count int) error {
	return w.p.Do(w.addr, func(in screen.PointerInput) error {
		if err := in.Move(x, y); err != nil {
			return err
		}
		time.Sleep(pointerSettle) // see pressAt
		return screen.Click(in, x, y, wl.ButtonLeft, count, nil)
	})
}
func (w desktopInput) move(x, y int) error        { return w.p.Hover(w.addr, x, y) }
func (w desktopInput) typeText(s string) error    { return w.d.Type(w.addr, s) }
func (w desktopInput) keys(combos []string) error { return w.d.Key(w.addr, combos) }
func (w desktopInput) scroll(x, y int, direction string, amount int) error {
	return w.p.Do(w.addr, func(in screen.PointerInput) error { return screen.ScrollAt(in, x, y, direction, amount) })
}

// DesktopAct is Act on desktop windows. For each op it takes the first path
// that can do it: AT-SPI (no cursor, no focus), then CDP events, then the
// pointer with a cursor restore or send_shortcut. The first line of the
// result names the path: path=<atspi|cdp|pointer|shortcut>. wins are the
// windows of the ref table; the fallback goes to the one that holds the node.
func DesktopAct(ctx context.Context, p desktop.Pointer, d desktop.Desktop, wins []hypr.Client, src Source, t *Table, op ActOp) (string, error) {
	return desktopAct(ctx, func(addr string) inputter { return desktopInput{p, d, addr} }, wins, src, t, op)
}

func desktopAct(ctx context.Context, input func(addr string) inputter, wins []hypr.Client, src Source, t *Table, op ActOp) (string, error) {
	path := ""
	diff, err := actWith(ctx, src, t, op, func(n Node, press bool) error {
		var err error
		path, err = desktopSend(ctx, input, wins, src, n, press, op)
		return err
	})
	if err != nil {
		return "", err
	}
	return "path=" + path + "\n" + diff.String(), nil
}

// desktopSend sends op to n on the first path that can do it, and names the path.
func desktopSend(ctx context.Context, input func(addr string) inputter, wins []hypr.Client, src Source, n Node, press bool, op ActOp) (string, error) {
	if press { // no usable coordinates: DoAction or nothing
		return "atspi", src.Press(ctx, n.Key)
	}
	// A textbox's action activates it (Enter): a click on it goes the pointer
	// way. So does a menu title of a menu bar: its Press succeeds, but the
	// menu does not open on Wayland.
	if op.Op == "click" && src.Name() == "atspi" && n.Role != "textbox" && !menuTitle(src, n.Key) {
		if err := src.Press(ctx, n.Key); !unsupported(err) {
			return "atspi", err
		}
	}
	if ins, ok := src.(inserter); ok && op.Op == "type" {
		if err := ins.Insert(ctx, n.Key, op.Text); !unsupported(err) {
			return "atspi", err
		}
	}
	inner := src
	if s, ok := src.(shifted); ok {
		inner = s.Source
	}
	if c, ok := inner.(cdpInput); ok {
		var err error
		switch op.Op {
		case "click":
			err = c.Mouse(ctx, n.Key, op.Op, 1, "")
		case "double_click":
			err = c.Mouse(ctx, n.Key, op.Op, 2, "")
		case "hover", "scroll":
			err = c.Mouse(ctx, n.Key, op.Op, 0, op.Direction)
		case "type":
			err = c.InsertText(ctx, n.Key, op.Text)
		case "key":
			err = c.Keys(ctx, op.Keys)
		}
		if op.Op != "key" || !unsupported(err) { // a key CDP cannot name goes to send_shortcut

			return "cdp", err
		}
	}
	addr, err := windowOf(wins, n, op.Op)
	if err != nil {
		return "", err
	}
	in := input(addr)
	switch op.Op {
	case "click":
		return "pointer", in.click(n.X, n.Y, 1)
	case "double_click":
		return "pointer", in.click(n.X, n.Y, 2)
	case "hover":
		return "pointer", in.move(n.X, n.Y)
	case "scroll":
		return "pointer", in.scroll(n.X, n.Y, direction(op), 3)
	case "type":
		// send_shortcut types into the window's focused widget: focus the field first.
		if !slices.Contains(n.States, "focused") {
			if err := in.click(n.X, n.Y, 1); err != nil {
				return "pointer", err
			}
		}
		return "shortcut", in.typeText(op.Text)
	}
	return "shortcut", in.keys(op.Keys)
}

// windowOf returns the window that the fallback input goes to: the only
// window, else the one that holds the node's centre.
func windowOf(wins []hypr.Client, n Node, op string) (string, error) {
	if len(wins) == 1 {
		return wins[0].Address, nil
	}
	if op != "key" {
		for _, w := range wins {
			if n.X >= w.At[0] && n.Y >= w.At[1] && n.X < w.At[0]+w.Size[0] && n.Y < w.At[1]+w.Size[1] {
				return w.Address, nil
			}
		}
	}
	return "", screen.Errf(screen.CodeUnsupported, "snapshot with window, then act on its refs",
		"no single desktop window for %s on %s %q", op, n.Role, n.Name)
}

// menuTitle reports whether key is a title of a menu bar in src's last read:
// a "menu" (GTK) or a "menu item" (Qt) whose parent is the menu bar.
func menuTitle(src Source, key string) bool {
	a, ok := src.(*atspiSource)
	if !ok {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	o := a.last[key]
	return (o.Role == "menu" || o.Role == "menu item") && a.last[atspiKey(o.Bus, o.Parent)].Role == "menu bar"
}

// unsupported reports whether err means that the path cannot do the op.
func unsupported(err error) bool {
	var se *screen.Error
	return errors.As(err, &se) && se.Code == screen.CodeUnsupported
}
