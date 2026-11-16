package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/devtools"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestConditionValidation(t *testing.T) {
	for _, in := range []expectIn{{}, {Success: []predicate{{Kind: "bad"}}}, {Success: []predicate{{Kind: "element", Text: "["}}}, {Success: []predicate{{Kind: "request", Absent: true}}}} {
		if _, _, _, err := compilePredicates(in); err == nil {
			t.Fatal("invalid conditions accepted", in)
		}
	}
}
func TestScreenConditions(t *testing.T) {
	requireChrome(t)
	_, cs := guardServer(t, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			close(started)
			<-release
			return
		}
		if r.URL.Path == "/bad" {
			w.WriteHeader(500)
			return
		}
		if r.URL.Path == "/ok" {
			fmt.Fprint(w, "ok")
			return
		}
		fmt.Fprint(w, `<!doctype html><button onclick="fetch('/bad?token=fixture-secret').then(()=>document.querySelector('p').textContent='Failed')">Submit</button><p>Pending</p>`)
	}))
	defer web.Close()
	defer once.Do(func() { close(release) })
	rec := createHeadlessScreen(t, cs)
	mustCall(t, cs, "browser_open", map[string]any{"screen": rec.Name, "url": web.URL})
	call := func(args map[string]any) expectResult {
		t.Helper()
		args["screen"] = rec.Name
		var r expectResult
		if err := json.Unmarshal([]byte(text(mustCall(t, cs, "condition_wait", args))), &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	present := call(map[string]any{"success": []predicate{{Kind: "element", Text: "^Submit$", Role: "button"}}})
	if present.Status != "satisfied" {
		t.Fatal(present)
	}
	absent := call(map[string]any{"success": []predicate{{Kind: "element", Text: "^Missing$", Absent: true}}})
	if absent.Status != "satisfied" {
		t.Fatal(absent)
	}
	timed := call(map[string]any{"success": []predicate{{Kind: "element", Text: "^Missing$"}}, "timeout_ms": 30})
	if timed.Status != "timed_out" {
		t.Fatal(timed)
	}
	snapshot := text(mustCall(t, cs, "snapshot", map[string]any{"screen": rec.Name}))
	match := regexp.MustCompile(`\[([^]]+)\] button "Submit"`).FindStringSubmatch(snapshot)
	if len(match) != 2 {
		t.Fatal(snapshot)
	}
	failed := call(map[string]any{"success": []predicate{{Kind: "element", Text: "^Failed$"}}, "failure": []predicate{{Kind: "request", Text: "/bad", Failed: true}}, "action": map[string]any{"ref": match[1], "op": "click", "verify": false}})
	if failed.Status != "failed" || failed.ActionID == "" {
		t.Fatal(failed)
	}
	b, _ := json.Marshal(failed)
	if strings.Contains(string(b), "fixture-secret") {
		t.Fatal("query secret leaked")
	}
	var cursor devtools.Cursor
	if err := json.Unmarshal([]byte(text(mustCall(t, cs, "condition_cursor", map[string]any{"screen": rec.Name}))), &cursor); err != nil {
		t.Fatal(err)
	}
	mustCall(t, cs, "devtools_eval", map[string]any{"screen": rec.Name, "expression": "fetch('/ok')"})
	completed := call(map[string]any{"success": []predicate{{Kind: "request", Text: "/ok", Status: 200}}, "after": cursor})
	if completed.Status != "satisfied" {
		t.Fatal(completed)
	}
	// A request already in flight at the cursor must not satisfy a later action.
	mustCall(t, cs, "devtools_eval", map[string]any{"screen": rec.Name, "expression": "void fetch('/slow')"})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	if err := json.Unmarshal([]byte(text(mustCall(t, cs, "condition_cursor", map[string]any{"screen": rec.Name}))), &cursor); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	prior := call(map[string]any{"success": []predicate{{Kind: "request", Text: "/slow"}}, "after": cursor, "timeout_ms": 150})
	if prior.Status != "timed_out" {
		t.Fatal("old request satisfied condition", prior)
	}
	// A changed collector is an evidence gap, even if an older request matched.
	cursor.Source = "different"
	gap := call(map[string]any{"success": []predicate{{Kind: "request", Text: "/ok"}}, "after": cursor})
	if gap.Status != "unsupported" {
		t.Fatal(gap)
	}
	// Losing the source cannot prove absence.
	record, err := registry.Load(rec.Name)
	if err != nil {
		t.Fatal(err)
	}
	record.DebugPort = 0
	if err := registry.Save(record); err != nil {
		t.Fatal(err)
	}
	unavailable := call(map[string]any{"success": []predicate{{Kind: "request", Text: "/ok"}}})
	if unavailable.Status != "unsupported" {
		t.Fatal(unavailable)
	}
	record.DebugPort = rec.DebugPort
	if err := registry.Save(record); err != nil {
		t.Fatal(err)
	}
	done := make(chan *expectResult, 1)
	go func() {
		res, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "condition_wait", Arguments: map[string]any{"screen": rec.Name, "success": []predicate{{Kind: "element", Text: "Never"}}, "timeout_ms": 5000}})
		if e != nil {
			done <- nil
			return
		}
		var result expectResult
		_ = json.Unmarshal([]byte(text(res)), &result)
		done <- &result
	}()
	select {
	case <-done:
		t.Fatal("wait ended before cleanup")
	case <-time.After(75 * time.Millisecond):
	}
	mustCall(t, cs, "screen_destroy", map[string]any{"screen": rec.Name})
	select {
	case result := <-done:
		if result == nil || result.Status != "failed" {
			t.Fatal(result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup blocked by screen wait")
	}

}
func TestConditionWaitCancellation(t *testing.T) {
	s := newServer(config.Default())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := s.conditionWait(ctx, expectIn{Screen: "not-opened", Success: []predicate{{Kind: "element", Text: "ready"}}, TimeoutMs: 100})
	if err != nil {
		t.Fatal(err)
	}
	var r expectResult
	_ = json.Unmarshal([]byte(text(res)), &r)
	if r.Status != "cancelled" {
		t.Fatal(r)
	}
	s.mu.Lock()
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	defer s.mu.Unlock()
	if err := s.conditionLock(ctx); err == nil {
		t.Fatal("lock ignored deadline")
	}
}
