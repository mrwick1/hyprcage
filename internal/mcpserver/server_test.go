package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/perceive"
	"github.com/hexadecimil/hyprcage/internal/registry"
)

func connect(t *testing.T) *mcp.ClientSession { return connectCfg(t, config.Default()) }

func connectCfg(t *testing.T, cfg config.Config) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	srv := newServer(cfg).mcpServer()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestToolsRegistered(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"screen_create", "screen_destroy", "screen_list", "mirror", "app_launch", "app_close", "windows",
		"screenshot", "click", "double_click", "move", "scroll", "drag", "type", "key", "wait", "batch", "setup",
		"record_start", "record_stop", "clipboard_get", "clipboard_set",
		"desktop_windows", "desktop_focus", "desktop_move", "desktop_type", "desktop_key", "desktop_workspace", "browser_open", "devtools_eval", "devtools_console", "devtools_trace", "devtools_heap",
		"snapshot", "act", "find"}
	got := map[string]*mcp.Tool{}
	for _, tl := range res.Tools {
		got[tl.Name] = tl
	}
	for _, name := range want {
		if got[name] == nil {
			t.Errorf("tool %s missing", name)
		}
	}
	if len(res.Tools) != len(want) {
		t.Errorf("%d tools, want %d", len(res.Tools), len(want))
	}
	// x and y are required for click, screen is not (cahier F14).
	schema, _ := got["click"].InputSchema.(map[string]any)
	req, _ := schema["required"].([]any)
	var reqs []string
	for _, r := range req {
		reqs = append(reqs, r.(string))
	}
	joined := strings.Join(reqs, ",")
	if !strings.Contains(joined, "x") || !strings.Contains(joined, "y") || strings.Contains(joined, "screen") {
		t.Errorf("click required = %v", reqs)
	}
}

func TestScreenListEmpty(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	cs := connect(t)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "screen_list", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || strings.TrimSpace(text(res)) != "[]" {
		t.Errorf("screen_list: isError=%v text=%q", res.IsError, text(res))
	}
}

// A screen needs no Hyprland any more: without one, an action on a screen
// that does not exist is an ordinary not-found tool error, not a protocol
// error.
func TestActionWithoutHyprlandIsToolError(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	cs := connect(t)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "click", Arguments: map[string]any{"x": 1, "y": 2}})
	if err != nil {
		t.Fatalf("protocol error instead of tool error: %v", err)
	}
	if !res.IsError || !strings.Contains(text(res), "screen_not_found") {
		t.Errorf("isError=%v text=%q", res.IsError, text(res))
	}
}

func TestInvalidArgumentsAreRejected(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	cs := connect(t)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "click", Arguments: map[string]any{"x": 1}})
	if err == nil && !res.IsError {
		t.Error("missing y should be rejected")
	}
}

func TestPerceiveToolsListed(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	res, err := connect(t).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tl := range res.Tools {
		got[tl.Name] = true
	}
	for _, name := range []string{"snapshot", "act", "find", "devtools_eval", "devtools_console", "devtools_trace", "devtools_heap"} {
		if !got[name] {
			t.Errorf("tool %s missing", name)
		}
	}
}

func TestSnapshotUnknownScreen(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	res, err := connect(t).CallTool(context.Background(), &mcp.CallToolParams{Name: "snapshot", Arguments: map[string]any{"screen": "nope"}})
	if err != nil {
		t.Fatalf("protocol error instead of tool error: %v", err)
	}
	if !res.IsError || !strings.Contains(text(res), "screen_not_found") {
		t.Errorf("isError=%v text=%q", res.IsError, text(res))
	}
}

func TestActMissingRef(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	res, err := connect(t).CallTool(context.Background(), &mcp.CallToolParams{Name: "act", Arguments: map[string]any{"op": "click"}})
	msg := ""
	if err != nil {
		msg = err.Error()
	} else if res.IsError {
		msg = text(res)
	}
	if !strings.Contains(msg, "ref") {
		t.Errorf("act without ref: err=%v text=%q", err, msg)
	}
}

