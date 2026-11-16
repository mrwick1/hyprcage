//go:build linux

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/hexadecimil/offstage/internal/audit"
	"github.com/hexadecimil/offstage/internal/perceive"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/session"
	"github.com/hexadecimil/offstage/internal/task"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func timelineMetadata(in any) []audit.Option {
	b, _ := json.Marshal(in)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(b, &fields)
	var name string
	_ = json.Unmarshal(fields["screen"], &name)
	owner := session.Current().Owner()
	if name == "" {
		typ := reflect.TypeOf(in)
		if typ == nil || typ.Kind() != reflect.Struct {
			return nil
		}
		if _, ok := typ.FieldByName("Screen"); !ok {
			return nil
		}
		records, err := registry.List()
		if err != nil {
			return nil
		}
		for _, r := range records {
			if r.Owner == owner {
				if name != "" {
					return nil
				}
				name = r.Name
			}
		}
	}
	rec, err := registry.Load(name)
	if err != nil || rec.TaskID == "" || rec.Owner != owner {
		return nil
	}
	return []audit.Option{audit.WithScreen(name), audit.WithTaskID(rec.TaskID), audit.WithScreenInstance(tableKey(rec))}
}
func eventData(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// recordTimeline runs after the tool releases its UI lock. Evidence errors never replace its result.
func (s *Server) recordTimeline(e audit.Entry, res *mcp.CallToolResult) {
	defer func() {
		if recover() != nil && res != nil {
			res.Content = append(res.Content, &mcp.TextContent{Text: "Task evidence collection failed. The original tool result is unchanged."})
		}
	}()
	if e.Tool == "task_timeline" {
		return
	}
	var reply map[string]json.RawMessage
	if res != nil {
		_ = json.Unmarshal([]byte(textResultText(res)), &reply)
	}
	if e.Tool == "task_create" {
		_ = json.Unmarshal(reply["id"], &e.TaskID)
	}
	if e.Tool == "screen_create" {
		_ = json.Unmarshal(reply["screen"], &e.Screen)
		if rec, err := registry.Load(e.Screen); err == nil {
			e.ScreenInstance = tableKey(rec)
			e.TaskID = rec.TaskID
		}
	}
	if e.TaskID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	payload := map[string]any{"action": e}
	correlationID := e.ActionID
	var status string
	_ = json.Unmarshal(reply["status"], &status)
	if strings.HasPrefix(e.Tool, "browser_") && res != nil {
		if e.Tool == "browser_console" || e.Tool == "browser_network" {
			payload["native_diagnostics"] = textResultText(res)
		}
		if res.StructuredContent != nil {
			raw, _ := json.Marshal(res.StructuredContent)
			var native map[string]any
			_ = json.Unmarshal(raw, &native)
			summary := map[string]any{}
			for _, key := range []string{"action_status", "page_status", "verification", "ok", "result_source"} {
				if v, ok := native[key]; ok {
					summary[key] = v
				}
			}
			payload["native_outcome"] = summary
		}
	}
	if status != "" {
		payload["status"] = status
	}
	if e.Tool == "condition_wait" || e.Tool == "task_wait" {
		var evidence any
		_ = json.Unmarshal(reply["last_observation"], &evidence)
		payload["outcome"] = evidence
		var actionID string
		_ = json.Unmarshal(reply["action_id"], &actionID)
		if actionID != "" {
			payload["trigger_action_id"] = actionID
			correlationID = actionID
		}
	}
	events := []task.Event{{ID: e.ActionID, Source: "actions", Kind: e.Tool, ActionID: correlationID, Data: eventData(payload)}}
	sources := map[string]task.SourceState{"screenshots": {Status: "unavailable", Note: "screenshots are not collected; failure page excerpts are retained"},
		"actions": {Status: "available", Note: "events are recorded when tool calls return; order does not prove causality"}}
	if (e.Error != "" || status == "failed" || status == "timed_out") && e.Screen != "" {
		capture := e
		capture.ActionID = correlationID
		shot, state := s.failureState(ctx, capture)
		sources["failure_state:"+e.ScreenInstance] = state
		if shot != nil {
			events = append(events, *shot)
		}
	}
	err := task.AppendEvents(ctx, e.TaskID, session.Current().Owner(), events, sources)
	if err == nil {
		_, err = s.taskTimeline(ctx, timelineIn{TaskID: e.TaskID, Limit: 1})
	}
	if err != nil && res != nil {
		res.Content = append(res.Content, &mcp.TextContent{Text: "Task evidence could not be saved. The tool result is unchanged; task_timeline may be incomplete."})
	}
}
func (s *Server) failureState(ctx context.Context, e audit.Entry) (*task.Event, task.SourceState) {
	unavailable := task.SourceState{Status: "unavailable", Note: "failure page state could not be read"}
	if err := s.conditionLock(ctx); err != nil {
		return nil, unavailable
	}
	defer s.mu.Unlock()
	rec, cl, err := s.resolve(e.Screen, true)
	if err != nil || tableKey(rec) != e.ScreenInstance {
		return nil, unavailable
	}
	if b := s.bare[e.ScreenInstance]; b != nil {
		name := s.bareSessionHints[e.ScreenInstance]
		if name == "" {
			return nil, unavailable
		}
		reply, err := b.CallTool(ctx, &mcp.CallToolParams{Name: "snapshot", Arguments: map[string]any{"session": name, "mode": "all", "max_chars": 4000}})
		if err != nil || reply == nil || reply.IsError {
			return nil, unavailable
		}
		lines := strings.Split(textResultText(reply), "\n")
		for i, line := range lines {
			lines[i] = strings.SplitN(line, " = ", 2)[0]
		}
		event := task.Event{ID: e.ActionID + ":state", Source: "failure_state:" + e.ScreenInstance, Kind: "page_state", ActionID: e.ActionID, Data: eventData(map[string]any{"source": "bare", "outline": strings.Join(lines, "\n"), "values_omitted": true})}
		return &event, task.SourceState{Status: "partial", Note: "bounded native Bare outline; input values omitted"}
	}
	src, err := perceive.Choose(ctx, rec, cl, "auto")
	if err != nil {
		return nil, unavailable
	}
	defer src.Close()
	nodes, err := src.Nodes(ctx)
	if err != nil {
		return nil, unavailable
	}
	// Values and full page text are not retained. Keep a small failure excerpt.
	selected := []map[string]any{}
	for _, n := range nodes {
		if len(selected) >= 20 {
			break
		}
		name := []rune(n.Name)
		if len(name) > 160 {
			name = name[:160]
		}
		selected = append(selected, map[string]any{"role": n.Role, "name": string(name), "states": n.States})
	}
	event := task.Event{ID: e.ActionID + ":state", Source: "failure_state:" + e.ScreenInstance, Kind: "page_state", ActionID: e.ActionID, Data: eventData(map[string]any{"source": src.Name(), "elements": selected, "total_nodes": len(nodes), "truncated": len(nodes) > len(selected)})}
	state := task.SourceState{Status: "available", Note: "page excerpt collected after the failed call; timing does not establish cause"}
	if len(nodes) > len(selected) {
		state.Status = "partial"
	}
	return &event, state
}

type timelineIn struct {
	TaskID   string              `json:"task_id"`
	After    task.TimelineCursor `json:"after,omitempty"`
	Limit    int                 `json:"limit,omitempty" jsonschema:"default 50, maximum 200"`
	ActionID string              `json:"action_id,omitempty"`
}

func (s *Server) registerTimeline(srv *mcp.Server) {
	addContext(srv, "task_timeline", "Read a local task failure timeline with bounded pagination, evidence gaps and source availability. Collects native-app diagnostics and process log excerpts; Bare diagnostic excerpts come from explicit browser_console/browser_network calls; tool outcomes and failure page excerpts are retained when calls return. Sequence is collection order, not proof of causality. Does not upload anything.", func(ctx context.Context, in timelineIn) (*mcp.CallToolResult, []audit.Option, error) {
		r, e := s.taskTimeline(ctx, in)
		return r, nil, e
	}, s)
}
func (s *Server) taskTimeline(ctx context.Context, in timelineIn) (*mcp.CallToolResult, error) {
	if in.After.TaskID != "" && in.After.TaskID != in.TaskID {
		return nil, fmt.Errorf("cursor belongs to another task")
	}
	if in.Limit < 0 || in.Limit > 200 {
		return nil, fmt.Errorf("invalid limit")
	}
	owner := session.Current().Owner()
	t, err := task.InspectContext(ctx, in.TaskID, owner)
	if err != nil {
		return nil, err
	}
	events := []task.Event{}
	sources := map[string]task.SourceState{}
	for _, r := range t.Resources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r.Kind == "process" {
			source := "process:" + r.ID
			state := task.SourceState{Status: "available", Path: r.Log}
			if r.LogState == "unavailable" || r.LogState == "incomplete" {
				state.Status = "partial"
				state.Note = "log collection disconnected or incomplete"
			}
			f, e := os.Open(r.Log)
			if e != nil {
				state.Status = "unavailable"
				state.Note = "process log unavailable"
			} else {
				info, e := f.Stat()
				if e != nil {
					state.Status = "unavailable"
				} else {
					start := max(int64(0), info.Size()-4096)
					_, _ = f.Seek(start, io.SeekStart)
					data, e := io.ReadAll(io.LimitReader(f, 4096))
					if e != nil {
						state.Status = "unavailable"
					} else {
						if start > 0 {
							state.Status = "partial"
							state.Note = "tail excerpt only; full redacted log is linked"
						}
						events = append(events, task.Event{ID: "log:" + strconv.FormatInt(info.Size(), 10), Source: source, Kind: "log_excerpt", Stream: "log", SourceSequence: uint64(info.Size()) + 1, SourceAt: info.ModTime(), Data: eventData(map[string]any{"offset": start, "text": string(data)})})
					}
				}
				f.Close()
			}
			sources[source] = state
			if r.ExitCode != nil {
				events = append(events, task.Event{ID: "exit", Source: source, Kind: "process_exit", Stream: "exit", SourceSequence: 1, Data: eventData(map[string]any{"exit_code": *r.ExitCode, "state": r.State})})
			}
		}
		if r.Kind == "screen" {
			identity := fmt.Sprintf("%s@%d", r.Screen, r.CreatedAt.UnixNano())
			source := "browser:" + identity
			if err := s.conditionLock(ctx); err != nil {
				return nil, err
			}
			if b := s.bare[identity]; b != nil {
				name := s.bareSessionHints[identity]
				if name == "" {
					sources[source] = task.SourceState{Status: "unavailable", Note: "no selected Bare session"}
					s.mu.Unlock()
					continue
				}
				sources[source] = task.SourceState{Status: "partial", Note: "diagnostic excerpts are retained only from explicit browser_console/browser_network calls; timeline reads do not consume native cursors"}
				s.mu.Unlock()
				continue
			}
			d := s.devtools[identity]
			if d == nil {
				sources[source] = task.SourceState{Status: "unavailable", Note: "no retained diagnostics collector"}
				s.mu.Unlock()
				continue
			}
			requests, cursor, lost := d.Completed()
			messages, dropped := d.ConsoleEvidence()
			status := "available"
			if !d.Alive() || lost > 0 || dropped > 0 {
				status = "partial"
			}
			sources[source] = task.SourceState{Status: status, Dropped: lost + dropped, Note: "buffered events collected now; no reconnect or page navigation"}
			for _, ev := range requests {
				events = append(events, task.Event{ID: cursor.Source + ":request:" + strconv.FormatUint(ev.Sequence, 10), Source: source, Kind: "request", Stream: cursor.Source + ":request", SourceSequence: ev.Sequence, SourceAt: ev.At, Data: eventData(ev.Request)})
			}
			for _, msg := range messages {
				events = append(events, task.Event{ID: cursor.Source + ":console:" + strconv.FormatUint(msg.Sequence, 10), Source: source, Kind: "console", Stream: cursor.Source + ":console", SourceSequence: msg.Sequence, SourceAt: msg.At, Data: eventData(msg)})
			}
			s.mu.Unlock()
		}
	}
	if err := task.AppendEvents(ctx, in.TaskID, owner, events, sources); err != nil {
		return nil, err
	}
	page, err := task.ReadTimeline(ctx, in.TaskID, owner, in.After, in.Limit, in.ActionID)
	if err != nil {
		return nil, err
	}
	return textResult(page), nil
}
