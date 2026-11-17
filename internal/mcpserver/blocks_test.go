package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/offstage/internal/report"
	"github.com/hexadecimil/offstage/internal/screen"
)

func TestFailedCallsGoToTheBlockLog(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	goos = "linux" // test tools are registered on Linux only
	t.Cleanup(func() { goos = runtime.GOOS })
	srv := mcp.NewServer(&mcp.Implementation{Name: "t"}, nil)
	type in struct {
		Screen string `json:"screen,omitempty"`
	}
	unlocked(srv, "fails", "fails", func(in) (*mcp.CallToolResult, error) {
		return nil, screen.Errf(screen.CodeTimeout, "", "no frame from /home/joel/x")
	})
	unlocked(srv, "fine", "works", func(in) (*mcp.CallToolResult, error) { return plain("ok"), nil })
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	_, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "fails", Arguments: map[string]any{"screen": "hc-a"}})
	_, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "fine", Arguments: map[string]any{}})
	data, err := os.ReadFile(report.BlocksPath())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("block log has %d lines: %s", len(lines), data)
	}
	var b report.Block
	if err := json.Unmarshal([]byte(lines[0]), &b); err != nil {
		t.Fatal(err)
	}
	if b.Tool != "fails" || b.Code != string(screen.CodeTimeout) || b.Screen != "hc-a" || b.ActionID == "" || !strings.Contains(b.Message, "no frame") || strings.Contains(b.Message, "joel") {
		t.Errorf("block %+v", b)
	}
}