func TestSnapshotBadMode(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	res, err := connect(t).CallTool(context.Background(), &mcp.CallToolParams{Name: "snapshot", Arguments: map[string]any{"mode": "tiny"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(text(res), "unknown mode") {
		t.Errorf("isError=%v text=%q", res.IsError, text(res))
	}
}

// screenDestroy needs a live screen, so this tests the table key and
// dropTable, which screenDestroy calls.
func TestTablePerScreenInstance(t *testing.T) {
	s := newServer(config.Default())
	a := &registry.Screen{Name: "hc-1", CreatedAt: time.Unix(1, 0)}
	b := &registry.Screen{Name: "hc-1", CreatedAt: time.Unix(2, 0)}
	ta := s.table(a)
	if s.table(a) != ta {
		t.Error("same screen, new table")
	}
	if s.table(b) == ta {
		t.Error("reused name inherits the old table")
	}
	s.dropTable(a)
	if _, ok := s.tables[tableKey(a)]; ok {
		t.Error("dropTable left the table")
	}
	if _, ok := s.tables[tableKey(b)]; !ok {
		t.Error("dropTable removed another screen's table")
	}
}

func TestDesktopTablesAreSeparate(t *testing.T) {
	s := newServer(config.Default())
	s.tableAt(desktopKey("0xa")).Assign([]perceive.Node{{Key: "atspi:1:/b", Role: "button", Name: "Back"}})
	if _, ok := s.tableAt(desktopKey("0xa")).Lookup("e1"); !ok {
		t.Fatal("e1 unknown on its own table")
	}
	for _, tb := range []*perceive.Table{
		s.tableAt(desktopKey("0xb")),
		s.tableAt(desktopKey("")),
		s.table(&registry.Screen{Name: "hc-1", CreatedAt: time.Unix(1, 0)}),
	} {
		if _, ok := tb.Lookup("e1"); ok {
			t.Error("a ref from desktop:0xa resolves on another table")
		}
	}
}

func TestFindInvalidRegexp(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	res, err := connect(t).CallTool(context.Background(), &mcp.CallToolParams{Name: "find", Arguments: map[string]any{"text": "("}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(text(res), "regular expression") {
		t.Errorf("isError=%v text=%q", res.IsError, text(res))
	}
}

func TestDesktopRefusedByScreenTools(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	cs := connect(t)
	for _, c := range []struct {
		tool string
		args map[string]any
		hint string
	}{
		{"key", map[string]any{"keys": []string{"Return"}}, "desktop_key"},
		{"type", map[string]any{"text": "a"}, "desktop_type"},
		{"windows", nil, "desktop_windows"},
		{"wait", map[string]any{"ms": 1}, "no desktop"},
		{"batch", map[string]any{"actions": []any{}}, "desktop_"},
		{"app_close", map[string]any{"toplevel": 1}, "no desktop"},
		{"clipboard_get", nil, "no desktop"},
		{"clipboard_set", map[string]any{"text": "a"}, "no desktop"},
		{"mirror", nil, "no desktop"},
		{"act", map[string]any{"ref": "e1", "op": "click"}, "later build"},
		{"screen_create", nil, "reserved"},
	} {
		args := map[string]any{"screen": "desktop"}
		if c.tool == "screen_create" {
			args = map[string]any{"name": "desktop"}
		}
		for k, v := range c.args {
			args[k] = v
		}
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: c.tool, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", c.tool, err)
		}
		got := text(res)
		if !res.IsError || !strings.Contains(got, c.hint) {
			t.Errorf("%s: isError=%v text=%q, want %q", c.tool, res.IsError, got, c.hint)
		}
		if c.tool != "screen_create" && !strings.Contains(got, "unsupported_input") {
			t.Errorf("%s: %q, want unsupported_input", c.tool, got)
		}
	}
}

func TestDesktopLaunchRefusesDebug(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	res, err := connect(t).CallTool(context.Background(), &mcp.CallToolParams{Name: "app_launch",
		Arguments: map[string]any{"screen": "desktop", "command": []string{"chromium"}, "debug": true}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(text(res), "unsupported_input") {
		t.Errorf("isError=%v text=%q", res.IsError, text(res))
	}
}
