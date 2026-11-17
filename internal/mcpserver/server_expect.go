package mcpserver

import (
	"context"
	"crypto/rand"
	"fmt"
	"regexp"
	"time"

	"github.com/hexadecimil/offstage/internal/audit"
	"github.com/hexadecimil/offstage/internal/condition"
	"github.com/hexadecimil/offstage/internal/desktop"
	"github.com/hexadecimil/offstage/internal/devtools"
	"github.com/hexadecimil/offstage/internal/perceive"
	"github.com/hexadecimil/offstage/internal/redact"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type predicate struct {
	TaskID     string `json:"task_id,omitempty"`
	ResourceID string `json:"resource_id,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	Kind       string `json:"kind" jsonschema:"element, request, process or audio"`
	Text       string `json:"text,omitempty" jsonschema:"element name/value or request URL regular expression"`
	Role       string `json:"role,omitempty"`
	Absent     bool   `json:"absent,omitempty" jsonschema:"element absence; requires a successful non-OCR read"`
	Status     int    `json:"status,omitempty" jsonschema:"request HTTP status; omit to match any completed request"`
	Failed     bool   `json:"failed,omitempty" jsonschema:"request transport error or HTTP status at least 400"`
	Playing    *bool  `json:"playing,omitempty" jsonschema:"audio: true waits for a stream that plays into the screen's audio output, false for silence"`
}
type expectIn struct {
	Screen    string           `json:"screen"`
	Success   []predicate      `json:"success" jsonschema:"any matching condition succeeds unless a failure also matches"`
	Failure   []predicate      `json:"failure,omitempty" jsonschema:"any matching condition fails; failure takes precedence"`
	After     *devtools.Cursor `json:"after,omitempty" jsonschema:"cursor from condition_cursor; collector changes or expired events return unsupported"`
	Action    *actIn           `json:"action,omitempty" jsonschema:"optional act input; observation is armed before dispatch; screen must match"`
	TimeoutMs int              `json:"timeout_ms,omitempty" jsonschema:"one overall deadline, default 10000, maximum 600000"`
}
type expectResult struct {
	condition.Result
	Screen   string           `json:"screen"`
	ActionID string           `json:"action_id,omitempty"`
	Cursor   *devtools.Cursor `json:"cursor,omitempty"`
}

func (s *Server) registerScreenConditions(srv *mcp.Server) {
	addContext(srv, "condition_wait", "Wait for element presence/absence, completed requests or audio playing/silent on one agent screen. Failure predicates take precedence. Optional action is observed before dispatch. Returns evidence and explicit timeout, cancellation or unsupported state. Never treats a source error as element absence.", func(ctx context.Context, in expectIn) (*mcp.CallToolResult, []audit.Option, error) {
		r, e := s.conditionWait(ctx, in)
		return r, nil, e
	}, s)
	addContext(srv, "condition_cursor", "Arm request observation and return a cursor before an action. Use it in condition_wait after that action. A reconnect or buffer gap invalidates the cursor.", func(ctx context.Context, in screenIn) (*mcp.CallToolResult, []audit.Option, error) {
		if desktop.IsDesktop(in.Screen) {
			return nil, nil, fmt.Errorf("condition cursors require an agent screen")
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := s.conditionLock(ctx); err != nil {
			return nil, nil, err
		}
		defer s.mu.Unlock()
		_, d, err := s.devtoolsScreen(ctx, in.Screen)
		if err != nil {
			return nil, nil, err
		}
		c, err := d.Arm(ctx)
		if err != nil {
			return nil, nil, err
		}
		return textResult(c), nil, nil
	}, s)
}
func (s *Server) conditionLock(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.mu.TryLock() {
			return nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func compilePredicates(in expectIn) ([]*regexp.Regexp, bool, bool, error) {
	all := append(append([]predicate{}, in.Failure...), in.Success...)
	if len(in.Success) == 0 || len(all) > 16 {
		return nil, false, false, fmt.Errorf("provide success conditions; maximum 16 total predicates")
	}
	var patterns []*regexp.Regexp
	elements, network := false, false
	for _, p := range all {
		if p.Kind != "process" && (p.TaskID != "" || p.ResourceID != "" || p.ExitCode != nil) {
			return nil, false, false, fmt.Errorf("process fields on non-process condition")
		}
		if p.Kind != "audio" && p.Playing != nil {
			return nil, false, false, fmt.Errorf("playing on non-audio condition")
		}
		if len(p.Text) > 2048 {
			return nil, false, false, fmt.Errorf("condition pattern exceeds 2048 bytes")
		}
		switch p.Kind {
		case "process":
			if p.TaskID == "" || p.ResourceID == "" || p.Text != "" || p.Role != "" || p.Absent || p.Status != 0 || p.Failed {
				return nil, false, false, fmt.Errorf("invalid process predicate")
			}
		case "audio":
			if p.Playing == nil || p.Text != "" || p.Role != "" || p.Absent || p.Status != 0 || p.Failed {
				return nil, false, false, fmt.Errorf("invalid audio predicate: set playing only")
			}
		case "element":
			elements = true
			if p.Status != 0 || p.Failed {
				return nil, false, false, fmt.Errorf("request fields on element condition")
			}
		case "request":
			network = true
			if p.Absent || p.Role != "" || p.Status < 0 || p.Status > 599 || (p.Status > 0 && p.Status < 100) {
				return nil, false, false, fmt.Errorf("invalid request predicate")
			}
		default:
			return nil, false, false, fmt.Errorf("unknown condition kind %q", p.Kind)
		}
		re, err := regexp.Compile(p.Text)
		if err != nil {
			return nil, false, false, err
		}
		patterns = append(patterns, re)
	}
	return patterns, elements, network, nil
}
func (s *Server) conditionWait(parent context.Context, in expectIn) (*mcp.CallToolResult, error) {
	patterns, elements, network, err := compilePredicates(in)
	if err != nil {
		return nil, err
	}
	if desktop.IsDesktop(in.Screen) || in.Screen == "" {
		return nil, fmt.Errorf("provide an agent screen")
	}
	if in.TimeoutMs < 0 || in.TimeoutMs > 600000 {
		return nil, fmt.Errorf("invalid timeout_ms")
	}
	if in.TimeoutMs == 0 {
		in.TimeoutMs = 10000
	}
	if in.After != nil && !network {
		return nil, fmt.Errorf("after requires a request condition")
	}
	if in.Action != nil && (in.After != nil || in.Action.Window != "" || (in.Action.Screen != "" && in.Action.Screen != in.Screen)) {
		return nil, fmt.Errorf("action must target this agent screen and use a fresh cursor")
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(parent, time.Duration(in.TimeoutMs)*time.Millisecond)
	defer cancel()
	out := expectResult{Screen: in.Screen}
	interrupt := func() (*mcp.CallToolResult, error) {
		out.Status = "timed_out"
		if parent.Err() != nil {
			out.Status = "cancelled"
		}
		out.ElapsedMs = time.Since(start).Milliseconds()
		return textResult(out), nil
	}
	if err = s.conditionLock(ctx); err != nil {
		return interrupt()
	}
	rec, _, err := s.resolve(in.Screen, false)
	var d *devtools.Session
	var cursor devtools.Cursor
	if err == nil && network {
		d, err = s.devtoolsFor(ctx, rec)
		if err == nil {
			if in.After != nil {
				cursor = *in.After
				_, _, err = d.Events(cursor)
			} else {
				cursor, err = d.Arm(ctx)
			}
		}
	}
	s.mu.Unlock()
	if err != nil {
		if ctx.Err() != nil {
			return interrupt()
		}
		out.Status = "unsupported"
		out.Last = condition.Observation{Status: "unsupported", Evidence: map[string]string{"reason": redact.Text(err.Error())}}
		return textResult(out), nil
	}
	identity := tableKey(rec)
	if err := s.conditionLock(ctx); err != nil {
		return interrupt()
	}
	isBare := s.bare[identity] != nil
	s.mu.Unlock()
	if isBare {
		out.Status = "unsupported"
		out.Last = condition.Observation{Status: "unsupported", Evidence: map[string]string{"reason": "use native browser step assertions for Bare; request-start cursors are unavailable"}}
		return textResult(out), nil
	}

	source := "auto"
	all := append(append([]predicate{}, in.Failure...), in.Success...)
	observe := func(ctx context.Context) condition.Observation {
		if err := s.conditionLock(ctx); err != nil {
			return condition.Observation{Status: "pending"}
		}
		defer s.mu.Unlock()
		current, cl, err := s.resolve(in.Screen, elements)
		if err != nil || tableKey(current) != identity {
			return condition.Observation{Status: "failed", Evidence: map[string]string{"reason": "screen closed or replaced"}}
		}
		var nodes []perceive.Node
		if elements {
			sample, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			src, e := perceive.Choose(sample, current, cl, source)
			if e != nil {
				return condition.Observation{Status: "unsupported", Evidence: map[string]string{"reason": "element source unavailable"}}
			}
			nodes, e = src.Nodes(sample)
			name := src.Name()
			src.Close()
			if e != nil {
				return condition.Observation{Status: "unsupported", Evidence: map[string]string{"reason": "element read failed"}}
			}
			source = name
		}
		var events []devtools.NetworkEvent
		if network {
			if !d.Alive() || s.devtools[identity] != d {
				return condition.Observation{Status: "unsupported", Evidence: map[string]string{"reason": "request collector disconnected or replaced"}}
			}
			var next devtools.Cursor
			events, next, err = d.Events(cursor)
			out.Cursor = &next
			if err != nil {
				return condition.Observation{Status: "unsupported", Evidence: map[string]string{"reason": err.Error()}}
			}
		}
		matched := -1
		var evidence any = map[string]any{"source": source, "nodes": len(nodes), "completed_requests": len(events)}
		for i, p := range all {
			hit := false
			switch p.Kind {
			case "process":
				observation := observeProcessCondition(ctx, p)
				if observation.Status == "unsupported" {
					return observation
				}
				hit = observation.Status == "satisfied"
				evidence = observation.Evidence
			case "audio":
				h, observation := observeAudio(s.osf.Audio, current.Name, *p.Playing)
				if observation.Status == "unsupported" {
					return observation
				}
				hit, evidence = h, observation.Evidence
			case "element":
				if p.Absent && source == "ocr" {
					return condition.Observation{Status: "unsupported", Evidence: map[string]string{"reason": "OCR cannot prove element absence"}}
				}
				for _, n := range nodes {
					if (p.Role == "" || n.Role == p.Role) && (patterns[i].MatchString(n.Name) || patterns[i].MatchString(n.Value)) {
						hit = true
						break
					}
				}
				if p.Absent {
					hit = !hit
				}
			case "request":
				for _, ev := range events {
					r := ev.Request
					if r.Started > cursor.Started && patterns[i].MatchString(r.URL) && (p.Status == 0 || p.Status == r.Status) && (!p.Failed || r.Failed()) {
						hit = true
						r.URL = redact.Text(r.URL)
						r.Page = redact.Text(r.Page)
						r.Error = redact.Text(r.Error)
						evidence = map[string]any{"predicate": i, "sequence": ev.Sequence, "request": r}
						break
					}
				}
			}
			if hit {
				matched = i
				break
			}
		}
		status := "pending"
		if matched >= 0 {
			status = "satisfied"
			if matched < len(in.Failure) {
				status = "failed"
			}
			evidence = map[string]any{"predicate": matched, "observation": evidence}
		}
		return condition.Observation{Status: status, Evidence: evidence}
	}
	// Probe capability and source identity before an optional action.
	initial := observe(ctx)
	out.Last = initial
	if ctx.Err() != nil {
		return interrupt()
	}
	if initial.Status == "unsupported" || initial.Status == "failed" {
		out.Result = condition.Result{Status: initial.Status, Last: initial}
		return textResult(out), nil
	}
	if in.Action != nil {
		var actionEntry *audit.Entry
		if err = s.conditionLock(ctx); err != nil {
			return interrupt()
		}
		current, _, e := s.resolve(in.Screen, false)
		if e == nil && tableKey(current) != identity {
			e = fmt.Errorf("screen replaced before action")
		}
		if e == nil {
			out.ActionID = rand.Text()
			action := *in.Action
			action.Screen = in.Screen
			_, e = s.actContext(ctx, action)
			entry := audit.Record("act", action, e, audit.WithActionID(out.ActionID), audit.WithTaskID(current.TaskID), audit.WithScreenInstance(identity))
			actionEntry = &entry
		}
		s.mu.Unlock()
		if actionEntry != nil {
			s.recordTimeline(*actionEntry, nil)
		}
		if e != nil {
			out.Status = "failed"
			out.Last = condition.Observation{Status: "failed", Evidence: map[string]string{"reason": redact.Text(e.Error())}}
			return textResult(out), nil
		}
	}
	out.Result = condition.Wait(parent, time.Until(start.Add(time.Duration(in.TimeoutMs)*time.Millisecond)), 50*time.Millisecond, observe)
	out.ElapsedMs = time.Since(start).Milliseconds()
	return textResult(out), nil
}
