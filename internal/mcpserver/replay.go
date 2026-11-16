package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/offstage/internal/audit"
	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/desktop"
	"github.com/hexadecimil/offstage/internal/perceive"
)

// ReplayStep is the outcome of one trace line in a replay.
type ReplayStep struct {
	N       int    // line number in the trace, from 1
	Tool    string // tool of the line
	Was     string // outcome in the trace: an error code, a verdict or ok
	Fail    bool   // the line failed in the trace
	GotFail bool   // the line failed in the replay
	Got     string // outcome in the replay, empty when skipped
	Note    string // why the line was skipped, or how its target was found
}

// Same reports whether the replay gave the outcome of the trace. A verdict
// counts only when both runs have one: verify can be on in one run only.
func (r ReplayStep) Same() bool {
	return r.Got == "" || r.Got == r.Was || !r.Fail && !r.GotFail && (r.Was == "ok" || r.Got == "ok")
}

// ReplayOptions changes how a trace replays.
type ReplayOptions struct {
	Pace time.Duration // longest pause kept between two lines
	Keep bool          // keep the screens the replay created
}

// Replay runs a trace from the audit log again, on new agent screens, through
// the same tools. It never touches the human's desktop: it skips the desktop
// screen, the desktop_* and notify_* tools, setup and report_issue. Typed text
// is replaced by as many "x" characters as the trace logged, and a printable
// key by "x". Before an act or a pointer action that has a target, it takes a
// snapshot and finds the element again by role and name.
func Replay(ctx context.Context, trace []audit.Entry, o ReplayOptions) ([]ReplayStep, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	s := newServer(cfg)
	var last audit.Entry
	recorded = func(e audit.Entry) { last = e }
	defer func() { recorded = nil }()
	ct, st := mcp.NewInMemoryTransports()
	ss, err := s.mcpServer().Connect(ctx, st, nil)
	if err != nil {
		return nil, err
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "offstage-replay"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		return nil, err
	}
	defer cs.Close()

	screens := map[string]string{} // trace screen -> replay screen
	var created, unmapped []string
	if !o.Keep {
		defer func() {
			for _, name := range created {
				_, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "screen_destroy", Arguments: map[string]any{"screen": name}})
			}
		}()
	}
	var steps []ReplayStep
	var prev time.Time
	for i, e := range trace {
		step := ReplayStep{N: i + 1, Tool: e.Tool, Was: outcome(e), Fail: e.Error != ""}
		if step.Note = skipReason(e); step.Note != "" {
			steps = append(steps, step)
			continue
		}
		var args map[string]any
		if b, err := json.Marshal(e.Args); err == nil {
			_ = json.Unmarshal(b, &args)
		}
		if args == nil {
			args = map[string]any{}
		}
		fill(args)
		if e.Ack != "" {
			args["ack"] = e.Ack
		}
		if e.Screen != "" {
			name, ok := screens[e.Screen]
			if !ok && len(unmapped) > 0 {
				name, unmapped, ok = unmapped[0], unmapped[1:], true
				screens[e.Screen] = name
			}
			if !ok {
				step.Note = "no screen_create in the trace for screen " + e.Screen
				steps = append(steps, step)
				continue
			}
			args["screen"] = name
		}
		if t, err := time.Parse(time.RFC3339Nano, e.Time); err == nil {
			if !prev.IsZero() {
				time.Sleep(min(max(t.Sub(prev), 0), o.Pace))
			}
			prev = t
		}
		if len(e.Target) > 0 {
			step.Note = s.retarget(ctx, cs, e, args)
		}
		last = audit.Entry{}
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: e.Tool, Arguments: args})
		if err != nil {
			return steps, fmt.Errorf("step %d (%s): %w", step.N, e.Tool, err)
		}
		step.Got, step.GotFail = outcome(last), last.Error != ""
		if e.Tool == "screen_create" && !res.IsError {
			var out struct{ Screen string }
			if tc, ok := res.Content[0].(*mcp.TextContent); ok && json.Unmarshal([]byte(tc.Text), &out) == nil && out.Screen != "" {
				created = append(created, out.Screen)
				unmapped = append(unmapped, out.Screen)
			}
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// skipReason returns why a trace line does not replay, empty when it does.
func skipReason(e audit.Entry) string {
	switch {
	case desktop.IsDesktop(e.Screen) || strings.HasPrefix(e.Tool, "desktop_") || strings.HasPrefix(e.Tool, "notify_"):
		return "acts on the human's desktop"
	case e.Tool == "setup" || e.Tool == "report_issue":
		return "not replayed"
	case e.Tool == "devtools_eval":
		return "the expression is not logged"
	}
	return ""
}

// fill puts placeholders where the audit log removed typed text or a
// printable key.
func fill(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if base, ok := strings.CutSuffix(k, "_len"); ok && secret[base] {
				if n, ok := x.(float64); ok {
					t[base] = strings.Repeat("x", int(n))
				}
				delete(t, k)
				continue
			}
			if k == "keys" {
				if ks, ok := x.([]any); ok {
					for i, key := range ks {
						if key == "<char>" {
							ks[i] = "x"
						}
					}
				}
			}
			fill(x)
		}
	case []any:
		for _, x := range t {
			fill(x)
		}
	}
}

