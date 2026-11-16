//go:build linux

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/session"
	"github.com/hexadecimil/offstage/internal/sysd"
	"github.com/hexadecimil/offstage/internal/task"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestTaskFailureTimeline(t *testing.T) {
	requireChrome(t)
	if !sysd.Available() {
		t.Skip("user systemd unavailable")
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	s, cs := guardServer(t, nil)
	// systemctl resolves its user-manager socket through XDG_RUNTIME_DIR.
	// Screen names are generated uniquely; cleanup removes only this task's records.
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	var owned task.Task
	if err := json.Unmarshal([]byte(text(mustCall(t, cs, "task_create", nil))), &owned); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hcall(t, cs, "task_close", map[string]any{"task_id": owned.ID}) })
	mustCall(t, cs, "task_launch", map[string]any{"task_id": owned.ID, "command": []string{"/bin/sh", "-c", "echo 'fixture process error'; exit 7"}})
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/submit" {
			w.WriteHeader(500)
			return
		}
		fmt.Fprint(w, `<!doctype html><button onclick="console.error('fixture request error');fetch('/submit?token=fixture-secret').then(()=>document.querySelector('p').textContent='Failed')">Save</button><p>Ready</p>`)
	}))
	defer fixture.Close()
	rec := createHeadlessScreen(t, cs)
	if err := task.Attach(owned.ID, session.Current().Owner(), rec); err != nil {
		t.Fatal(err)
	}
	created := struct{ Screen string }{rec.Name}
	openNativeTestPage(t, s, rec, fixture.URL)
	snapshot := text(mustCall(t, cs, "snapshot", map[string]any{"screen": created.Screen}))
	ref := regexp.MustCompile(`\[([^]]+)\] button "Save"`).FindStringSubmatch(snapshot)
	if len(ref) != 2 {
		t.Fatal(snapshot)
	}
	var outcome expectResult
	result := mustCall(t, cs, "condition_wait", map[string]any{"screen": created.Screen, "success": []predicate{{Kind: "element", Text: "Failed"}}, "failure": []predicate{{Kind: "request", Text: "/submit", Failed: true}}, "action": map[string]any{"ref": ref[1], "op": "click", "verify": false}})
	if err := json.Unmarshal([]byte(text(result)), &outcome); err != nil {
		t.Fatal(err)
	}
	if outcome.Status != "failed" {
		t.Fatal(outcome)
	}
	read := func(args map[string]any) task.TimelinePage {
		t.Helper()
		args["task_id"] = owned.ID
		args["limit"] = 200
		var page task.TimelinePage
		if err := json.Unmarshal([]byte(text(mustCall(t, cs, "task_timeline", args))), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	page := read(map[string]any{})
	kinds := map[string]bool{}
	for _, ev := range page.Events {
		kinds[ev.Kind] = true
	}
	for _, kind := range []string{"act", "condition_wait", "request", "console", "log_excerpt", "process_exit", "page_state"} {
		if !kinds[kind] {
			t.Fatalf("missing %s: %v", kind, kinds)
		}
	}
	data, _ := json.Marshal(page)
	if strings.Contains(string(data), "fixture-secret") {
		t.Fatal("query secret persisted")
	}
	linked := read(map[string]any{"action_id": outcome.ActionID})
	if len(linked.Events) < 2 {
		t.Fatal("action outcome and failure state not linked", linked)
	}
	mustCall(t, cs, "task_close", map[string]any{"task_id": owned.ID})
	closed := read(map[string]any{})
	if len(closed.Events) < len(page.Events) {
		t.Fatal("evidence lost on close")
	}
}

func TestTimelineDoesNotReadBareDiagnosticCursors(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	owner := session.Current().Owner()
	owned, err := task.Create(owner)
	if err != nil {
		t.Fatal(err)
	}
	rec := &registry.Screen{Name: "hc-bare-evidence", CreatedAt: time.Now(), Owner: owner}
	if err := task.Attach(owned.ID, owner, rec); err != nil {
		t.Fatal(err)
	}
	s := newServer(config.Default())
	// A sentinel client has no transport. Any diagnostic call would panic.
	s.bare = map[string]*mcp.ClientSession{tableKey(rec): {}}
	s.bareSessionHints = map[string]string{tableKey(rec): "main"}
	if _, err := s.taskTimeline(context.Background(), timelineIn{TaskID: owned.ID}); err != nil {
		t.Fatal(err)
	}
}
