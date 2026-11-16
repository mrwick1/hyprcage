//go:build linux

package mcpserver

import (
	"encoding/json"
	"github.com/hexadecimil/offstage/internal/task"
	"testing"
)

func TestTaskTools(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	cs := connect(t)
	var created task.Task
	if err := json.Unmarshal([]byte(text(mustCall(t, cs, "task_create", nil))), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.State != "active" {
		t.Fatal(created)
	}
	mustCall(t, cs, "task_inspect", map[string]any{"task_id": created.ID})
	mustCall(t, cs, "task_close", map[string]any{"task_id": created.ID})
	mustCall(t, cs, "task_close", map[string]any{"task_id": created.ID})
}
