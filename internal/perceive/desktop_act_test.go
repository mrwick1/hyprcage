package perceive

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/hexadecimil/hyprcage/internal/hypr"
)

// oneWindow is the desktop of the desktop act tests: one window at 0,0.
var oneWindow = []hypr.Client{{Address: "0xa", PID: 1, Size: [2]int{1280, 720}}}

// desktopRun snapshots src into a new table, then acts on the node named
// name. It returns the act's text and the fallback input with its window.
func desktopRun(t *testing.T, src Source, name string, op ActOp) (string, *fakeInput, []string) {
	t.Helper()
	fastTiming(t)
	tb := NewTable()
	_, nodes, err := Snapshot(context.Background(), src, tb, SnapOpts{Mode: ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.Name == name && op.Ref == "" {
			op.Ref = n.Ref
		}
	}
	in := &fakeInput{}
	var addrs []string
	out, err := desktopAct(context.Background(), func(addr string) inputter { addrs = append(addrs, addr); return in }, oneWindow, src, tb, op)
	if err != nil {
		t.Fatal(err)
	}
	return out, in, addrs
}

func TestDesktopActClickPrefersPress(t *testing.T) {
	f := &fakeTree{objs: []accessible{obj("/b", "push button", "Home", [4]int{10, 10, 20, 20}, "enabled")}}
	out, in, _ := desktopRun(t, newDesktopATSPIWith(f, oneWindow, false), "Home", ActOp{Op: "click"})
	if !slices.Equal(f.actions, []string{":1.5/b"}) {
		t.Fatalf("DoAction calls %v", f.actions)
	}
	if len(in.calls) != 0 {
		t.Fatalf("pointer input %v", in.calls)
	}
	if !strings.HasPrefix(out, "path=atspi\n") {
		t.Fatalf("out %q", out)
	}
}

// A menu title of a menu bar opens only by the pointer; an item of the open
// popup still takes DoAction.
func TestDesktopActMenuTitleUsesPointer(t *testing.T) {
	bar := obj("/bar", "menu bar", "", [4]int{0, 0, 400, 20}, "enabled")
	help := obj("/help", "menu", "Help", [4]int{100, 0, 40, 20}, "enabled")
	help.Parent = "/bar"
	about := obj("/about", "menu item", "About", [4]int{100, 20, 80, 20}, "enabled")
	about.Parent = "/help"
	f := &fakeTree{objs: []accessible{bar, help, about}}
	src := newDesktopATSPIWith(f, oneWindow, false)
	out, in, _ := desktopRun(t, src, "Help", ActOp{Op: "click"})
	if len(f.actions) != 0 || len(in.calls) == 0 || !strings.HasPrefix(out, "path=pointer\n") {
		t.Fatalf("Help: DoAction %v, pointer %v, out %q; want the pointer", f.actions, in.calls, out)
	}
	out, in, _ = desktopRun(t, src, "About", ActOp{Op: "click"})
	if !slices.Equal(f.actions, []string{":1.5/about"}) || len(in.calls) != 0 || !strings.HasPrefix(out, "path=atspi\n") {
		t.Fatalf("About: DoAction %v, pointer %v, out %q; want DoAction", f.actions, in.calls, out)
	}
}

func TestDesktopActTypeInserts(t *testing.T) {
	f := &fakeTree{objs: []accessible{obj("/e", "entry", "Location", [4]int{10, 10, 200, 20}, "enabled", "editable")}}
	out, in, _ := desktopRun(t, newDesktopATSPIWith(f, oneWindow, false), "Location", ActOp{Op: "type", Text: "/tmp"})
	if !slices.Equal(f.inserts, []string{":1.5/e /tmp"}) {
		t.Fatalf("InsertText calls %v", f.inserts)
	}
	if len(in.calls) != 0 {
		t.Fatalf("input %v", in.calls)
	}
	if !strings.HasPrefix(out, "path=atspi\n") {
		t.Fatalf("out %q", out)
	}
}

func TestDesktopActFallsBackToPointer(t *testing.T) {
	src := &fakeSource{name: "ocr", reads: [][]Node{{{Key: "ocr:0", Role: "text", Name: "Go", X: 40, Y: 12}}}}
	out, in, addrs := desktopRun(t, src, "Go", ActOp{Op: "click"})
	if !slices.Equal(in.calls, []string{"click 40,12 x1"}) || !slices.Equal(addrs, []string{"0xa"}) {
		t.Fatalf("input %v in %v", in.calls, addrs)
	}
	if len(src.pressed) != 0 {
		t.Fatalf("pressed %v", src.pressed)
	}
	if !strings.HasPrefix(out, "path=pointer\n") {
		t.Fatalf("out %q", out)
	}
}

func TestDesktopActPathLine(t *testing.T) {
	src := &fakeSource{name: "ocr", reads: [][]Node{{{Key: "ocr:0", Role: "text", Name: "Go", X: 40, Y: 12}}}}
	out, in, _ := desktopRun(t, src, "Go", ActOp{Op: "key", Keys: []string{"Escape"}})
	if !slices.Equal(in.calls, []string{"keys Escape"}) {
		t.Fatalf("input %v", in.calls)
	}
	if first, _, _ := strings.Cut(out, "\n"); first != "path=shortcut" {
		t.Fatalf("first line %q", first)
	}
}

func TestDesktopActWindowOfNode(t *testing.T) {
	wins := []hypr.Client{{Address: "0xa", Size: [2]int{100, 100}}, {Address: "0xb", At: [2]int{100, 0}, Size: [2]int{100, 100}}}
	for _, c := range []struct {
		n    Node
		op   string
		want string
	}{
		{Node{X: 150, Y: 50}, "click", "0xb"},
		{Node{X: 50, Y: 50}, "click", "0xa"},
		{Node{X: 500, Y: 50}, "click", ""},
		{Node{}, "key", ""},
	} {
		got, err := windowOf(wins, c.n, c.op)
		if got != c.want || (err == nil) != (c.want != "") {
			t.Errorf("%+v %s: %q %v, want %q", c.n, c.op, got, err, c.want)
		}
	}
}

func TestDesktopActCDPClick(t *testing.T) {
	f := newFake(t)
	src := shifted{newCDPWith(f, cdpTargetTypes), [2]int{100, 50}}
	nodes, err := src.Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	name := ""
	for _, n := range nodes {
		if n.Role == "button" && n.Name == "Continue without Signing In" {
			name = n.Name
		}
	}
	out, in, _ := desktopRun(t, src, name, ActOp{Op: "click"})
	if len(in.calls) != 0 || !strings.HasPrefix(out, "path=cdp\n") {
		t.Fatalf("input %v out %q", in.calls, out)
	}
	if !slices.Equal(f.input, []string{"mouseMoved", "mousePressed", "mouseReleased"}) {
		t.Fatalf("mouse events %v", f.input)
	}
}
