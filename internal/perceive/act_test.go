package perceive

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// fakeSource returns reads[i] on the i-th Nodes call, then the last one.
type fakeSource struct {
	mu      sync.Mutex
	name    string
	reads   [][]Node
	n       int
	reveal  func(key string) (Node, error)
	pressed []string
	closed  bool
}

func (f *fakeSource) Name() string { return f.name }

func (f *fakeSource) Nodes(context.Context) ([]Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := min(f.n, len(f.reads)-1)
	f.n++
	return append([]Node(nil), f.reads[i]...), nil
}

func (f *fakeSource) Reveal(_ context.Context, key string) (Node, error) {
	if f.reveal != nil {
		return f.reveal(key)
	}
	return Node{}, errors.New("no reveal")
}

func (f *fakeSource) Press(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pressed = append(f.pressed, key)
	return nil
}

func (f *fakeSource) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// changing returns a new button name on every read.
type changing struct{ fakeSource }

func (c *changing) Nodes(context.Context) ([]Node, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return []Node{{Key: "k1", Role: "button", Name: fmt.Sprint("n", c.n), X: 10, Y: 10}}, nil
}

// fakeInput records the input that act sends.
type fakeInput struct {
	calls []string
	at    []time.Time // when each move and click came
}

func (f *fakeInput) click(x, y, count int) error {
	f.calls = append(f.calls, fmt.Sprintf("click %d,%d x%d", x, y, count))
	f.at = append(f.at, time.Now())
	return nil
}
func (f *fakeInput) move(x, y int) error {
	f.calls = append(f.calls, fmt.Sprintf("move %d,%d", x, y))
	f.at = append(f.at, time.Now())
	return nil
}
func (f *fakeInput) typeText(s string) error { f.calls = append(f.calls, "type "+s); return nil }
func (f *fakeInput) keys(k []string) error {
	f.calls = append(f.calls, "keys "+strings.Join(k, "+"))
	return nil
}
func (f *fakeInput) scroll(x, y int, dir string, amount int) error {
	f.calls = append(f.calls, fmt.Sprintf("scroll %d,%d %s %d", x, y, dir, amount))
	return nil
}

// fastTiming shrinks the settle and find timings for the test.
func fastTiming(t *testing.T) {
	t.Helper()
	p, q, fq, to, fp, tf, ps := settlePoll, settleQuiet, settleFirstQuiet, settleTimeout, findPoll, typeFocusWait, pointerSettle
	settlePoll, settleQuiet, settleFirstQuiet, settleTimeout, findPoll = time.Millisecond, 5*time.Millisecond, 5*time.Millisecond, 200*time.Millisecond, time.Millisecond
	typeFocusWait, pointerSettle = 5*time.Millisecond, time.Millisecond
	t.Cleanup(func() {
		settlePoll, settleQuiet, settleFirstQuiet, settleTimeout, findPoll, typeFocusWait, pointerSettle = p, q, fq, to, fp, tf, ps
	})
}

func button(key, name string, x, y int) Node {
	return Node{Key: key, Role: "button", Name: name, X: x, Y: y}
}

func TestTimingDefaults(t *testing.T) {
	if settlePoll != 100*time.Millisecond || settleQuiet != 300*time.Millisecond || settleFirstQuiet != time.Second ||
		settleTimeout != 3*time.Second || findPoll != 250*time.Millisecond {
		t.Fatalf("timings %v %v %v %v %v", settlePoll, settleQuiet, settleFirstQuiet, settleTimeout, findPoll)
	}
}

func TestSnapshotCapsAndFlags(t *testing.T) {
	var nodes []Node
	for i := range 400 {
		nodes = append(nodes, button(fmt.Sprint("k", i), fmt.Sprint("b", i), i, i))
	}
	src := &fakeSource{name: "cdp", reads: [][]Node{nodes}}
	out, got, err := Snapshot(context.Background(), src, NewTable(), SnapOpts{Mode: ModeInteractive, MaxNodes: 300})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 301 || len(got) != 300 {
		t.Fatalf("%d lines, %d nodes", len(lines), len(got))
	}
	if lines[0] != "source=cdp nodes=300 truncated=true" {
		t.Fatalf("header %q", lines[0])
	}
	if got[299].Name != "b299" || got[0].Ref != "e1" {
		t.Fatalf("order or refs: %+v %+v", got[0], got[299])
	}
}

