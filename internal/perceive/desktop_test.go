package perceive

import (
	"context"
	"os"
	"testing"

	"github.com/hexadecimil/hyprcage/internal/hypr"
)

func TestATSPIDesktopOffset(t *testing.T) {
	app := obj("/app", "application", "thunar", [4]int{})
	win := obj("/win", "frame", "Home", [4]int{0, 0, 500, 400})
	win.Parent = "/app"
	btn := obj("/btn", "push button", "Back", [4]int{5, 15, 10, 10}) // centre (10,20) in the window
	btn.Parent = "/win"
	wins := []hypr.Client{{Address: "0xa", PID: 1, Title: "Home", At: [2]int{100, 200}, Size: [2]int{500, 400}}}
	nodes, err := newDesktopATSPIWith(&fakeTree{objs: []accessible{app, win, btn}}, wins).Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.Name == "Back" {
			if n.X != 110 || n.Y != 220 {
				t.Fatalf("Back at (%d,%d), want (110,220)", n.X, n.Y)
			}
			return
		}
	}
	t.Fatalf("no Back node in %+v", nodes)
}

func TestATSPIDesktopFiltersPIDs(t *testing.T) {
	mine := obj("/a", "push button", "Mine", [4]int{0, 0, 10, 10})
	other := obj("/b", "push button", "Other", [4]int{0, 0, 10, 10})
	other.Bus, other.PID = ":1.9", 2
	wins := []hypr.Client{{Address: "0xa", PID: 1}}
	nodes, err := newDesktopATSPIWith(&fakeTree{objs: []accessible{mine, other}}, wins).Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Name != "Mine" {
		t.Fatalf("got %+v, want only Mine", nodes)
	}
}

func TestChooseDesktopOrder(t *testing.T) {
	stubChoose(t, false, true)
	p, a, c, n := devToolsPort, pidsHaveApps, newCDP, newDesktopATSPI
	t.Cleanup(func() { devToolsPort, pidsHaveApps, newCDP, newDesktopATSPI = p, a, c, n })
	newCDP = func(context.Context, int) (Source, error) { return &fakeSource{name: "cdp"}, nil }
	newDesktopATSPI = func(context.Context, []hypr.Client) (Source, error) { return &fakeSource{name: "atspi"}, nil }
	target := Target{Windows: []hypr.Client{{Address: "0xa", PID: 7}}}
	for _, c := range []struct {
		port int
		apps bool
		want string
	}{{9222, true, "cdp"}, {0, true, "atspi"}, {0, false, "ocr"}} {
		devToolsPort = func(int) int { return c.port }
		pidsHaveApps = func(context.Context, []int) bool { return c.apps }
		src, err := ChooseDesktop(context.Background(), nil, target, "auto")
		if err != nil || src.Name() != c.want {
			t.Fatalf("port=%d apps=%v: got %v, %v, want %s", c.port, c.apps, src, err, c.want)
		}
	}
	if err := os.Remove(ocrData); err != nil {
		t.Fatal(err)
	}
	if _, err := ChooseDesktop(context.Background(), nil, target, "auto"); err == nil {
		t.Fatal("no source accepted")
	}
}
