//go:build linux

package mcpserver

import (
	"context"
	"encoding/json"
	"github.com/hexadecimil/offstage/internal/condition"
	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/session"
	"github.com/hexadecimil/offstage/internal/sysd"
	"github.com/hexadecimil/offstage/internal/task"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
	"time"
)

func TestProcessWait(t *testing.T) {
	if !sysd.Available() {
		t.Skip("user systemd unavailable")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	owner := session.Current().Owner()
	owned, err := task.Create(owner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = task.Close(owned.ID, owner, nil) })
	s := newServer(config.Default())
	resource, err := task.Launch(owned.ID, owner, []string{"/bin/sh", "-c", "exit 7"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []int{7, 0} {
		res, err := s.taskWait(context.Background(), taskWaitIn{TaskID: owned.ID, ResourceID: resource.ID, ExitCode: &expected, TimeoutMs: 1000})
		if err != nil {
			t.Fatal(err)
		}
		var result condition.Result
		if err := json.Unmarshal([]byte(text(res)), &result); err != nil {
			t.Fatal(err)
		}
		want := "satisfied"
		if expected == 0 {
			want = "failed"
		}
		if result.Status != want {
			t.Fatal(result)
		}
	}
	successful, err := task.Launch(owned.ID, owner, []string{"/bin/true"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := s.taskWait(context.Background(), taskWaitIn{TaskID: owned.ID, ResourceID: successful.ID, TimeoutMs: 1000})
	if err != nil {
		t.Fatal(err)
	}
	var success condition.Result
	if err := json.Unmarshal([]byte(text(completed)), &success); err != nil {
		t.Fatal(err)
	}
	if success.Status != "satisfied" {
		t.Fatal(success)
	}
	running, err := task.Launch(owned.ID, owner, []string{"/bin/sleep", "120"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	cs := connect(t)
	done := make(chan string, 1)
	go func() {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "task_wait", Arguments: map[string]any{"task_id": owned.ID, "resource_id": running.ID, "timeout_ms": 10000}})
		if err != nil {
			done <- err.Error()
			return
		}
		var r condition.Result
		_ = json.Unmarshal([]byte(text(res)), &r)
		done <- r.Status
	}()
	select {
	case result := <-done:
		t.Fatalf("wait returned before cleanup: %s", result)
	case <-time.After(75 * time.Millisecond):
	}
	start := time.Now()
	mustCall(t, cs, "task_close", map[string]any{"task_id": owned.ID})
	if time.Since(start) > 5*time.Second {
		t.Fatal("task close was blocked by wait")
	}
	select {
	case status := <-done:
		if status != "failed" {
			t.Fatal(status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("wait blocked task close")
	}
}
