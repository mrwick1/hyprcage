package perceive

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// Timings of Act and Find. They are variables only so that tests can shrink them.
var (
	settlePoll       = 100 * time.Millisecond
	settleQuiet      = 300 * time.Millisecond
	settleFirstQuiet = time.Second // quiet before the first change: a navigation commits late
	settleTimeout    = 3 * time.Second
	findPoll         = 250 * time.Millisecond
	typeFocusWait    = time.Second           // how long type waits for the clicked field to report focused
	pointerSettle    = 80 * time.Millisecond // between the move and the press: Chrome drops a press that comes with the first enter
)

const defaultMaxNodes = 300

// SnapOpts selects what Snapshot renders.
type SnapOpts struct {
	Mode     Mode
	RootRef  string
	MaxNodes int // 0 means 300
}

// ActOp is one action on a ref.
type ActOp struct {
	Ref       string
	Op        string // click, double_click, type, key, hover, scroll
	Text      string
	Keys      []string
	Direction string // scroll only, default "down"
}

// mode returns m, or ModeInteractive when m is empty. OCR nodes all have
// the role "text", which ModeInteractive drops, so OCR always reads in
// ModeFull.
func mode(src Source, m Mode) Mode {
	if src.Name() == "ocr" {
		return ModeFull
	}
	if m == "" {
		return ModeInteractive
	}
	return m
}

// Snapshot reads, filters, caps to MaxNodes in document order, assigns refs
// and renders.
func Snapshot(ctx context.Context, src Source, t *Table, o SnapOpts) (string, []Node, error) {
	rootKey := ""
	if o.RootRef != "" {
		root, ok := t.Lookup(o.RootRef)
		if !ok {
			return "", nil, screen.Errf(screen.CodeStaleRef, "take a new snapshot without root", "unknown ref %s", o.RootRef)
		}
		rootKey = root.Key
	}
	nodes, err := src.Nodes(ctx)
	if err != nil {
		return "", nil, err
	}
	if rootKey != "" && !slices.ContainsFunc(nodes, func(n Node) bool { return n.Key == rootKey }) {
		// no Assign: the refs of the last snapshot stay valid
		return "", nil, screen.Errf(screen.CodeStaleRef, "take a new snapshot without root", "root %s is gone", o.RootRef)
	}
	filtered := Filter(nodes, mode(src, o.Mode), rootKey)
	limit := o.MaxNodes
	if limit <= 0 {
		limit = defaultMaxNodes
	}
	capped := filtered[:min(limit, len(filtered))]
	t.Assign(capped)
	t.SetSource(src.Name())
	return Render(Header{Source: src.Name(), Nodes: len(capped), Truncated: len(filtered) > limit}, capped), capped, nil
}

// inputter sends pointer and keyboard input. Tests replace the wl.Client.
type inputter interface {
	click(x, y, count int) error
	move(x, y int) error
	typeText(s string) error
	keys(combos []string) error
	scroll(x, y int, direction string, amount int) error
}

type wlInput struct{ cl *wl.Client }

func (w wlInput) click(x, y, count int) error {
	return screen.Click(w.cl, x, y, wl.ButtonLeft, count, nil)
}
func (w wlInput) move(x, y int) error        { return w.cl.Move(x, y) }
func (w wlInput) typeText(s string) error    { return w.cl.Type(s) }
func (w wlInput) keys(combos []string) error { return screen.Keys(w.cl, combos) }
func (w wlInput) scroll(x, y int, direction string, amount int) error {
	return screen.ScrollAt(w.cl, x, y, direction, amount)
}

// Act resolves the ref, reveals it when off screen, sends the input,
// waits for the tree to settle, and returns the diff. The op "key" sends
// the keys to the focused element: it does not click the node first.
func Act(ctx context.Context, cl *wl.Client, src Source, t *Table, op ActOp) (Diff, error) {
	return act(ctx, wlInput{cl}, src, t, op)
}

var ops = map[string]bool{"click": true, "double_click": true, "type": true, "key": true, "hover": true, "scroll": true}

// ValidOp reports whether Act knows op.
func ValidOp(op string) bool { return ops[op] }

