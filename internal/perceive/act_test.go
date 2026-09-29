package perceive

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
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

func (f *fakeSource) Close() error { return nil }

// changing returns a new button name on every read.
type changing struct{ fakeSource }

func (c *changing) Nodes(context.Context) ([]Node, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return []Node{{Key: "k1", Role: "button", Name: fmt.Sprint("n", c.n), X: 10, Y: 10}}, nil
}

// fakeInput records the input that act sends.
type fakeInput struct{ calls []string }

func (f *fakeInput) click(x, y, count int) error {
	f.calls = append(f.calls, fmt.Sprintf("click %d,%d x%d", x, y, count))
	return nil
}
func (f *fakeInput) move(x, y int) error {
	f.calls = append(f.calls, fmt.Sprintf("move %d,%d", x, y))
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
	p, q, to, fp := settlePoll, settleQuiet, settleTimeout, findPoll
	settlePoll, settleQuiet, settleTimeout, findPoll = time.Millisecond, 5*time.Millisecond, 200*time.Millisecond, time.Millisecond
	t.Cleanup(func() { settlePoll, settleQuiet, settleTimeout, findPoll = p, q, to, fp })
}

func button(key, name string, x, y int) Node {
	return Node{Key: key, Role: "button", Name: name, X: x, Y: y}
}

func TestTimingDefaults(t *testing.T) {
	if settlePoll != 100*time.Millisecond || settleQuiet != 300*time.Millisecond ||
		settleTimeout != 3*time.Second || findPoll != 250*time.Millisecond {
		t.Fatalf("timings %v %v %v %v", settlePoll, settleQuiet, settleTimeout, findPoll)
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
	if len(in.calls) != 1 || in.calls[0] != "click 120,45 x1" {
		t.Fatalf("calls %v", in.calls)
	}
}

func TestActOps(t *testing.T) {
	fastTiming(t)
	for _, c := range []struct {
		op   ActOp
		want string
	}{
		{ActOp{Op: "double_click"}, "click 5,6 x2"},
		{ActOp{Op: "type", Text: "hi"}, "click 5,6 x1|type hi"},
		{ActOp{Op: "key", Keys: []string{"ctrl+a", "Delete"}}, "keys ctrl+a+Delete"},
		{ActOp{Op: "hover"}, "move 5,6"},
		{ActOp{Op: "scroll"}, "scroll 5,6 down 3"},
		{ActOp{Op: "scroll", Direction: "up"}, "scroll 5,6 up 3"},
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
	if in.calls[0] != "click 50,60 x1" {
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
	if in.calls[0] != "click 7,8 x1" {
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