func TestSnapshotDefaultCap(t *testing.T) {
	var nodes []Node
	for i := range 301 {
		nodes = append(nodes, button(fmt.Sprint("k", i), "b", 0, 0))
	}
	_, got, err := Snapshot(context.Background(), &fakeSource{name: "cdp", reads: [][]Node{nodes}}, NewTable(), SnapOpts{Mode: ModeInteractive})
	if err != nil || len(got) != 300 {
		t.Fatalf("%d nodes, %v", len(got), err)
	}
}

func TestSnapshotRoot(t *testing.T) {
	nodes := []Node{
		{Key: "root", Role: "main", Name: "m"},
		{Key: "a", Parent: "root", Role: "navigation", Name: "nav"},
		{Key: "a1", Parent: "a", Role: "link", Name: "Home"},
		{Key: "b", Parent: "root", Role: "button", Name: "Other"},
	}
	src := &fakeSource{name: "cdp", reads: [][]Node{nodes}}
	tb := NewTable()
	if _, _, err := Snapshot(context.Background(), src, tb, SnapOpts{Mode: ModeInteractive}); err != nil {
		t.Fatal(err)
	}
	out, got, err := Snapshot(context.Background(), src, tb, SnapOpts{Mode: ModeInteractive, RootRef: "e2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "a" || got[1].Key != "a1" || strings.Contains(out, "Other") {
		t.Fatalf("subtree:\n%s", out)
	}
	_, _, err = Snapshot(context.Background(), src, tb, SnapOpts{RootRef: "e99"})
	stale(t, err)
}

func TestActClickSendsCentre(t *testing.T) {
	fastTiming(t)
	nodes := []Node{button("k1", "OK", 120, 45)}
	src := &fakeSource{name: "cdp", reads: [][]Node{nodes}}
	tb := NewTable()
	tb.Assign(append([]Node(nil), nodes...))
	in := &fakeInput{}
	if _, err := act(context.Background(), in, src, tb, ActOp{Ref: "e1", Op: "click"}); err != nil {
		t.Fatal(err)
	}
	if len(in.calls) != 2 || in.calls[1] != "click 120,45 x1" {
		t.Fatalf("calls %v", in.calls)
	}
}

func TestActOps(t *testing.T) {
	fastTiming(t)
	for _, c := range []struct {
		op   ActOp
		want string
	}{
		{ActOp{Op: "double_click"}, "move 5,6|click 5,6 x2"},
		{ActOp{Op: "type", Text: "hi"}, "move 5,6|click 5,6 x1|move 5,6|click 5,6 x1|type hi"}, // never focused: two clicks
		{ActOp{Op: "key", Keys: []string{"ctrl+a", "Delete"}}, "keys ctrl+a+Delete"},
		{ActOp{Op: "hover"}, "move 5,6"},
		{ActOp{Op: "scroll"}, "move 5,6|scroll 5,6 down 3"},
		{ActOp{Op: "scroll", Direction: "up"}, "move 5,6|scroll 5,6 up 3"},
	} {
		nodes := []Node{button("k1", "OK", 5, 6)}
		tb := NewTable()
		tb.Assign(append([]Node(nil), nodes...))
		in := &fakeInput{}
		c.op.Ref = "e1"
		if _, err := act(context.Background(), in, &fakeSource{name: "cdp", reads: [][]Node{nodes}}, tb, c.op); err != nil {
			t.Fatal(c.op.Op, err)
		}
		if got := strings.Join(in.calls, "|"); got != c.want {
			t.Errorf("%s: %q, want %q", c.op.Op, got, c.want)
		}
	}
	tb := NewTable()
	tb.Assign([]Node{button("k1", "OK", 5, 6)})
	if _, err := act(context.Background(), &fakeInput{}, &fakeSource{name: "cdp", reads: [][]Node{{button("k1", "OK", 5, 6)}}}, tb, ActOp{Ref: "e1", Op: "wave"}); err == nil {
		t.Fatal("unknown op accepted")
	}
}

func TestActUnknownRef(t *testing.T) {
	_, err := act(context.Background(), &fakeInput{}, &fakeSource{name: "cdp", reads: [][]Node{nil}}, NewTable(), ActOp{Ref: "e7", Op: "click"})
	stale(t, err)
}

func TestActReResolvesAndUsesFreshCoordinates(t *testing.T) {
	fastTiming(t)
	tb := NewTable()
	tb.Assign([]Node{button("old", "Save", 1, 1)})
	src := &fakeSource{name: "cdp", reads: [][]Node{{button("new", "Save", 50, 60)}}}
	in := &fakeInput{}
	if _, err := act(context.Background(), in, src, tb, ActOp{Ref: "e1", Op: "click"}); err != nil {
		t.Fatal(err)
	}
	if in.calls[1] != "click 50,60 x1" {
		t.Fatalf("calls %v", in.calls)
	}
}

func TestActReturnsDiff(t *testing.T) {
	fastTiming(t)
	before := []Node{button("k1", "Play", 10, 10), button("k2", "Stop", 30, 10)}
	after := []Node{button("k1", "Pause", 10, 10), button("k2", "Stop", 30, 10)}
	src := &fakeSource{name: "cdp", reads: [][]Node{before, after}}
	tb := NewTable()
	tb.Assign(append([]Node(nil), before...))
	d, err := act(context.Background(), &fakeInput{}, src, tb, ActOp{Ref: "e1", Op: "click"})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Changed) != 1 || len(d.Added)+len(d.Removed) != 0 || d.Changed[0].After.Name != "Pause" {
		t.Fatalf("diff %+v", d)
	}
	if n, ok := tb.Lookup("e1"); !ok || n.Name != "Pause" {
		t.Fatalf("table not updated: %+v", n)
	}
}

