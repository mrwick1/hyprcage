package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"math"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/desktop"
	"github.com/hexadecimil/hyprcage/internal/hypr"
	"github.com/hexadecimil/hyprcage/internal/perceive"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// verifySettle is the delay between an action and the frame that judges it.
var verifySettle = 100 * time.Millisecond

const verifyDoc = "judge the action by diffing the screen before and after it (default on; set the verify key of config.toml to change the default). The result gains a verify line: CONFIRMED, PARTIAL, NO_OP or DIVERGED, with the change ratio and bounding box, and a sense object. A heuristic: animated pages can give a false CONFIRMED"

// frameSource is what a verification reads from a screen. *wl.Client satisfies
// it. A source also lists its windows, by Toplevels (agent screen) or by
// Windows (desktop, with addresses).
type frameSource interface {
	Capture(overlayCursor bool) (*image.RGBA, error)
}

type toplevelSource interface {
	Toplevels() ([]wl.Toplevel, error)
}

type windowSource interface {
	Windows() ([]hypr.Client, error)
}

// modalCheck reports whether one of the new desktop windows is a dialog.
var modalCheck = func(wins []hypr.Client) bool {
	ctx, cancel := context.WithTimeout(context.Background(), perceiveTimeout)
	defer cancel()
	return perceive.AnyModal(ctx, wins)
}

// verifier holds the state captured before an action.
type verifier struct {
	src     frameSource
	before  *image.RGBA
	windows int
	addrs   map[string]bool // desktop windows before the action; nil on an agent screen
	modal   func([]hypr.Client) bool
	points  []image.Point
	conn    *desktop.Conn // the connection the verifier opened, closed by release
}

// verifyOn reports whether a call verifies: on is the call's verify, nil for
// the config default.
func (s *Server) verifyOn(on *bool) bool {
	if on == nil {
		return s.cfg.Verify
	}
	return *on
}

// startVerify captures the frame and the window count before an action. It
// returns nil when verification is off (on is the call's verify, nil for the
// config default) or the capture fails: a verdict never fails an action.
func (s *Server) startVerify(on *bool, src frameSource, points ...image.Point) *verifier {
	if !s.verifyOn(on) {
		return nil
	}
	img, err := src.Capture(false)
	if err != nil {
		return nil
	}
	v := &verifier{src: src, before: img, points: points, modal: modalCheck}
	switch ws := src.(type) {
	case windowSource:
		if wins, err := ws.Windows(); err == nil {
			v.addrs = map[string]bool{}
			for _, w := range wins {
				v.addrs[w.Address] = true
			}
		}
	case toplevelSource:
		if tls, err := ws.Toplevels(); err == nil {
			v.windows = len(tls)
		}
	}
	return v
}

// finish waits for settle, captures the frame after the action and judges the
// change. A nil verifier yields nil.
func (v *verifier) finish(settle time.Duration) *perceive.Verification {
	if v == nil {
		return nil
	}
	time.Sleep(settle)
	after, err := v.src.Capture(false)
	if err != nil {
		return nil
	}
	ver := perceive.NewVerification(perceive.Compare(v.before, after), v.points...)
	switch ws := v.src.(type) {
	case windowSource:
		if wins, err := ws.Windows(); err == nil && v.addrs != nil {
			var fresh []hypr.Client
			for _, w := range wins {
				if !v.addrs[w.Address] {
					fresh = append(fresh, w)
				}
			}
			ver.Sense.Opened = len(fresh) > 0
			ver.Sense.Modal = len(fresh) > 0 && v.modal != nil && v.modal(fresh)
		}
	case toplevelSource:
		if tls, err := ws.Toplevels(); err == nil {
			ver.Sense.Opened = len(tls) > v.windows
		}
	}
	return &ver
}

// release closes the connection that the verifier opened. A nil verifier is fine.
func (v *verifier) release() {
	if v != nil && v.conn != nil {
		v.conn.CL.Close()
		v.conn = nil
	}
}

// desktopFrames captures the desktop output, or one window of it.
type desktopFrames struct {
	conn   *desktop.Conn
	window string
}

func (f desktopFrames) Capture(cursor bool) (*image.RGBA, error) {
	return f.conn.Frame(f.window, cursor)
}

func (f desktopFrames) Windows() ([]hypr.Client, error) { return f.conn.H.Clients() }

// toFrame converts a point in global logical pixels to a pixel of a frame
// whose origin sits at origin (logical) and whose width is logicalW logical
// pixels and frameW pixels. A zero logicalW keeps the scale at 1.
func toFrame(p image.Point, origin [2]int, logicalW float64, frameW int) image.Point {
	k := 1.0
	if logicalW > 0 {
		k = float64(frameW) / logicalW
	}
	return image.Pt(int(math.Round(float64(p.X-origin[0])*k)), int(math.Round(float64(p.Y-origin[1])*k)))
}

// startDesktopVerify captures the desktop output, or window, before an action
// on conn. The points are in global logical pixels. It returns nil when
// verification is off or the geometry is unknown: a verdict never fails an
// action.
func (s *Server) startDesktopVerify(on *bool, conn *desktop.Conn, window string, points ...image.Point) *verifier {
	if !s.verifyOn(on) {
		return nil
	}
	src := desktopFrames{conn, window}
	var origin [2]int
	var logicalW float64
	if window != "" {
		w, err := conn.Window(window)
		if err != nil {
			return nil
		}
		origin, logicalW = w.At, float64(w.Size[0])
	} else {
		mons, err := conn.H.Monitors()
		if err != nil || len(mons) == 0 {
			return nil
		}
		origin, logicalW = [2]int{mons[0].X, mons[0].Y}, float64(mons[0].Width)/mons[0].Scale
	}
	v := s.startVerify(nil, src)
	if v == nil {
		return nil
	}
	for _, p := range points {
		v.points = append(v.points, toFrame(p, origin, logicalW, v.before.Bounds().Dx()))
	}
	return v
}

// startDesktopKeyVerify is startDesktopVerify for a tool that has no
// connection of its own. The verifier owns the connection: call release.
func (s *Server) startDesktopKeyVerify(on *bool, d desktop.Desktop, window string) *verifier {
	if !s.verifyOn(on) {
		return nil
	}
	conn, err := desktop.Open(d.H)
	if err != nil {
		return nil
	}
	v := s.startDesktopVerify(on, conn, window)
	if v == nil {
		conn.CL.Close()
		return nil
	}
	v.conn = conn
	return v
}

// verdicts maps a result to its verification until the audit log reads it.
var verdicts sync.Map // *mcp.CallToolResult -> *perceive.Verification

// noteVerdict adds the verify line and the sense object to res and keeps the
// verification for the audit log. A nil verification changes nothing.
func noteVerdict(res *mcp.CallToolResult, v *perceive.Verification) {
	if v == nil {
		return
	}
	sense, _ := json.Marshal(map[string]any{"sense": v.Sense})
	res.Content = append(res.Content, &mcp.TextContent{Text: fmt.Sprintf("%s\n%s", v, sense)})
	verdicts.Store(res, v)
}

// takeVerdict returns the verification noted on res, as the value the audit
// log attaches, or nil.
func takeVerdict(res *mcp.CallToolResult) any {
	if res == nil {
		return nil
	}
	if v, ok := verdicts.LoadAndDelete(res); ok {
		return v
	}
	return nil
}
