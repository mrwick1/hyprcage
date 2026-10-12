package mcpserver

import (
	"encoding/json"
	"fmt"
	"image"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/perceive"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// verifySettle is the delay between an action and the frame that judges it.
var verifySettle = 100 * time.Millisecond

const verifyDoc = "judge the action by diffing the screen before and after it (default on; set the verify key of config.toml to change the default). The result gains a verify line: CONFIRMED, PARTIAL, NO_OP or DIVERGED, with the change ratio and bounding box, and a sense object. A heuristic: animated pages can give a false CONFIRMED"

// frameSource is what a verification reads from a screen. *wl.Client satisfies it.
type frameSource interface {
	Capture(overlayCursor bool) (*image.RGBA, error)
	Toplevels() ([]wl.Toplevel, error)
}

// verifier holds the state captured before an action.
type verifier struct {
	src     frameSource
	before  *image.RGBA
	windows int
	points  []image.Point
}

// startVerify captures the frame and the window count before an action. It
// returns nil when verification is off (on is the call's verify, nil for the
// config default) or the capture fails: a verdict never fails an action.
func (s *Server) startVerify(on *bool, src frameSource, points ...image.Point) *verifier {
	if on == nil && !s.cfg.Verify || on != nil && !*on {
		return nil
	}
	img, err := src.Capture(false)
	if err != nil {
		return nil
	}
	v := &verifier{src: src, before: img, points: points}
	if tls, err := src.Toplevels(); err == nil {
		v.windows = len(tls)
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
	if tls, err := v.src.Toplevels(); err == nil {
		ver.Sense.Opened = len(tls) > v.windows
	}
	return &ver
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