func TestActSettleTimeout(t *testing.T) {
	fastTiming(t)
	settleTimeout = 150 * time.Millisecond
	src := &changing{fakeSource{name: "cdp"}}
	tb := NewTable()
	tb.Assign([]Node{{Key: "k1", Role: "button", Name: "n0", X: 10, Y: 10}})
	start := time.Now()
	d, err := act(context.Background(), &fakeInput{}, src, tb, ActOp{Ref: "e1", Op: "click"})
	el := time.Since(start)
	if err != nil {
		t.Fatalf("settle timeout is an error: %v", err)
	}
	if d.Empty() {
		t.Fatal("empty diff")
	}
	if el < settleTimeout || el > 10*settleTimeout {
		t.Fatalf("returned after %v, timeout %v", el, settleTimeout)
	}
}

// TestActWaitsForLateFirstChange: a navigation that commits ~600 ms after
// the click must still reach the diff (the default timings apply).
func TestActWaitsForLateFirstChange(t *testing.T) {
	before := []Node{{Key: "k1", Role: "link", Name: "Learn more", X: 10, Y: 10}}
	after := []Node{{Key: "k2", Role: "heading", Name: "IANA", X: 10, Y: 10}}
	// read 0 is Act's own read; the settle reads 1..5 see no change, read 6 does.
	src := &fakeSource{name: "cdp", reads: [][]Node{before, before, before, before, before, before, after}}
	tb := NewTable()
	tb.Assign(append([]Node(nil), before...))
	d, err := act(context.Background(), &fakeInput{}, src, tb, ActOp{Ref: "e1", Op: "click"})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Added) != 1 || d.Added[0].Name != "IANA" {
		t.Fatalf("diff %+v, want the heading added", d)
	}
}

func TestFindAutoReChooses(t *testing.T) {
	fastTiming(t)
	var made []*fakeSource
	choose := func(context.Context) (Source, error) {
		src := &fakeSource{name: "ocr", reads: [][]Node{{button("o1", "blank", 1, 1)}}}
		if len(made) == 2 {
			src = &fakeSource{name: "atspi", reads: [][]Node{{button("a1", "Back", 22, 48)}}}
		}
		made = append(made, src)
		return src, nil
	}
	tb := NewTable()
	got, err := FindAuto(context.Background(), choose, tb, regexp.MustCompile("Back"), "", 2*time.Second)
	if err != nil || len(got) != 1 || got[0].Key != "a1" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if len(made) != 3 || tb.SourceName() != "atspi" {
		t.Fatalf("%d sources made, table source %q", len(made), tb.SourceName())
	}
	for i, s := range made {
		if !s.closed {
			t.Errorf("source %d not closed", i)
		}
	}
}

// logSource logs each read into the same list as the input, so that a
// test sees the order of reads and input.
type logSource struct {
	fakeSource
	log *[]string
}

func (l *logSource) Nodes(ctx context.Context) ([]Node, error) {
	*l.log = append(*l.log, fmt.Sprint("read ", l.n))
	return l.fakeSource.Nodes(ctx)
}