func act(ctx context.Context, in inputter, src Source, t *Table, op ActOp) (Diff, error) {
	if !ops[op.Op] {
		return Diff{}, fmt.Errorf("unknown op %q (click, double_click, type, key, hover, scroll)", op.Op)
	}
	old, ok := t.Lookup(op.Ref)
	if !ok {
		return Diff{}, screen.Errf(screen.CodeStaleRef, "take a new snapshot", "unknown ref %s", op.Ref)
	}
	m := mode(src, ModeInteractive)
	fresh, err := src.Nodes(ctx)
	if err != nil {
		return Diff{}, err
	}
	before := Filter(fresh, m, "")
	t.fillRefs(before) // so that the removed lines of the diff carry their refs
	var n Node
	press := false
	if op.Op != "key" { // key goes to the focus: it never touches the node
		if n, err = resolve(old, fresh, before); err != nil {
			return Diff{}, err
		}
		if n.ActionOnly { // its coordinates are not reliable: no pointer input
			if op.Op != "click" && op.Op != "double_click" {
				return Diff{}, screen.Errf(screen.CodeUnsupported, "use click on menu items, or key to navigate the menu",
					"%s %q has no reliable coordinates for %s", n.Role, n.Name, op.Op)
			}
			press = true
		} else if n = stable(ctx, src, n); n.Offscreen {
			r, err := src.Reveal(ctx, n.Key)
			if err != nil {
				return Diff{}, err
			}
			n = r
			if n.Offscreen {
				if src.Name() != "atspi" || op.Op != "click" {
					return Diff{}, screen.Errf(screen.CodeRefOffscreen, "scroll the node into view, then take a new snapshot",
						"%s %q stays off screen", n.Role, n.Name)
				}
				press = true
			}
		}
		if h, ok := src.(hitTester); ok && !press {
			if err := h.HitTest(ctx, n.Key); err != nil {
				return Diff{}, err
			}
		}
	}

	switch {
	case press:
		err = src.Press(ctx, n.Key)
	case op.Op == "click":
		err = pressAt(ctx, in, n, func() error { return in.click(n.X, n.Y, 1) })
	case op.Op == "double_click":
		err = pressAt(ctx, in, n, func() error { return in.click(n.X, n.Y, 2) })
	case op.Op == "type":
		click := func() error { return pressAt(ctx, in, n, func() error { return in.click(n.X, n.Y, 1) }) }
		// OCR has no focus state: click, then type. Other sources click once
		// more, at a fresh stable centre, when the field does not report focused.
		if err = click(); err == nil && src.Name() != "ocr" && !waitFocus(ctx, src, n.Key) {
			n = stable(ctx, src, n)
			if err = click(); err == nil {
				waitFocus(ctx, src, n.Key)
			}
		}
		if err == nil {
			err = in.typeText(op.Text) // focused or not: the text goes out once
		}
	case op.Op == "key":
		err = in.keys(op.Keys)
	case op.Op == "hover":
		err = in.move(n.X, n.Y)
	case op.Op == "scroll":
		dir := op.Direction
		if dir == "" {
			dir = "down"
		}
		err = pressAt(ctx, in, n, func() error { return in.scroll(n.X, n.Y, dir, 3) })
	}
	if err != nil {
		return Diff{}, err
	}

	after, err := settle(ctx, src, m, before)
	if err != nil {
		return Diff{}, err
	}
	t.Assign(after)
	return DiffNodes(before, after), nil
}

// stable reads the nodes every settlePoll until the centre of n is the
// same on two reads in a row, or until typeFocusWait, and returns n from
// the last read. Right after a window maps, Chrome has not laid out its
// toolbar yet, so the first centre can be stale. OCR positions come from
// one screenshot, so an OCR n is returned as is. A read that fails or
// lacks n is skipped.
func stable(ctx context.Context, src Source, n Node) Node {
	if src.Name() == "ocr" {
		return n
	}
	deadline := time.Now().Add(typeFocusWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return n
		case <-time.After(settlePoll):
		}
		nodes, err := src.Nodes(ctx)
		if err != nil {
			continue
		}
		i := slices.IndexFunc(nodes, func(x Node) bool { return x.Key == n.Key })
		if i < 0 {
			continue
		}
		prev := n
		n = nodes[i]
		if n.X == prev.X && n.Y == prev.Y {
			return n
		}
	}
	return n
}

// pressAt moves the pointer to n, waits pointerSettle, then runs press.
// Chrome drops a button press that comes together with the pointer's
// first enter on a surface.
func pressAt(ctx context.Context, in inputter, n Node, press func() error) error {
	if err := in.move(n.X, n.Y); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(pointerSettle):
	}
	return press()
}

