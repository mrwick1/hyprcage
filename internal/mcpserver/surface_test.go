package mcpserver

import (
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// fakeSurface is a screen.Surface that records the calls it gets.
type fakeSurface struct{ calls []string }

func (f *fakeSurface) rec(format string, a ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, a...))
}

func (f *fakeSurface) Move(x, y int) error { f.rec("move %d %d", x, y); return nil }
func (f *fakeSurface) PressButton(b wl.Button, down bool) error {
	f.rec("button %v", down)
	return nil
}
func (f *fakeSurface) Scroll(wl.Axis, int) error         { return nil }
func (f *fakeSurface) HoldModifiers([]string) error      { return nil }
func (f *fakeSurface) Type(text string) error            { f.rec("type %s", text); return nil }
func (f *fakeSurface) Key(combo string) error            { f.rec("key %s", combo); return nil }
func (f *fakeSurface) Toplevels() ([]wl.Toplevel, error) { return nil, nil }
func (f *fakeSurface) CloseToplevel(uint32) error        { return nil }
func (f *fakeSurface) Capture(bool) (*image.RGBA, error) {
	f.rec("capture")
	return image.NewRGBA(image.Rect(0, 0, 4, 4)), nil
}

var _ screen.Surface = (*fakeSurface)(nil)

func TestRunActionOnSurface(t *testing.T) {
	f := &fakeSurface{}
	s := &Server{}
	for _, a := range []batchAction{
		{Op: "click", X: 3, Y: 2},
		{Op: "type", Text: "hi"},
		{Op: "key", Keys: []string{"ctrl+a", "Return"}},
	} {
		if err := s.runAction(f, a); err != nil {
			t.Fatalf("%s: %v", a.Op, err)
		}
	}
	want := "move 3 2,button true,button false,type hi,key ctrl+a,key Return"
	if got := strings.Join(f.calls, ","); got != want {
		t.Errorf("calls = %s, want %s", got, want)
	}
}

func TestShotOnSurface(t *testing.T) {
	f := &fakeSurface{}
	res, err := screen.Shot(f, screen.ShotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.ScreenW != 4 || res.ScreenH != 4 || len(f.calls) != 1 || f.calls[0] != "capture" {
		t.Errorf("shot %dx%d, calls %v", res.ScreenW, res.ScreenH, f.calls)
	}
}
