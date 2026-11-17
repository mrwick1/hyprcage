package mcpserver

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/offstage/internal/record"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/session"
	"github.com/hexadecimil/offstage/internal/update"
	"github.com/hexadecimil/offstage/internal/version"
)

// quietFor is how long no request may have ended before the server exits:
// the reply of the last request is on stdout by then.
const quietFor = 2 * time.Second

// activity counts the requests in flight and notes when the last one ended.
type activity struct {
	inflight atomic.Int32
	lastDone atomic.Int64 // unix nano
}

func (a *activity) begin() { a.inflight.Add(1) }
func (a *activity) end()   { a.lastDone.Store(time.Now().UnixNano()); a.inflight.Add(-1) }

// updateNotice tells the agent, once, that a new build is staged.
var updateNotice atomic.Pointer[string]

// middleware counts every request and appends the update notice to the next
// tool result.
func (a *activity) middleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		a.begin()
		defer a.end()
		res, err := next(ctx, method, req)
		if r, ok := res.(*mcp.CallToolResult); ok && err == nil && r != nil && !r.IsError {
			if n := updateNotice.Swap(nil); n != nil {
				r.Content = append(r.Content, &mcp.TextContent{Text: *n})
			}
		}
		return res, err
	}
}

func mine(o registry.Owner, id session.Identity) bool {
	return (o.PID == id.PID && o.PIDStart == id.PIDStart) || (id.SessionID != "" && o.SessionID == id.SessionID)
}

// busy says why the server must not exit now, or "" when it is idle: no
// request in flight, none ended lately, no screen, recording, task or Bare
// session of this session.
func (s *Server) busy(ctx context.Context) string {
	if s.activity.inflight.Load() > 0 {
		return "a request is running"
	}
	if time.Since(time.Unix(0, s.activity.lastDone.Load())) < quietFor {
		return "a request just ended"
	}
	id := session.Current()
	recs, _ := registry.List()
	for _, r := range recs {
		if mine(r.Owner, id) {
			return "screen " + r.Name
		}
	}
	recordings, _ := record.List()
	for _, r := range recordings {
		if mine(r.Owner, id) {
			return "a recording"
		}
	}
	if tasksActive(id.Owner()) {
		return "a task"
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, b := range s.bare {
		res, err := b.CallTool(cctx, &mcp.CallToolParams{Name: "sessions", Arguments: map[string]any{}})
		if err != nil || res.IsError || len(res.Content) != 1 {
			return "a browser session"
		}
		if t, ok := res.Content[0].(*mcp.TextContent); !ok || strings.TrimSpace(t.Text) != "no live sessions" {
			return "a browser session"
		}
	}
	return ""
}

func envDuration(name string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(name)); err == nil && d > 0 {
		return d
	}
	return def
}

// selfUpdate checks for a newer build once after a short delay and then about
// hourly, and calls restart when a new build is on disk and the server is
// idle. It returns at once when updates are off.
func (s *Server) selfUpdate(ctx context.Context, restart func()) {
	current := version.Version
	if versionForTest != "" {
		current = versionForTest
	}
	if !s.cfg.Update.Auto || os.Getenv("OFFSTAGE_NO_AUTO_UPDATE") == "1" || !update.Eligible(current) {
		return
	}
	exe := exeForTest
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return
		}
	}
	update.Cleanup(exe)
	opts := update.Options{Exe: exe, Current: current, Base: os.Getenv("OFFSTAGE_UPDATE_BASE")}
	watch := update.NewWatch(exe)
	interval := envDuration("OFFSTAGE_UPDATE_INTERVAL", time.Hour)
	next := time.Now().Add(envDuration("OFFSTAGE_UPDATE_DELAY", time.Minute))
	// Claude Code starts a server that exited (verified); Codex does not, and
	// the other clients are unverified: they only get the files and the notice.
	restarts := session.Current().Client == "claude-code" || os.Getenv("OFFSTAGE_UPDATE_RESTART") == "1" || restartForTest
	staged := false
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if !staged && !time.Now().Before(next) {
			next = time.Now().Add(interval + time.Duration(rand.Int63n(int64(interval)/5+1)))
			res, err := update.Check(opts)
			switch {
			case err != nil:
				fmt.Fprintf(os.Stderr, "offstage: update check: %v\n", err)
			case res.Version != "":
				stage(fmt.Sprintf("offstage updated to %s", res.Version), restarts)
			case res.Bare:
				stage("offstage updated its browser (Bare)", restarts)
			}
		}
		if !staged && watch.Changed() {
			staged = true
			stage("offstage was updated on disk", restarts) // a no-op when Check just said it
		}
		if staged && restarts && s.busy(ctx) == "" {
			fmt.Fprintln(os.Stderr, "offstage: restarting on the new build")
			restart()
			return
		}
	}
}

// Tests replace the running version and binary path.
var (
	versionForTest, exeForTest string
	restartForTest             bool
)

// stage sets the one-line notice for the agent, unless one is waiting.
func stage(what string, restarts bool) {
	n := what + "; the server restarts when idle."
	if !restarts {
		n = what + "; restart the MCP server to use it."
	}
	updateNotice.CompareAndSwap(nil, &n)
}