// TestActClickSettlesPointer: Chrome drops a press that arrives with the
// pointer's first enter, so act moves, waits pointerSettle, then clicks.
func TestActClickSettlesPointer(t *testing.T) {
	fastTiming(t)
	pointerSettle = 40 * time.Millisecond
	tb := NewTable()
	tb.Assign([]Node{button("k1", "OK", 5, 6)})
	in := &fakeInput{}
	src := &fakeSource{name: "cdp", reads: [][]Node{{button("k1", "OK", 5, 6)}}}
	if _, err := act(context.Background(), in, src, tb, ActOp{Ref: "e1", Op: "click"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(in.calls, []string{"move 5,6", "click 5,6 x1"}) {
		t.Fatalf("calls %v, want move then click", in.calls)
	}
	if gap := in.at[1].Sub(in.at[0]); gap < pointerSettle {
		t.Fatalf("click %v after move, want at least %v", gap, pointerSettle)
	}
}

func TestActTypeWaitsForFocus(t *testing.T) {
	fastTiming(t)
	typeFocusWait = 2 * time.Second
	field := Node{Key: "k1", Role: "textbox", Name: "query", X: 5, Y: 6}
	focused := field
	focused.States = []string{"focused"}
	in := &fakeInput{}
	// read 0 is Act's own read, read 1 finds the centre stable, the focus polls are reads 2 and 3.
	src := &logSource{fakeSource{name: "cdp", reads: [][]Node{{field}, {field}, {field}, {focused}}}, &in.calls}
	tb := NewTable()
	tb.Assign([]Node{field})
	if _, err := act(context.Background(), in, src, tb, ActOp{Ref: "e1", Op: "type", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"read 0", "read 1", "move 5,6", "click 5,6 x1", "read 2", "read 3", "type hello"}
	if len(in.calls) < len(want) || !slices.Equal(in.calls[:len(want)], want) {
		t.Fatalf("order %v, want prefix %v", in.calls, want)
	}
}

func TestActTypeWithoutFocus(t *testing.T) {
	fastTiming(t)
	typeFocusWait = 50 * time.Millisecond
	field := Node{Key: "k1", Role: "textbox", Name: "query", X: 5, Y: 6}
	src := &fakeSource{name: "cdp", reads: [][]Node{{field}}}
	tb := NewTable()
	tb.Assign([]Node{field})
	in := &fakeInput{}
	start := time.Now()
	if _, err := act(context.Background(), in, src, tb, ActOp{Ref: "e1", Op: "type", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el < 2*typeFocusWait {
		t.Fatalf("returned after %v, before two focus waits of %v", el, typeFocusWait)
	}
	if !slices.Equal(in.calls, []string{"move 5,6", "click 5,6 x1", "move 5,6", "click 5,6 x1", "type hi"}) {
		t.Fatalf("calls %v", in.calls)
	}
}

// clickFocus reports the field as focused once the input holds `after` clicks.
type clickFocus struct {
	fakeSource
	in    *fakeInput
	after int
}

func (c *clickFocus) Nodes(context.Context) ([]Node, error) {
	field := Node{Key: "k1", Role: "textbox", Name: "query", X: 5, Y: 6}
	clicks := 0
	for _, s := range c.in.calls {
		if strings.HasPrefix(s, "click ") {
			clicks++
		}
	}
	if clicks >= c.after {
		field.States = []string{"focused"}
	}
	c.in.calls = append(c.in.calls, "read")
	return []Node{field}, nil
}

func TestActTypeClicksAgain(t *testing.T) {
	for _, after := range []int{1, 2} {
		fastTiming(t)
		typeFocusWait = 20 * time.Millisecond
		in := &fakeInput{}
		src := &clickFocus{fakeSource{name: "cdp"}, in, after}
		tb := NewTable()
		tb.Assign([]Node{{Key: "k1", Role: "textbox", Name: "query", X: 5, Y: 6}})
		if _, err := act(context.Background(), in, src, tb, ActOp{Ref: "e1", Op: "type", Text: "hello"}); err != nil {
			t.Fatal(err)
		}
		count := func(c string) int {
			return len(slices.DeleteFunc(slices.Clone(in.calls), func(s string) bool { return s != c }))
		}
		lastClick := slices.Index(in.calls, "type hello") - 2 // ..., click, read (focused), type
		if count("click 5,6 x1") != after || count("type hello") != 1 || lastClick < 0 ||
			in.calls[lastClick] != "click 5,6 x1" || in.calls[lastClick+1] != "read" {
			t.Errorf("focused after %d clicks: %v, want %d clicks, then one read, then one type", after, in.calls, after)
		}
	}
}

// TestActClickWaitsForStableCentre: right after a window maps, Chrome's
// toolbar is not laid out yet, so the first centre is stale.
func TestActClickWaitsForStableCentre(t *testing.T) {
	fastTiming(t)
	typeFocusWait = time.Second
	tb := NewTable()
	tb.Assign([]Node{button("k1", "query", 208, 36)})
	// read 0 is Act's own read; read 1 moves, read 2 agrees with read 1.
	src := &fakeSource{name: "cdp", reads: [][]Node{{button("k1", "query", 208, 36)}, {button("k1", "query", 208, 115)}}}
	in := &fakeInput{}
	if _, err := act(context.Background(), in, src, tb, ActOp{Ref: "e1", Op: "click"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(in.calls, []string{"move 208,115", "click 208,115 x1"}) {
		t.Fatalf("calls %v, want the click at the stable centre", in.calls)
	}
}

// drifting moves the button one pixel right on every read and logs the read.
type drifting struct {
	fakeSource
	log *[]string
}

func (d *drifting) Nodes(context.Context) ([]Node, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.n++
	*d.log = append(*d.log, fmt.Sprint("read ", d.n))
	return []Node{button("k1", "B", d.n, 10)}, nil
}

func TestActClickNeverStable(t *testing.T) {
	fastTiming(t)
	typeFocusWait = 20 * time.Millisecond
	tb := NewTable()
	tb.Assign([]Node{button("k1", "B", 1, 10)})
	in := &fakeInput{}
	src := &drifting{fakeSource{name: "cdp"}, &in.calls}
	if _, err := act(context.Background(), in, src, tb, ActOp{Ref: "e1", Op: "click"}); err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(in.calls, func(c string) bool { return strings.HasPrefix(c, "click ") })
	if i < 2 || in.calls[i-2] != "read "+strings.TrimSuffix(strings.Fields(in.calls[i])[1], ",10") {
		t.Fatalf("calls %v: the click must use the last read before it", in.calls)
	}
	if !strings.HasPrefix(in.calls[i-2], "read ") || in.calls[i-2] == "read 1" {
		t.Fatalf("calls %v: no stability reads", in.calls)
	}
}

func TestActOCRTypeClicksOnce(t *testing.T) {
	fastTiming(t)
	typeFocusWait = time.Second
	line := Node{Key: "ocr:1", Role: "text", Name: "query", X: 5, Y: 6}
	tb := NewTable()
	tb.Assign([]Node{line})
	in := &fakeInput{}
	start := time.Now()
	if _, err := act(context.Background(), in, &fakeSource{name: "ocr", reads: [][]Node{{line}}}, tb, ActOp{Ref: "o1", Op: "type", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el > typeFocusWait/2 {
		t.Fatalf("OCR type took %v: it must not wait for focus", el)
	}
	if !slices.Equal(in.calls, []string{"move 5,6", "click 5,6 x1", "type hi"}) {
		t.Fatalf("calls %v", in.calls)
	}
}

func TestActActionOnlyUsesPress(t *testing.T) {
	fastTiming(t)
	item := Node{Key: "k1", Role: "menuitem", Name: "Open Parent", X: 135, Y: 18, ActionOnly: true}
	src := &fakeSource{name: "atspi", reads: [][]Node{{item}}}
	tb := NewTable()
	tb.Assign([]Node{item})
	in := &fakeInput{}
	if _, err := act(context.Background(), in, src, tb, ActOp{Ref: "e1", Op: "click"}); err != nil {
		t.Fatal(err)
	}
	if len(in.calls) != 0 || !slices.Equal(src.pressed, []string{"k1"}) {
		t.Fatalf("click: input %v, pressed %v", in.calls, src.pressed)
	}
	_, err := act(context.Background(), in, src, tb, ActOp{Ref: "e1", Op: "hover"})
	var se *screen.Error
	if !errors.As(err, &se) || se.Code != screen.CodeUnsupported {
		t.Fatalf("hover: %v, want unsupported", err)
	}
	if len(in.calls) != 0 || len(src.pressed) != 1 {
		t.Fatalf("hover: input %v, pressed %v", in.calls, src.pressed)
	}
}

func TestActOffscreenPressFallback(t *testing.T) {
	fastTiming(t)
	off := Node{Key: "k1", Role: "button", Name: "Hidden", X: -1, Y: -1, Offscreen: true}
	src := &fakeSource{name: "atspi", reads: [][]Node{{off}}, reveal: func(string) (Node, error) { return off, nil }}
	tb := NewTable()
	tb.Assign([]Node{off})
	in := &fakeInput{}
	if _, err := act(context.Background(), in, src, tb, ActOp{Ref: "e1", Op: "click"}); err != nil {
		t.Fatal(err)
	}
	if len(in.calls) != 0 || len(src.pressed) != 1 || src.pressed[0] != "k1" {
		t.Fatalf("input %v, pressed %v", in.calls, src.pressed)
	}
}

func TestActOffscreenRevealed(t *testing.T) {
	fastTiming(t)
	off := Node{Key: "k1", Role: "button", Name: "B", Offscreen: true}
	src := &fakeSource{name: "cdp", reads: [][]Node{{off}}, reveal: func(string) (Node, error) { return button("k1", "B", 7, 8), nil }}
	tb := NewTable()
	tb.Assign([]Node{off})
	in := &fakeInput{}
	if _, err := act(context.Background(), in, src, tb, ActOp{Ref: "e1", Op: "click"}); err != nil {
		t.Fatal(err)
	}
	if in.calls[1] != "click 7,8 x1" {
		t.Fatalf("calls %v", in.calls)
	}
}

func TestActOffscreenError(t *testing.T) {
	fastTiming(t)
	off := Node{Key: "k1", Role: "button", Name: "B", Offscreen: true}
	src := &fakeSource{name: "cdp", reads: [][]Node{{off}}, reveal: func(string) (Node, error) { return off, nil }}
	tb := NewTable()
	tb.Assign([]Node{off})
	_, err := act(context.Background(), &fakeInput{}, src, tb, ActOp{Ref: "e1", Op: "click"})
	var se *screen.Error
	if !errors.As(err, &se) || se.Code != screen.CodeRefOffscreen {
		t.Fatalf("got %v, want ref_offscreen", err)
	}
}

func TestFindWaits(t *testing.T) {
	fastTiming(t)
	findPoll = 20 * time.Millisecond
	src := &fakeSource{name: "cdp", reads: [][]Node{
		{button("k1", "Cancel", 1, 1)},
		{button("k1", "Cancel", 1, 1)},
		{button("k1", "Cancel", 1, 1), button("k2", "Submit order", 2, 2)},
	}}
	got, err := Find(context.Background(), src, NewTable(), regexp.MustCompile("(?i)submit"), "", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "k2" || got[0].Ref == "" {
		t.Fatalf("got %+v", got)
	}
}

func TestFindKeepsRefsAndFiltersRole(t *testing.T) {
	nodes := []Node{button("k1", "Save", 1, 1), {Key: "k2", Role: "link", Name: "Save as"}}
	src := &fakeSource{name: "cdp", reads: [][]Node{nodes}}
	tb := NewTable()
	tb.Assign(append([]Node(nil), nodes...))
	got, err := Find(context.Background(), src, tb, regexp.MustCompile("Save"), "link", 0)
	if err != nil || len(got) != 1 || got[0].Ref != "e2" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, ok := tb.Lookup("e1"); !ok {
		t.Fatal("Find wiped the ref of a non-matching node")
	}
}

func TestFindTimeout(t *testing.T) {
	fastTiming(t)
	src := &fakeSource{name: "cdp", reads: [][]Node{{button("k1", "A", 1, 1)}}}
	got, err := Find(context.Background(), src, NewTable(), regexp.MustCompile("Z"), "", 30*time.Millisecond)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Find(ctx, src, NewTable(), regexp.MustCompile("Z"), "", time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

// stubChoose replaces the probes of Choose for the test.
func stubChoose(t *testing.T, apps bool, ocrPresent bool) {
	t.Helper()
	h, d, a := hasApps, ocrData, newATSPI
	hasApps = func(context.Context, string) bool { return apps }
	newATSPI = func(context.Context, string) (Source, error) { return &fakeSource{name: "atspi"}, nil }
	ocrData = filepath.Join(t.TempDir(), "eng.traineddata")
	if ocrPresent {
		if err := os.WriteFile(ocrData, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { hasApps, ocrData, newATSPI = h, d, a })
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestChooseFallsBackWhenCDPDown(t *testing.T) {
	stubChoose(t, false, true)
	src, err := Choose(context.Background(), &registry.Screen{Name: "s", DebugPort: freePort(t)}, (*wl.Client)(nil), "auto")
	if err != nil {
		t.Fatal(err)
	}
	if src.Name() != "ocr" {
		t.Fatalf("source %s", src.Name())
	}
}

func TestChoosePrefersATSPI(t *testing.T) {
	stubChoose(t, true, true)
	src, err := Choose(context.Background(), &registry.Screen{Name: "s"}, nil, "auto")
	if err != nil || src.Name() != "atspi" {
		t.Fatalf("got %v, %v", src, err)
	}
}

func TestChooseNoSource(t *testing.T) {
	stubChoose(t, false, false)
	for _, want := range []string{"auto", "cdp", "ocr"} {
		_, err := Choose(context.Background(), &registry.Screen{Name: "s"}, nil, want)
		var se *screen.Error
		if !errors.As(err, &se) || se.Code != screen.CodeNoSource {
			t.Fatalf("%s: got %v, want no_source", want, err)
		}
	}
	if _, err := Choose(context.Background(), &registry.Screen{Name: "s"}, nil, "vision"); err == nil {
		t.Fatal("unknown source accepted")
	}
}

func TestOCRKeepsTextNodes(t *testing.T) {
	nodes := []Node{{Key: "ocr:0", Role: "text", Name: "File saved", X: 3, Y: 4}}
	src := &fakeSource{name: "ocr", reads: [][]Node{nodes}}
	tb := NewTable()
	out, got, err := Snapshot(context.Background(), src, tb, SnapOpts{Mode: ModeInteractive})
	if err != nil || len(got) != 1 || !strings.Contains(out, `[o1] text "File saved"`) {
		t.Fatalf("snapshot %q, %v", out, err)
	}
	found, err := Find(context.Background(), src, tb, regexp.MustCompile("saved"), "", 0)
	if err != nil || len(found) != 1 || found[0].Ref != "o1" {
		t.Fatalf("find %+v, %v", found, err)
	}
}

func TestActOCRLayoutShiftReResolves(t *testing.T) {
	fastTiming(t)
	old := Node{Key: "ocr:3", Role: "text", Name: "Save", X: 10, Y: 10}
	tb := NewTable()
	tb.Assign([]Node{old})
	src := &fakeSource{name: "ocr", reads: [][]Node{{
		{Key: "ocr:3", Role: "text", Name: "Toast text", X: 10, Y: 10},
		{Key: "ocr:4", Role: "text", Name: "Save", X: 10, Y: 40},
	}}}
	in := &fakeInput{}
	if _, err := act(context.Background(), in, src, tb, ActOp{Ref: "o1", Op: "click"}); err != nil {
		t.Fatal(err)
	}
	if len(in.calls) != 2 || in.calls[1] != "click 10,40 x1" {
		t.Fatalf("calls %v", in.calls)
	}

	tb = NewTable()
	tb.Assign([]Node{old})
	src = &fakeSource{name: "ocr", reads: [][]Node{{
		{Key: "ocr:3", Role: "text", Name: "Toast text", X: 10, Y: 10},
		{Key: "ocr:4", Role: "text", Name: "Save", X: 10, Y: 40},
		{Key: "ocr:5", Role: "text", Name: "Save", X: 10, Y: 70},
	}}}
	in = &fakeInput{}
	_, err := act(context.Background(), in, src, tb, ActOp{Ref: "o1", Op: "click"})
	stale(t, err)
	if len(in.calls) != 0 {
		t.Fatalf("input sent: %v", in.calls)
	}
}

func TestActCDPKeyIsIdentity(t *testing.T) {
	fastTiming(t)
	tb := NewTable()
	tb.Assign([]Node{button("k1", "Play", 1, 1)})
	in := &fakeInput{}
	src := &fakeSource{name: "cdp", reads: [][]Node{{button("k1", "Pause", 2, 2), button("k2", "Play", 3, 3)}}}
	if _, err := act(context.Background(), in, src, tb, ActOp{Ref: "e1", Op: "click"}); err != nil || in.calls[1] != "click 2,2 x1" {
		t.Fatalf("calls %v, %v", in.calls, err)
	}
}

func TestSnapshotVanishedRoot(t *testing.T) {
	first := []Node{{Key: "root", Role: "main", Name: "m"}, {Key: "b", Parent: "root", Role: "button", Name: "B"}}
	src := &fakeSource{name: "cdp", reads: [][]Node{first, {{Key: "b", Role: "button", Name: "B"}}}}
	tb := NewTable()
	if _, _, err := Snapshot(context.Background(), src, tb, SnapOpts{}); err != nil {
		t.Fatal(err)
	}
	_, _, err := Snapshot(context.Background(), src, tb, SnapOpts{RootRef: "e1"})
	stale(t, err)
	if _, ok := tb.Lookup("e2"); !ok {
		t.Fatal("refs wiped by the failed snapshot")
	}
}

func TestActUnknownOpReadsNothing(t *testing.T) {
	src := &fakeSource{name: "cdp", reads: [][]Node{{button("k1", "OK", 1, 1)}}}
	tb := NewTable()
	tb.Assign([]Node{button("k1", "OK", 1, 1)})
	in := &fakeInput{}
	if _, err := act(context.Background(), in, src, tb, ActOp{Ref: "e1", Op: "wave"}); err == nil {
		t.Fatal("unknown op accepted")
	}
	if src.n != 0 || len(in.calls) != 0 {
		t.Fatalf("reads %d, input %v", src.n, in.calls)
	}
}

func TestActKeySkipsResolve(t *testing.T) {
	fastTiming(t)
	// The node is gone and would be off screen: key must not care.
	tb := NewTable()
	tb.Assign([]Node{{Key: "gone", Role: "textbox", Offscreen: true}})
	src := &fakeSource{name: "cdp", reads: [][]Node{{button("k1", "OK", 1, 1)}}}
	in := &fakeInput{}
	if _, err := act(context.Background(), in, src, tb, ActOp{Ref: "e1", Op: "key", Keys: []string{"Return"}}); err != nil {
		t.Fatal(err)
	}
	if len(in.calls) != 1 || in.calls[0] != "keys Return" {
		t.Fatalf("calls %v", in.calls)
	}
	_, err := act(context.Background(), in, src, NewTable(), ActOp{Ref: "e9", Op: "key", Keys: []string{"Return"}})
	stale(t, err)
}

func TestChooseReportsCDPError(t *testing.T) {
	stubChoose(t, false, false)
	c := newCDP
	newCDP = func(context.Context, int) (Source, error) { return nil, errors.New("fuse blew the port") }
	t.Cleanup(func() { newCDP = c })
	_, err := Choose(context.Background(), &registry.Screen{Name: "s", DebugPort: 9}, nil, "auto")
	var se *screen.Error
	if !errors.As(err, &se) || se.Code != screen.CodeNoSource || !strings.Contains(se.Msg, "fuse blew the port") {
		t.Fatalf("got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	newCDP = func(context.Context, int) (Source, error) { cancel(); return nil, errors.New("cancelled") }
	stubChoose(t, true, true)
	if _, err := Choose(ctx, &registry.Screen{Name: "s", DebugPort: 9}, nil, "auto"); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestSnapshotRecordsSource(t *testing.T) {
	tb := NewTable()
	src := &fakeSource{name: "ocr", reads: [][]Node{{{Key: "ocr:1", Role: "text", Name: "Hi"}}}}
	if _, _, err := Snapshot(context.Background(), src, tb, SnapOpts{}); err != nil {
		t.Fatal(err)
	}
	if got := tb.SourceName(); got != "ocr" {
		t.Errorf("act would choose %q, want ocr", got)
	}
}

func TestActDiffRemovedKeepsRef(t *testing.T) {
	fastTiming(t)
	before := []Node{button("k1", "Continue", 10, 10), button("k2", "Stop", 30, 10)}
	after := []Node{button("k2", "Stop", 30, 10)}
	src := &fakeSource{name: "cdp", reads: [][]Node{before, after}}
	tb := NewTable()
	tb.Assign(append([]Node(nil), before...))
	d, err := act(context.Background(), &fakeInput{}, src, tb, ActOp{Ref: "e1", Op: "click"})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.String(); !strings.HasPrefix(got, `- [e1] button "Continue"`) {
		t.Errorf("diff %q", got)
	}
}

func TestValidOp(t *testing.T) {
	if !ValidOp("click") || ValidOp("wave") {
		t.Error("ValidOp")
	}
}

func TestFindAutoWaitsForSource(t *testing.T) {
	fastTiming(t)
	calls := 0
	choose := func(context.Context) (Source, error) {
		calls++
		if calls <= 2 {
			return nil, screen.Errf(screen.CodeNoSource, "", "nothing on the screen yet")
		}
		return &fakeSource{name: "atspi", reads: [][]Node{{button("a1", "Back", 22, 48)}}}, nil
	}
	got, err := FindAuto(context.Background(), choose, NewTable(), regexp.MustCompile("Back"), "", 2*time.Second)
	if err != nil || len(got) != 1 || got[0].Key != "a1" || calls != 3 {
		t.Fatalf("got %+v, %v after %d chooses", got, err, calls)
	}

	other := errors.New("boom")
	_, err = FindAuto(context.Background(), func(context.Context) (Source, error) { return nil, other }, NewTable(), regexp.MustCompile("Back"), "", 2*time.Second)
	if !errors.Is(err, other) {
		t.Fatalf("err = %v, want boom at once", err)
	}
}

func TestFindAutoTimeoutKeepsSource(t *testing.T) {
	fastTiming(t)
	choose := func(context.Context) (Source, error) {
		return &fakeSource{name: "ocr", reads: [][]Node{{button("o1", "blank", 1, 1)}}}, nil
	}
	tb := NewTable()
	got, err := FindAuto(context.Background(), choose, tb, regexp.MustCompile("Back"), "", 20*time.Millisecond)
	if err != nil || len(got) != 0 || tb.SourceName() != "auto" {
		t.Fatalf("got %+v, %v, source %q", got, err, tb.SourceName())
	}
}
