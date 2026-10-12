package mcpserver

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/audit"
	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/hypr"
	"github.com/hexadecimil/hyprcage/internal/perceive"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// fakeScreen serves one frame per Capture and one window list per Toplevels.
type fakeScreen struct {
	frames []*image.RGBA
	wins   [][]wl.Toplevel
}

func (f *fakeScreen) Capture(bool) (*image.RGBA, error) {
	img := f.frames[0]
	f.frames = f.frames[1:]
	return img, nil
}

func (f *fakeScreen) Toplevels() ([]wl.Toplevel, error) {
	w := f.wins[0]
	f.wins = f.wins[1:]
	return w, nil
}

func blank(fill image.Rectangle) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 800))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{250, 250, 250, 255}), image.Point{}, draw.Src)
	draw.Draw(img, fill, image.NewUniform(color.RGBA{20, 20, 200, 255}), image.Point{}, draw.Src)
	return img
}

func TestVerifierJudgesTheChange(t *testing.T) {
	s := newServer(config.Default())
	src := &fakeScreen{
		frames: []*image.RGBA{blank(image.Rectangle{}), blank(image.Rect(100, 100, 300, 200))},
		wins:   [][]wl.Toplevel{{{ID: 1}}, {{ID: 1}, {ID: 2}}},
	}
	v := s.startVerify(nil, src, image.Pt(150, 150))
	got := v.finish(0)
	if got == nil || got.Verdict != perceive.Confirmed || !got.Sense.Opened || !got.Sense.Changed {
		t.Fatalf("got %+v", got)
	}
}

func TestVerifierNoOpOnDeadArea(t *testing.T) {
	s := newServer(config.Default())
	src := &fakeScreen{
		frames: []*image.RGBA{blank(image.Rectangle{}), blank(image.Rectangle{})},
		wins:   [][]wl.Toplevel{{}, {}},
	}
	got := s.startVerify(nil, src, image.Pt(5, 5)).finish(0)
	if got.Verdict != perceive.NoOp || !got.Sense.NoOp || got.Sense.Opened {
		t.Fatalf("got %+v", got)
	}
}

func TestVerifyOptOut(t *testing.T) {
	off := false
	s := newServer(config.Default())
	if v := s.startVerify(&off, &fakeScreen{}); v != nil {
		t.Fatal("verify:false must not capture")
	}
	if v := s.startVerify(&off, &fakeScreen{}); v.finish(0) != nil {
		t.Fatal("a nil verifier yields no verdict")
	}
	c := config.Default()
	c.Verify = false
	if v := newServer(c).startVerify(nil, &fakeScreen{}); v != nil {
		t.Fatal("config default off must skip the capture")
	}
	on := true
	src := &fakeScreen{frames: []*image.RGBA{blank(image.Rectangle{}), blank(image.Rectangle{})}, wins: [][]wl.Toplevel{{}, {}}}
	if v := newServer(c).startVerify(&on, src); v == nil {
		t.Fatal("verify:true must override a config default of off")
	}
}

// The verdict a handler notes on its result reaches the audit log.
func TestVerdictReachesAuditLog(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	type in struct {
		Verify bool `json:"verify"`
	}
	unlocked(srv, "probe", "probe", func(i in) (*mcp.CallToolResult, error) {
		res := plain("ok")
		if i.Verify {
			v := perceive.NewVerification(perceive.FrameDiff{Ratio: 0.042, BBox: image.Rect(120, 80, 760, 380)})
			noteVerdict(res, &v)
		}
		return res, nil
	})
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "probe", Arguments: map[string]any{"verify": true}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text(res), "verify: CONFIRMED (changed 4.2%") {
		t.Fatalf("result lacks the verdict line: %q", text(res))
	}
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "probe", Arguments: map[string]any{"verify": false}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(audit.Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d audit lines", len(lines))
	}
	var first, second map[string]any
	json.Unmarshal([]byte(lines[0]), &first)
	json.Unmarshal([]byte(lines[1]), &second)
	vd, _ := first["verdict"].(map[string]any)
	if vd["verdict"] != "CONFIRMED" {
		t.Fatalf("verdict not logged: %v", first)
	}
	if _, ok := second["verdict"]; ok {
		t.Fatalf("verdict logged without a verification: %v", second)
	}
}

