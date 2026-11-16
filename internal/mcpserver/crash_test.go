package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/offstage/internal/report"
)

func TestPanicBecomesAnInternalErrorAndACrashFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "t"}, nil)
	type in struct{}
	unlocked(srv, "boom", "panics", func(in) (*mcp.CallToolResult, error) { panic("kaput") })
	unlocked(srv, "fine", "works", func(in) (*mcp.CallToolResult, error) { return plain("ok"), nil })
	note := "crash waits"
	crashNotice.Store(&note)
	defer crashNotice.Store(nil)
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

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "boom", Arguments: map[string]any{}})
	if err != nil || !res.IsError || !strings.Contains(resultText(res), "internal: offstage panicked in boom: kaput") {
		t.Fatalf("res %v, err %v", res, err)
	}
	if len(report.Crashes()) != 1 {
		t.Fatalf("crash files %v", report.Crashes())
	}
	res, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "fine", Arguments: map[string]any{}})
	if !strings.Contains(resultText(res), "crash waits") {
		t.Fatalf("first good result lacks the crash notice: %s", resultText(res))
	}
	res, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "fine", Arguments: map[string]any{}})
	if strings.Contains(resultText(res), "crash waits") {
		t.Fatal("the crash notice came twice")
	}
}