// waitFocus reads the nodes every settlePoll until the node key reports
// focused, or until typeFocusWait, and reports whether it did. A click can
// give the focus late, and text typed before it is lost.
func waitFocus(ctx context.Context, src Source, key string) bool {
	deadline := time.Now().Add(typeFocusWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(settlePoll):
		}
		nodes, err := src.Nodes(ctx)
		if err == nil && slices.ContainsFunc(nodes, func(x Node) bool {
			return x.Key == key && slices.Contains(x.States, "focused")
		}) {
			return true
		}
	}
	return false
}

// resolve finds old in the fresh read. A missing Key goes through
// ReResolve against filtered. OCR keys are line positions, not identities:
// an OCR key whose line now has another role or name is re-resolved too,
// so that a layout shift never sends the input to another line.
func resolve(old Node, fresh, filtered []Node) (Node, error) {
	for _, f := range fresh {
		if f.Key != old.Key {
			continue
		}
		if !strings.HasPrefix(old.Key, "ocr:") || (f.Role == old.Role && f.Name == old.Name) {
			return f, nil
		}
		break
	}
	return ReResolve(old, filtered)
}

// settle reads the nodes every settlePoll until they have not changed for
// settleFirstQuiet (no change seen yet) or settleQuiet (after a change), or
// until settleTimeout. A timeout returns the last read.
func settle(ctx context.Context, src Source, m Mode, prev []Node) ([]Node, error) {
	start := time.Now()
	changed := start
	quiet := settleFirstQuiet
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(settlePoll):
		}
		nodes, err := src.Nodes(ctx)
		switch {
		case isStale(err):
			// a node vanished mid-read: try again next tick
		case err != nil:
			return nil, err
		default:
			cur := Filter(nodes, m, "")
			if !DiffNodes(prev, cur).Empty() {
				changed = time.Now()
				quiet = settleQuiet
			}
			prev = cur
		}
		if now := time.Now(); now.Sub(changed) >= quiet || now.Sub(start) >= settleTimeout {
			return prev, nil
		}
	}
}

func isStale(err error) bool {
	var se *screen.Error
	return errors.As(err, &se) && se.Code == screen.CodeStaleRef
}

// Find returns the nodes whose name or value matches re (and role, when set).
// With timeout > 0 it polls every 250 ms until a match appears. A timeout
// returns an empty slice and no error.
func Find(ctx context.Context, src Source, t *Table, re *regexp.Regexp, role string, timeout time.Duration) ([]Node, error) {
	deadline := time.Now().Add(timeout)
	for {
		nodes, err := src.Nodes(ctx)
		if err != nil {
			return nil, err
		}
		// Assign the whole read, not only the matches, so that the refs of
		// the other nodes stay valid.
		all := Filter(nodes, mode(src, ModeInteractive), "")
		t.Assign(all)
		matches := []Node{}
		for _, n := range all {
			if (role == "" || n.Role == role) && (re.MatchString(n.Name) || re.MatchString(n.Value)) {
				matches = append(matches, n)
			}
		}
		if len(matches) > 0 {
			// A miss must not pin the table to a source that shows nothing.
			t.SetSource(src.Name())
		}
		if len(matches) > 0 || !time.Now().Before(deadline) {
			return matches, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(findPoll):
		}
	}
}

// FindAuto is Find for a screen with no recorded source. An app that has
// just launched may not be on the a11y bus yet, so it runs choose again on
// every poll and closes each source after one read. A no_source error from
// choose also means "not up yet" and is retried until the timeout. Only a
// read with a match records its source in the table.
func FindAuto(ctx context.Context, choose func(context.Context) (Source, error), t *Table, re *regexp.Regexp, role string, timeout time.Duration) ([]Node, error) {
	deadline := time.Now().Add(timeout)
	for {
		matches := []Node{}
		src, err := choose(ctx)
		var se *screen.Error
		switch {
		case errors.As(err, &se) && se.Code == screen.CodeNoSource:
			if !time.Now().Before(deadline) {
				return nil, err
			}
		case err != nil:
			return nil, err
		default:
			matches, err = Find(ctx, src, t, re, role, 0)
			src.Close()
			if err != nil {
				return nil, err
			}
		}
		if len(matches) > 0 || !time.Now().Before(deadline) {
			return matches, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(findPoll):
		}
	}
}