func TestSettleIsShortenedForTests(t *testing.T) {
	if verifySettle <= 0 || verifySettle > time.Second {
		t.Fatalf("verifySettle %s", verifySettle)
	}
}

// deskScreen serves frames and desktop window lists, as the desktop does.
type deskScreen struct {
	frames []*image.RGBA
	wins   [][]hypr.Client
}

func (f *deskScreen) Capture(bool) (*image.RGBA, error) {
	img := f.frames[0]
	f.frames = f.frames[1:]
	return img, nil
}

func (f *deskScreen) Windows() ([]hypr.Client, error) {
	w := f.wins[0]
	f.wins = f.wins[1:]
	return w, nil
}

func TestDesktopVerifierOpenedAndModal(t *testing.T) {
	s := newServer(config.Default())
	src := &deskScreen{
		frames: []*image.RGBA{blank(image.Rectangle{}), blank(image.Rect(100, 100, 300, 200))},
		wins:   [][]hypr.Client{{{Address: "0xa"}}, {{Address: "0xa"}, {Address: "0xb", PID: 7, Title: "About"}}},
	}
	var got []hypr.Client
	v := s.startVerify(nil, src, image.Pt(150, 150))
	v.modal = func(w []hypr.Client) bool { got = w; return true }
	ver := v.finish(0)
	if ver == nil || !ver.Sense.Opened || !ver.Sense.Modal || ver.Verdict != perceive.Confirmed {
		t.Fatalf("got %+v", ver)
	}
	if len(got) != 1 || got[0].Address != "0xb" {
		t.Fatalf("modal check saw %+v, want only the new window", got)
	}
}

func TestDesktopVerifierClosedWindowIsNotOpened(t *testing.T) {
	s := newServer(config.Default())
	src := &deskScreen{
		frames: []*image.RGBA{blank(image.Rectangle{}), blank(image.Rect(0, 0, 50, 50))},
		wins:   [][]hypr.Client{{{Address: "0xa"}, {Address: "0xb"}}, {{Address: "0xa"}, {Address: "0xc"}}},
	}
	v := s.startVerify(nil, src)
	v.modal = func(w []hypr.Client) bool { return false }
	ver := v.finish(0)
	if !ver.Sense.Opened || ver.Sense.Modal {
		t.Fatalf("a replaced window is a new window, not modal by default: %+v", ver.Sense)
	}
	src = &deskScreen{
		frames: []*image.RGBA{blank(image.Rectangle{}), blank(image.Rectangle{})},
		wins:   [][]hypr.Client{{{Address: "0xa"}, {Address: "0xb"}}, {{Address: "0xa"}}},
	}
	if ver := s.startVerify(nil, src).finish(0); ver.Sense.Opened {
		t.Fatal("a window that closed is not opened")
	}
}

func TestToFrame(t *testing.T) {
	// A 1920x1080 monitor at scale 1.5 shows 1280 logical pixels, captured at 1920 pixels.
	if got := toFrame(image.Pt(740, 450), [2]int{100, 50}, 1280, 1920); got != image.Pt(960, 600) {
		t.Fatalf("got %v", got)
	}
	// A window captured at its logical size keeps the offset only.
	if got := toFrame(image.Pt(130, 70), [2]int{100, 50}, 800, 800); got != image.Pt(30, 20) {
		t.Fatalf("got %v", got)
	}
	// A zero width gives the offset without a scale.
	if got := toFrame(image.Pt(130, 70), [2]int{100, 50}, 0, 800); got != image.Pt(30, 20) {
		t.Fatalf("got %v", got)
	}
}
