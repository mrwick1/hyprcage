package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/perceive"
)

// guardServer serves a Server that the test can seed, over an in-memory transport.
func guardServer(t *testing.T, mut func(*config.Config)) (*Server, *mcp.ClientSession) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cfg := config.Default()
	if mut != nil {
		mut(&cfg)
	}
	s := newServer(cfg)
	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := s.mcpServer().Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return s, cs
}

func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error %v", tool, err)
	}
	return text(res)
}

func TestGuardBlocksDesktopCombos(t *testing.T) {
	_, cs := guardServer(t, nil)
	for _, k := range []string{"ctrl+w", "ctrl+q", "alt+F4"} {
		if out := call(t, cs, "desktop_key", map[string]any{"address": "0x1", "keys": []string{k}}); !strings.Contains(out, "ack_required") {
			t.Errorf("%s: %s", k, out)
		}
	}
	if out := call(t, cs, "desktop_key", map[string]any{"address": "0x1", "keys": []string{"ctrl+l"}}); strings.Contains(out, "ack_required") {
		t.Errorf("ctrl+l blocked: %s", out)
	}
}

func TestGuardAckRunsTheActionAndLogsIt(t *testing.T) {
	_, cs := guardServer(t, nil)
	args := map[string]any{"address": "0x1", "keys": []string{"ctrl+w"}}
	call(t, cs, "desktop_key", args)
	args["ack"] = "closing the scratch tab I opened"
	if out := call(t, cs, "desktop_key", args); strings.Contains(out, "ack_required") {
		t.Fatalf("ack did not unblock: %s", out)
	}
	raw, err := os.ReadFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "hyprcage", "actions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	ls := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(ls) != 2 || !strings.Contains(ls[0], `"error":"ack_required"`) || strings.Contains(ls[0], `"ack"`) {
		t.Errorf("blocked line: %v", ls)
	}
	if !strings.Contains(ls[1], `"ack":"closing the scratch tab I opened"`) || strings.Contains(ls[1], "ack_required") {
		t.Errorf("acknowledged line: %s", ls[1])
	}
}

func TestGuardBlocksDestructiveLabels(t *testing.T) {
	s, cs := guardServer(t, nil)
	s.tableAt(desktopKey("")).Assign([]perceive.Node{
		{Key: "k1", Role: "button", Name: "Delete file", X: 200, Y: 120},
		{Key: "k2", Role: "button", Name: "Save", X: 400, Y: 120},
	})
	for _, c := range []struct {
		tool    string
		args    map[string]any
		blocked bool
	}{
		{"click", map[string]any{"screen": "desktop", "x": 205, "y": 118}, true},
		{"double_click", map[string]any{"screen": "desktop", "x": 205, "y": 118}, true},
		{"click", map[string]any{"screen": "desktop", "x": 400, "y": 120}, false},
		{"click", map[string]any{"screen": "desktop", "x": 900, "y": 700}, false}, // no label there
		{"act", map[string]any{"screen": "desktop", "ref": "e1", "op": "click"}, true},
		{"act", map[string]any{"screen": "desktop", "ref": "e2", "op": "click"}, false},
		{"click", map[string]any{"screen": "desktop", "x": 205, "y": 118, "ack": "deleting the test file"}, false},
	} {
		if got := strings.Contains(call(t, cs, c.tool, c.args), "ack_required"); got != c.blocked {
			t.Errorf("%s %v: blocked=%v, want %v", c.tool, c.args, got, c.blocked)
		}
	}
}

func TestGuardAllowlist(t *testing.T) {
	_, cs := guardServer(t, func(c *config.Config) { c.Guard.Apps = []string{"kate"} })
	if out := call(t, cs, "click", map[string]any{"screen": "desktop", "x": 1, "y": 1}); !strings.Contains(out, "ack_required") {
		t.Errorf("desktop click with an unknown app: %s", out)
	}
	if out := call(t, cs, "desktop_type", map[string]any{"address": "0x1", "text": "x"}); !strings.Contains(out, "ack_required") {
		t.Errorf("desktop_type with an unknown app: %s", out)
	}
	if out := call(t, cs, "click", map[string]any{"screen": "desktop", "x": 1, "y": 1, "ack": "task needs it"}); strings.Contains(out, "ack_required") {
		t.Errorf("ack: %s", out)
	}
}

func TestGuardLeavesAgentScreensAlone(t *testing.T) {
	_, cs := guardServer(t, func(c *config.Config) { c.Guard.Apps = []string{"kate"} })
	for tool, args := range map[string]map[string]any{
		"key":   {"screen": "nope", "keys": []string{"ctrl+w"}},
		"click": {"screen": "nope", "x": 1, "y": 1},
		"batch": {"screen": "nope", "actions": []map[string]any{{"op": "key", "keys": []string{"ctrl+q"}}}},
	} {
		if out := call(t, cs, tool, args); strings.Contains(out, "ack_required") || !strings.Contains(out, "screen_not_found") {
			t.Errorf("%s: %s", tool, out)
		}
	}
}

func TestGuardAllCoversAgentScreens(t *testing.T) {
	_, cs := guardServer(t, func(c *config.Config) { c.Guard.Enabled = "all" })
	for tool, args := range map[string]map[string]any{
		"key":   {"screen": "nope", "keys": []string{"ctrl+w"}},
		"batch": {"screen": "nope", "actions": []map[string]any{{"op": "wait", "ms": 1}, {"op": "key", "keys": []string{"alt+F4"}}}},
	} {
		if out := call(t, cs, tool, args); !strings.Contains(out, "ack_required") {
			t.Errorf("%s: %s", tool, out)
		}
	}
}

func TestGuardOff(t *testing.T) {
	_, cs := guardServer(t, func(c *config.Config) { c.Guard.Enabled = "off" })
	if out := call(t, cs, "desktop_key", map[string]any{"address": "0x1", "keys": []string{"ctrl+w"}}); strings.Contains(out, "ack_required") {
		t.Errorf("off: %s", out)
	}
}
