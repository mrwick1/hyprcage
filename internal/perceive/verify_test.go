package perceive

import (
	"context"
	"github.com/hexadecimil/hyprcage/internal/hypr"
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func testFrame(fill func(*image.RGBA)) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 800))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{250, 250, 250, 255}), image.Point{}, draw.Src)
	if fill != nil {
		fill(img)
	}
	return img
}

func box(r image.Rectangle) func(*image.RGBA) {
	return func(img *image.RGBA) {
		draw.Draw(img, r, image.NewUniform(color.RGBA{20, 20, 200, 255}), image.Point{}, draw.Src)
	}
}

func TestJudge(t *testing.T) {
	button := image.Rect(100, 100, 200, 130)
	cases := []struct {
		name   string
		after  func(*image.RGBA)
		points []image.Point
		want   Verdict
	}{
		{"identical frames", nil, []image.Point{{150, 115}}, NoOp},
		{"caret blink only", box(image.Rect(300, 300, 301, 320)), []image.Point{{150, 115}}, NoOp},
		{"button changes under the click", box(button), []image.Point{{150, 115}}, Confirmed},
		{"change far from the click", box(image.Rect(900, 600, 1100, 700)), []image.Point{{150, 115}}, Diverged},
		{"change next to the click", box(image.Rect(100, 150, 300, 300)), []image.Point{{150, 115}}, Partial},
		{"key with no target and a change", box(image.Rect(900, 600, 1100, 700)), nil, Confirmed},
		{"full change", box(image.Rect(0, 0, 1280, 800)), []image.Point{{150, 115}}, Confirmed},
		{"drag where only the start changed", box(button), []image.Point{{150, 115}, {800, 500}}, Partial},
		{"drag where both ends changed", box(image.Rect(100, 100, 900, 600)), []image.Point{{150, 115}, {800, 500}}, Confirmed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NewVerification(Compare(testFrame(nil), testFrame(c.after)), c.points...)
			if got.Verdict != c.want {
				t.Fatalf("verdict %s (diff %+v), want %s", got.Verdict, got.Diff, c.want)
			}
			if got.Sense.NoOp != (c.want == NoOp) || got.Sense.Changed != (c.want != NoOp) {
				t.Fatalf("sense %+v does not follow verdict %s", got.Sense, c.want)
			}
		})
	}
}

func TestVerificationLine(t *testing.T) {
	v := NewVerification(Compare(testFrame(nil), testFrame(box(image.Rect(120, 80, 760, 380)))), image.Pt(200, 200))
	want := "verify: CONFIRMED (changed 18.8%, bbox 120,80 640x300)"
	if got := v.String(); got != want {
		t.Fatalf("line %q, want %q", got, want)
	}
	if got := NewVerification(FrameDiff{}).String(); got != "verify: NO_OP (changed 0.0%)" {
		t.Fatalf("line %q", got)
	}
}

func TestHasDialog(t *testing.T) {
	app := accessible{Bus: ":1", Path: "/app", Role: "application", PID: 7}
	top := func(path, role, name string, pid int) accessible {
		return accessible{Bus: ":1", Path: path, Parent: "/app", Role: role, Name: name, PID: pid}
	}
	tests := []struct {
		name string
		objs []accessible
		win  hypr.Client
		want bool
	}{
		{"dialog role", []accessible{app, top("/a", "frame", "Main", 7), top("/b", "dialog", "About", 7)}, hypr.Client{PID: 7, Title: "About"}, true},
		{"alert role", []accessible{app, top("/b", "alert", "Quit?", 7)}, hypr.Client{PID: 7, Title: "Quit?"}, true},
		{"file chooser", []accessible{app, top("/b", "file chooser", "Open", 7)}, hypr.Client{PID: 7, Title: "Open"}, true},
		{"frame is not a dialog", []accessible{app, top("/a", "frame", "Main", 7), top("/b", "dialog", "About", 7)}, hypr.Client{PID: 7, Title: "Main"}, false},
		{"no title match stays false", []accessible{app, top("/b", "dialog", "About", 7)}, hypr.Client{PID: 7, Title: "Other"}, false},
		{"another pid", []accessible{app, top("/b", "dialog", "About", 8)}, hypr.Client{PID: 7, Title: "About"}, false},
		{"empty tree", nil, hypr.Client{PID: 7, Title: "About"}, false},
	}
	for _, tc := range tests {
		if got := hasDialog(tc.objs, tc.win); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAnyModalSkipsWithoutBus(t *testing.T) {
	tr := &fakeTree{objs: []accessible{{Bus: ":1", Path: "/b", Parent: "/app", Role: "dialog", Name: "About", PID: 7}, {Bus: ":1", Path: "/app", Role: "application", PID: 7}}}
	if !anyModal(context.Background(), tr, []hypr.Client{{PID: 7, Title: "About"}}) {
		t.Fatal("a new dialog window must be modal")
	}
	if anyModal(context.Background(), tr, nil) {
		t.Fatal("no new window is not modal")
	}
}
