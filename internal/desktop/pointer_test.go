package desktop

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// fakeWL records pointer input in log. Move number failMove fails (0: none).
type fakeWL struct {
	mu       *sync.Mutex
	log      *[]string
	moves    int
	failMove int
}

func (f *fakeWL) add(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	*f.log = append(*f.log, s)
}

func (f *fakeWL) MoveIn(x, y, w, h int) error {
	f.moves++
	if f.moves == f.failMove {
		return errors.New("move failed")
	}
	f.add(fmt.Sprintf("move %d,%d in %dx%d", x, y, w, h))
	return nil
}

func (f *fakeWL) PressButton(b wl.Button, pressed bool) error {
	f.add(fmt.Sprintf("button %d %v", b, pressed))
	return nil
}

func (f *fakeWL) Scroll(a wl.Axis, steps int) error {
	f.add(fmt.Sprintf("scroll %d %d", a, steps))
	return nil
}

func (f *fakeWL) HoldModifiers(mods []string) error {
	f.add(fmt.Sprintf("mods %v", mods))
	return nil
}

const oneMonitor = `[{"width":1920,"height":1080,"x":0,"y":0,"scale":1.5,"activeWorkspace":{"id":1}}]`

// fakePointer builds a Pointer on a fake IPC with the given monitors, the
// window 0xa on workspace 1, 0xb on workspace 7 and the cursor at 11,22. The
// log holds the pointer input and the IPC commands in order.
func fakePointer(t *testing.T, locked bool, monitors string) (Pointer, *fakeWL, *[]string) {
	old := lockedFn
	lockedFn = func() bool { return locked }
	t.Cleanup(func() { lockedFn = old })
	var mu sync.Mutex
	log := []string{}
	h, _ := fakeIPCFunc(t, func(req string) string {
		switch req {
		case "j/monitors":
			return monitors
		case "j/clients":
			return `[{"address":"0xa","workspace":{"id":1}},{"address":"0xb","workspace":{"id":7}}]`
		case "j/cursorpos":
			return `{"x":11,"y":22}`
		}
		mu.Lock()
		log = append(log, req)
		mu.Unlock()
		return "ok"
	})
	in := &fakeWL{mu: &mu, log: &log}
	return Pointer{C: &Conn{H: h}, D: &fakeDriver{}, in: in}, in, &log
}

func TestPointerRestoresOnError(t *testing.T) {
	p, _, log := fakePointer(t, false, oneMonitor)
	boom := errors.New("boom")
	err := p.Do("0xa", func(in screen.PointerInput) error {
		if err := in.Move(100, 100); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Do = %v, want fn's error", err)
	}
	if last := (*log)[len(*log)-1]; last != "dispatch movecursor 11 22" {
		t.Errorf("log %q, want the restore to 11,22 last", *log)
	}
}

func TestPointerRestoresOnPanic(t *testing.T) {
	p, _, log := fakePointer(t, false, oneMonitor)
	func() {
		defer func() { _ = recover() }()
		_ = p.Do("", func(in screen.PointerInput) error {
			_ = in.PressButton(wl.ButtonLeft, true)
			panic("boom")
		})
	}()
	want := []string{fmt.Sprintf("button %d false", wl.ButtonLeft), "dispatch movecursor 11 22"}
	if !slices.Equal((*log)[len(*log)-2:], want) {
		t.Errorf("log %q, want %q at the end", *log, want)
	}
}

func TestDragReleasesOnError(t *testing.T) {
	p, in, log := fakePointer(t, false, oneMonitor)
	in.failMove = 3 // the press is after move 1; move 3 is mid-drag
	err := p.Do("0xa", func(in screen.PointerInput) error {
		return screen.Drag(in, 10, 10, 200, 200, 1)
	})
	if err == nil {
		t.Fatal("Do: want the move error")
	}
	release := slices.Index(*log, fmt.Sprintf("button %d false", wl.ButtonLeft))
	restore := slices.Index(*log, "dispatch movecursor 11 22")
	if release < 0 || restore < 0 || release > restore {
		t.Errorf("log %q, want a release before the restore", *log)
	}
}

func TestPointerRefusesHidden(t *testing.T) {
	p, _, log := fakePointer(t, false, oneMonitor)
	called := false
	err := p.Do("0xb", func(screen.PointerInput) error { called = true; return nil })
	wantCode(t, "hidden", err, screen.CodeWindowHidden)
	if se := (*screen.Error)(nil); errors.As(err, &se) && !strings.Contains(se.Hint, "desktop_workspace") {
		t.Errorf("hint %q, want desktop_workspace", se.Hint)
	}
	wantCode(t, "hidden hover", p.Hover("0xb", 1, 1), screen.CodeWindowHidden)
	if called || len(*log) != 0 {
		t.Errorf("input sent to a hidden window: called=%v log=%q", called, *log)
	}
}

func TestPointerLocked(t *testing.T) {
	p, _, log := fakePointer(t, true, oneMonitor)
	called := false
	err := p.Do("", func(screen.PointerInput) error { called = true; return nil })
	wantCode(t, "locked", err, screen.CodeLocked)
	wantCode(t, "locked hover", p.Hover("", 1, 1), screen.CodeLocked)
	if called || len(*log) != 0 {
		t.Errorf("input sent while locked: called=%v log=%q", called, *log)
	}
}

// The extent is the logical layout: 1920x1080 at 1.5 is 1280x720, and
// coordinates are relative to the layout's origin. A disabled monitor does
// not count.
func TestPointerLogicalExtent(t *testing.T) {
	p, _, log := fakePointer(t, false, oneMonitor)
	if err := p.Hover("", 300, 200); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*log, []string{"move 300,200 in 1280x720"}) {
		t.Errorf("log %q, want no restore and the extent 1280x720", *log)
	}
	two := `[{"width":1920,"height":1080,"x":-1280,"y":0,"scale":1.5},{"width":1920,"height":1200,"x":0,"y":-100,"scale":1},{"width":800,"height":600,"x":5000,"y":0,"scale":1,"disabled":true}]`
	p, _, log = fakePointer(t, false, two)
	if err := p.Hover("", 0, 0); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*log, []string{"move 1280,100 in 3200x1200"}) {
		t.Errorf("log %q, want 1280,100 in 3200x1200", *log)
	}
}
