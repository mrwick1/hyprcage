package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/config"
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
		"desktop_windows", "desktop_focus", "desktop_move", "desktop_type", "desktop_key", "browser_open",
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

func TestBrowserOpenNamesConfiguredPort(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	cfg := config.Default()
	cfg.BrowserPort = 9333
	res, err := connectCfg(t, cfg).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range res.Tools {
		if tl.Name == "browser_open" && !strings.Contains(tl.Description, "127.0.0.1:9333") {
			t.Errorf("browser_open description: %q", tl.Description)
		}
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
	for _, name := range []string{"snapshot", "act", "find"} {
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
	if err == nil && !res.IsError {
		t.Error("act without ref should be rejected")
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
