package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/offstage/internal/audit"
	"github.com/hexadecimil/offstage/internal/perceive"
	"github.com/hexadecimil/offstage/internal/report"
	"github.com/hexadecimil/offstage/internal/screen"
)

func entry(tool, screen, code string) audit.Entry {
	return audit.Entry{Tool: tool, Screen: screen, Error: code}
}

func TestAutoReportTriggers(t *testing.T) {
	type step struct {
		e    audit.Entry
		want string // type of the report filed after this call, "" for none
	}
	noop := entry("click", "hc-a", "")
	noop.Verdict = &perceive.Verification{Verdict: perceive.NoOp}
	for _, tc := range []struct {
		name  string
		steps []step
	}{
		{"internal error files a bug", []step{{entry("act", "hc-a", "internal"), "bug"}}},
		{"capture failure files a bug", []step{{entry("screenshot", "hc-a", "capture_failed"), "bug"}}},
		{"same fingerprint once", []step{{entry("act", "hc-a", "internal"), "bug"}, {entry("act", "hc-a", "internal"), ""}}},
		{"argument and guard errors never file", []step{
			{entry("click", "hc-a", "invalid_name"), ""}, {entry("click", "hc-a", "invalid_name"), ""}, {entry("click", "hc-a", "invalid_name"), ""},
			{entry("click", "desktop", "ack_required"), ""}, {entry("click", "desktop", "system_prompt"), ""}, {entry("x", "hc-a", "not_owner"), ""},
		}},
		{"three failures in a row file a doubt", []step{
			{entry("find", "hc-a", "timeout"), ""}, {entry("find", "hc-a", "timeout"), ""}, {entry("find", "hc-a", "timeout"), "doubt"},
		}},
		{"a success resets the streak", []step{
			{entry("find", "hc-a", "timeout"), ""}, {entry("find", "hc-a", "timeout"), ""}, {entry("find", "hc-a", ""), ""}, {entry("find", "hc-a", "timeout"), ""},
		}},
		{"no-op verdicts count as failures", []step{{noop, ""}, {noop, ""}, {noop, "doubt"}}},
		{"desktop after a screen failure files a suggestion", []step{
			{entry("find", "hc-a", "timeout"), ""}, {entry("desktop_key", "", ""), "suggestion"}, {entry("desktop_key", "", ""), ""},
		}},
		{"desktop without a failure files nothing", []step{{entry("desktop_key", "", ""), ""}}},
		{"report_issue is never observed", []step{{entry("report_issue", "hc-a", "internal"), ""}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAutoState()
			for i, st := range tc.steps {
				got := ""
				if in := a.decide(st.e, "msg"); in != nil {
					got = in.Type
					if !strings.HasPrefix(in.Title, autoTitlePrefix) || (st.e.Error != "" && !strings.Contains(in.Actual, "msg")) {
						t.Errorf("step %d: report %+v", i, in)
					}
				}
				if got != st.want {
					t.Errorf("step %d (%s %s): filed %q, want %q", i, st.e.Tool, st.e.Error, got, st.want)
				}
			}
		})
	}
}

func TestAutoReportLimit(t *testing.T) {
	a := newAutoState()
	n := 0
	for i := range 3 * autoMax {
		if a.decide(entry("tool"+string(rune('a'+i%26))+strings.Repeat("x", i/26), "hc-a", "internal"), "msg") != nil {
			n++
		}
	}
	if n != autoMax {
		t.Errorf("filed %d, want %d", n, autoMax)
	}
}

func TestAnInternalErrorIsQueuedAsAnAutomaticReport(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("OFFSTAGE_BETA_KEY", "")
	autoReports, autoEnabled = newAutoState(), true
	defer func() { autoEnabled = false }()
	srv := mcp.NewServer(&mcp.Implementation{Name: "t"}, nil)
	type in struct{}
	unlocked(srv, "breaks", "fails", func(in) (*mcp.CallToolResult, error) {
		return nil, screen.Errf(screen.CodeInternal, "", "offstage broke")
	})
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
	_, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "breaks", Arguments: map[string]any{}})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		metas, _ := filepath.Glob(filepath.Join(report.QueueDir(), "*", "meta.json"))
		if len(metas) == 1 {
			data, _ := os.ReadFile(metas[0])
			if !strings.Contains(string(data), `"title": "[auto] breaks failed with internal"`) || !strings.Contains(string(data), "offstage broke") {
				t.Errorf("meta: %s", data)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no automatic report in the queue")
}
