package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hexadecimil/offstage/internal/bare"
	"github.com/hexadecimil/offstage/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestBareBrowser exercises the real bundled engine. No Chrome or desktop is used.
func TestBareBrowser(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if _, err := bare.Find(); err != nil {
		t.Skip(err)
	}
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<title>Bare check</title><input aria-label="Name"><button onclick="document.getElementById('out').textContent='saved'">Save</button><p id="out">ready</p>`))
	}))
	defer page.Close()
	s := newServer(config.Default())
	srv := s.mcpServer()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.registerBare(ctx, srv); err != nil {
		t.Fatal(err)
	}
	defer s.closeBare()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, tool := range tools.Tools {
		found[tool.Name] = true
	}
	for _, name := range []string{"browser_open", "browser_snapshot", "browser_act", "browser_run_steps", "browser_close"} {
		if !found[name] {
			t.Fatal("missing " + name)
		}
	}
	for _, name := range []string{"devtools_audit", "devtools_trace", "devtools_eval"} {
		if found[name] {
			t.Fatal("Chrome tool registered: " + name)
		}
	}
	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Fatalf("%s: %s", name, text(res))
		}
		return res
	}
	// A caller cannot open a window on the human's desktop, even if it asks.
	call("browser_open", map[string]any{"url": page.URL, "session": "one", "window": true})
	snap := text(call("browser_snapshot", map[string]any{"session": "one"}))
	if !strings.Contains(snap, "Save") {
		t.Fatal(snap)
	}
	call("browser_open", map[string]any{"url": page.URL, "session": "two"})
	call("browser_step", map[string]any{"session": "two", "steps": "type label=\"Name\" \"Arjun\"\nclick role=button name=\"Save\"\nexpect css=\"#out\" text=\"saved\""})
	if got := text(call("browser_read", map[string]any{"session": "two"})); !strings.Contains(got, "saved") {
		t.Fatal(got)
	}
	// The other live session retains its own DOM.
	read := text(call("browser_read", map[string]any{"session": "one"}))
	if !strings.Contains(read, "ready") || strings.Contains(read, "saved") {
		t.Fatal(read)
	}
	// Screenshot content survives the bridge, including images.
	shot := call("browser_screenshot", map[string]any{"session": "one", "inline": true})
	image := false
	for _, c := range shot.Content {
		if _, ok := c.(*mcp.ImageContent); ok {
			image = true
		}
	}
	if !image {
		t.Fatal("screenshot image missing")
	}
	for _, session := range []string{"one", "two"} {
		closed := text(call("browser_close", map[string]any{"session": session}))
		if !strings.Contains(closed, `"window":false`) {
			t.Fatal("expected headless session: " + closed)
		}
	}
	if result := text(call("browser_sessions", map[string]any{})); !strings.Contains(result, "no live sessions") {
		t.Fatal(result)
	}
	raw, err := os.ReadFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "offstage", "actions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Arjun") || strings.Contains(string(raw), page.URL) {
		t.Fatal("browser arguments leaked into audit log")
	}
	failed, failure := cs.CallTool(ctx, &mcp.CallToolParams{Name: "browser_snapshot", Arguments: map[string]any{"screen": ""}})
	if failure != nil || failed == nil || !failed.IsError {
		t.Fatalf("Bare bridge errors need isError=true: result=%v error=%v", failed, failure)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "screen_create", Arguments: map[string]any{"kind": "headless"}})
	if err != nil || !res.IsError || !strings.Contains(text(res), "Bare") {
		t.Fatalf("old headless API: %v %v", res, err)
	}
}

// TestBareVisible uses the built executable so cage can run its _holder command.
func TestBareVisible(t *testing.T) {
	bin := os.Getenv("OFFSTAGE_TEST_BINARY")
	if bin == "" {
		t.Skip("set OFFSTAGE_TEST_BINARY to a built offstage on Hyprland")
	}
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<title>Bare visible check</title><button>Ready</button>`))
	}))
	defer page.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, &mcp.CommandTransport{Command: exec.Command(bin, "mcp")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Fatalf("%s: %s", name, text(res))
		}
		return res
	}
	created := call("screen_create", map[string]any{"mirror": false})
	var rec struct {
		Screen string `json:"screen"`
	}
	if err := json.Unmarshal([]byte(text(created)), &rec); err != nil {
		t.Fatal(err)
	}
	destroyed := false
	defer func() {
		if !destroyed {
			_, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "screen_destroy", Arguments: map[string]any{"screen": rec.Screen}})
		}
	}()
	call("browser_open", map[string]any{"screen": rec.Screen, "session": "visible", "url": page.URL})
	snapshot := text(call("browser_snapshot", map[string]any{"screen": rec.Screen, "session": "visible"}))
	if !strings.Contains(snapshot, "Ready") {
		t.Fatal(snapshot)
	}
	call("screen_destroy", map[string]any{"screen": rec.Screen})
	destroyed = true
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "browser_snapshot", Arguments: map[string]any{"screen": rec.Screen, "session": "visible"}})
	if err == nil && !res.IsError {
		t.Fatal("destroyed screen still accessible")
	}
}