// secret are the arguments that the audit log replaces with <name>_len.
var secret = map[string]bool{"text": true, "value": true, "data": true}

// retarget takes a snapshot and finds the targets of e again by role and
// name. It points args at the element found: the ref of an act, or the
// same offset from the element's centre for a pointer action. It returns a
// note for the step.
func (s *Server) retarget(ctx context.Context, cs *mcp.ClientSession, e audit.Entry, args map[string]any) string {
	snap := map[string]any{"mode": "full"}
	if v, ok := args["screen"]; ok {
		snap["screen"] = v
	}
	if res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "snapshot", Arguments: snap}); err != nil || res.IsError {
		return "no snapshot, target not checked"
	}
	screenName, _ := args["screen"].(string)
	s.mu.Lock()
	t := s.guardTable(guardCall{Screen: screenName})
	s.mu.Unlock()
	if t == nil {
		return "no snapshot, target not checked"
	}
	fresh := t.Nodes()
	points := [][2]string{{"x", "y"}}
	if e.Tool == "drag" {
		points = [][2]string{{"x1", "y1"}, {"x2", "y2"}}
	}
	var notes []string
	for i, want := range e.Target {
		if want.Role == "" || i >= len(points) {
			continue
		}
		n, err := perceive.ReResolve(perceive.Node{Role: want.Role, Name: want.Name}, fresh)
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s %q not found", want.Role, want.Name))
			continue
		}
		if e.Tool == "act" {
			args["ref"] = n.Ref
			notes = append(notes, fmt.Sprintf("%s %q is %s", want.Role, want.Name, n.Ref))
			continue
		}
		px, py := points[i][0], points[i][1]
		x, okx := args[px].(float64)
		y, oky := args[py].(float64)
		if !okx || !oky {
			continue
		}
		dx, dy := n.X-want.X, n.Y-want.Y
		if dx != 0 || dy != 0 {
			args[px], args[py] = int(x)+dx, int(y)+dy
			notes = append(notes, fmt.Sprintf("%s %q moved by %d,%d", want.Role, want.Name, dx, dy))
		}
	}
	return strings.Join(notes, "; ")
}

// outcome names the result of a trace line: its error code, else its
// verdict, else ok.
func outcome(e audit.Entry) string {
	if e.Error != "" {
		return e.Error
	}
	if e.Verdict != nil {
		var v struct{ Verdict string }
		if b, err := json.Marshal(e.Verdict); err == nil && json.Unmarshal(b, &v) == nil && v.Verdict != "" {
			return v.Verdict
		}
	}
	return "ok"
}

// Reproduced reports whether a replay hit the last failure of the trace: the
// same tool with the same error code. It returns false when the trace has no
// failure.
func Reproduced(steps []ReplayStep) bool {
	var want *ReplayStep
	for i := range steps {
		if steps[i].Fail {
			want = &steps[i]
		}
	}
	if want == nil {
		return false
	}
	for _, st := range steps {
		if st.Tool == want.Tool && st.Got == want.Was {
			return true
		}
	}
	return false
}
