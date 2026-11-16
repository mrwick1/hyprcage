//go:build linux

package mcpserver

import (
	"context"
	"fmt"
	"github.com/hexadecimil/offstage/internal/audit"
	"github.com/hexadecimil/offstage/internal/condition"
	"github.com/hexadecimil/offstage/internal/session"
	"github.com/hexadecimil/offstage/internal/task"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"time"
)

type taskWaitIn struct {
	TaskID     string `json:"task_id"`
	ResourceID string `json:"resource_id" jsonschema:"process resource ID returned by task_launch"`
	ExitCode   *int   `json:"exit_code,omitempty" jsonschema:"expected exit code; omit to accept any observed exit code"`
	TimeoutMs  int    `json:"timeout_ms,omitempty" jsonschema:"one overall deadline; default 10000, maximum 600000"`
}

func (s *Server) registerConditions(srv *mcp.Server) {
	addContext(srv, "task_wait", "Wait for an owned task process to exit. Returns satisfied, failed, timed_out, cancelled or unsupported with the last observation. A mismatched exit code or closed task fails. Does not hold the screen lock, so task_close remains available.", func(ctx context.Context, in taskWaitIn) (*mcp.CallToolResult, []audit.Option, error) {
		r, e := s.taskWait(ctx, in)
		return r, nil, e
	})
}
func (s *Server) taskWait(ctx context.Context, in taskWaitIn) (*mcp.CallToolResult, error) {
	if in.TimeoutMs < 0 || in.TimeoutMs > 600000 {
		return nil, fmt.Errorf("timeout_ms must be between 0 and 600000")
	}
	timeout := in.TimeoutMs
	if timeout == 0 {
		timeout = 10000
	}
	start := time.Now()
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()
	interrupted := func() (*mcp.CallToolResult, error) {
		status := "timed_out"
		if parent.Err() != nil {
			status = "cancelled"
		}
		return textResult(condition.Result{Status: status, ElapsedMs: time.Since(start).Milliseconds()}), nil
	}
	owner := session.Current().Owner()
	// Validate identity before polling. Unknown resource IDs are argument errors.
	t, err := task.InspectContext(ctx, in.TaskID, owner)
	if err != nil {
		if ctx.Err() != nil {
			return interrupted()
		}
		return nil, err
	}
	found := false
	for _, r := range t.Resources {
		if r.ID == in.ResourceID {
			if r.Kind != "process" {
				return nil, fmt.Errorf("task_wait currently supports process resources only")
			}
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("resource does not belong to this task")
	}
	result := condition.Wait(parent, time.Until(start.Add(time.Duration(timeout)*time.Millisecond)), 50*time.Millisecond, func(ctx context.Context) condition.Observation {
		t, err := task.InspectContext(ctx, in.TaskID, owner)
		if err != nil {
			return condition.Observation{Status: "unsupported", Evidence: map[string]string{"reason": "task state unavailable"}}
		}
		for _, r := range t.Resources {
			if r.ID == in.ResourceID {
				evidence := map[string]any{"task_state": t.State, "resource": r}
				if t.State != "active" || r.State == "closed" || r.State == "cleanup_failed" {
					return condition.Observation{Status: "failed", Evidence: evidence}
				}
				if r.State == "exited" && r.ExitCode != nil {
					status := "satisfied"
					if in.ExitCode != nil && *in.ExitCode != *r.ExitCode {
						status = "failed"
					}
					return condition.Observation{Status: status, Evidence: evidence}
				}
				if r.State == "missing" && r.LogState == "collecting" {
					return condition.Observation{Status: "pending", Evidence: evidence}
				}
				switch r.State {
				case "active", "activating", "starting":
					return condition.Observation{Status: "pending", Evidence: evidence}
				default:
					return condition.Observation{Status: "unsupported", Evidence: evidence}
				}
			}
		}
		return condition.Observation{Status: "unsupported", Evidence: map[string]string{"reason": "resource unavailable"}}
	})
	result.ElapsedMs = time.Since(start).Milliseconds()
	return textResult(result), nil
}

func observeProcessCondition(ctx context.Context, p predicate) condition.Observation {
	sample, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	t, err := task.InspectContext(sample, p.TaskID, session.Current().Owner())
	if err != nil {
		return condition.Observation{Status: "unsupported", Evidence: map[string]string{"reason": "process task unavailable"}}
	}
	for _, r := range t.Resources {
		if r.ID == p.ResourceID && r.Kind == "process" {
			evidence := map[string]any{"task_state": t.State, "resource": r}
			if t.State != "active" || r.State == "closed" {
				return condition.Observation{Status: "unsupported", Evidence: evidence}
			}
			if r.ExitCode != nil {
				status := "pending"
				if p.ExitCode == nil || *p.ExitCode == *r.ExitCode {
					status = "satisfied"
				}
				return condition.Observation{Status: status, Evidence: evidence}
			}
			if r.State == "active" || r.State == "starting" || r.State == "activating" || (r.State == "missing" && r.LogState == "collecting") {
				return condition.Observation{Status: "pending", Evidence: evidence}
			}
			return condition.Observation{Status: "unsupported", Evidence: evidence}
		}
	}
	return condition.Observation{Status: "unsupported", Evidence: map[string]string{"reason": "owned process unavailable"}}
}
