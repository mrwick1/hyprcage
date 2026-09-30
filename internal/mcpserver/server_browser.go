package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/browser"
	"github.com/hexadecimil/hyprcage/internal/devtools"
	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

type browserIn struct {
	Screen string `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
	URL    string `json:"url,omitempty" jsonschema:"page to open"`
}

type evalIn struct {
	Screen     string `json:"screen,omitempty"`
	Expression string `json:"expression" jsonschema:"JavaScript to run in the page; a promise is awaited"`
	Page       string `json:"page,omitempty" jsonschema:"part of the page URL, when more than one page is open"`
	TimeoutMs  int    `json:"timeout_ms,omitempty" jsonschema:"default 10000"`
}

type consoleIn struct {
	Screen string `json:"screen,omitempty"`
	Clear  bool   `json:"clear,omitempty" jsonschema:"empty the buffer after reading it"`
}

type traceIn struct {
	Screen string `json:"screen,omitempty"`
	Action string `json:"action" jsonschema:"start or stop"`
	Path   string `json:"path,omitempty" jsonschema:"stop only: where to write the trace, default ~/.cache/hyprcage/devtools/"`
}

type heapIn struct {
	Screen string `json:"screen,omitempty"`
	Page   string `json:"page,omitempty" jsonschema:"part of the page URL, when more than one page is open"`
	Path   string `json:"path,omitempty" jsonschema:"where to write the snapshot, default ~/.cache/hyprcage/devtools/"`
}

// registerBrowser adds browser_open and the devtools_* tools.
func (s *Server) registerBrowser(srv *mcp.Server) {
	tool(s, srv, "browser_open", "Open the agent's Chrome on a screen, with a throwaway profile and DevTools on a free 127.0.0.1 port of its own. Drive it with snapshot, act and find; read its internals with the devtools_* tools. It closes with its screen.", s.browserOpen)
	tool(s, srv, "devtools_eval", "Run JavaScript in a page of a screen opened with browser_open or app_launch debug=true, and return the value as JSON.", s.devtoolsEval)
	tool(s, srv, "devtools_console", "Read the console of a screen opened with browser_open or app_launch debug=true: console calls, uncaught exceptions and browser errors such as failed requests, oldest first.", s.devtoolsConsole)
	tool(s, srv, "devtools_trace", "Record a performance trace of a screen's browser: action start, do the work, then action stop. Stop writes a file that the DevTools Performance panel loads.", s.devtoolsTrace)
	tool(s, srv, "devtools_heap", "Take a heap snapshot of a page and write it to a file that the DevTools Memory panel loads. Compare two snapshots there to find a leak.", s.devtoolsHeap)
}

func (s *Server) browserOpen(in browserIn) (*mcp.CallToolResult, error) {
	rec, _, err := s.resolve(in.Screen, false)
	if err != nil {
		return nil, err
	}
	info, err := browser.Open(s.ctx, rec, in.URL)
	if err != nil {
		return nil, err
	}
	s.startDevtools(rec)
	return textResult(info), nil
}

// startDevtools connects at launch, so the console holds the page's
// messages from the start. A failure here is retried by the first
// devtools_* call.
func (s *Server) startDevtools(rec *registry.Screen) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.devtoolsFor(ctx, rec)
}

// devtoolsFor returns the screen's DevTools session, connecting when there
// is none or the app behind it changed. The port must still be held by the
// process that opened it, so a stranger's browser is never reached.
func (s *Server) devtoolsFor(ctx context.Context, rec *registry.Screen) (*devtools.Session, error) {
	if rec.DebugPort == 0 || !screen.OwnsPort(rec) {
		s.dropDevtools(rec)
		return nil, screen.Errf(screen.CodeCDP, "open the page with browser_open, or launch the app with debug=true",
			"screen %s has no app with a DevTools port", rec.Name)
	}
	k := tableKey(rec)
	if d := s.devtools[k]; d != nil && d.Alive() && d.Port() == rec.DebugPort {
		return d, nil
	}
	s.dropDevtools(rec)
	d, err := devtools.Open(ctx, rec.DebugPort)
	if err != nil {
		return nil, err
	}
	if s.devtools == nil {
		s.devtools = map[string]*devtools.Session{}
	}
	s.devtools[k] = d
	return d, nil
}

func (s *Server) dropDevtools(rec *registry.Screen) {
	k := tableKey(rec)
	if d := s.devtools[k]; d != nil {
		d.Close()
		delete(s.devtools, k)
	}
}

// devtoolsScreen resolves the screen and its session.
func (s *Server) devtoolsScreen(ctx context.Context, name string) (*registry.Screen, *devtools.Session, error) {
	rec, _, err := s.resolve(name, false)
	if err != nil {
		return nil, nil, err
	}
	d, err := s.devtoolsFor(ctx, rec)
	return rec, d, err
}

func (s *Server) devtoolsEval(in evalIn) (*mcp.CallToolResult, error) {
	if strings.TrimSpace(in.Expression) == "" {
		return nil, fmt.Errorf("expression must not be empty")
	}
	timeout := 10 * time.Second
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout+perceiveTimeout)
	defer cancel()
	_, d, err := s.devtoolsScreen(ctx, in.Screen)
	if err != nil {
		return nil, err
	}
	v, err := d.Eval(ctx, in.Page, in.Expression, timeout)
	if err != nil {
		return nil, err
	}
	return plain(string(v)), nil
}

func (s *Server) devtoolsConsole(in consoleIn) (*mcp.CallToolResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), perceiveTimeout)
	defer cancel()
	_, d, err := s.devtoolsScreen(ctx, in.Screen)
	if err != nil {
		return nil, err
	}
	msgs, dropped := d.Console(in.Clear)
	var b strings.Builder
	if dropped > 0 {
		fmt.Fprintf(&b, "(%d older messages dropped)\n", dropped)
	}
	for _, m := range msgs {
		fmt.Fprintf(&b, "[%s] %s", m.Level, m.Text)
		if m.Source != "" {
			fmt.Fprintf(&b, "  (%s)", m.Source)
		}
		b.WriteByte('\n')
	}
	if len(msgs) == 0 {
		b.WriteString("no console messages\n")
	}
	return plain(b.String()), nil
}

func (s *Server) devtoolsTrace(in traceIn) (*mcp.CallToolResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rec, d, err := s.devtoolsScreen(ctx, in.Screen)
	if err != nil {
		return nil, err
	}
	switch in.Action {
	case "start":
		if err := d.StartTrace(ctx); err != nil {
			return nil, err
		}
		return textResult(map[string]any{"tracing": true, "screen": rec.Name}), nil
	case "stop":
		path := in.Path
		if path == "" {
			path = devtools.DefaultPath(rec.Name, "trace.json")
		}
		n, err := d.StopTrace(ctx, path)
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{"path": path, "bytes": n}), nil
	default:
		return nil, fmt.Errorf("action must be start or stop, got %q", in.Action)
	}
}

func (s *Server) devtoolsHeap(in heapIn) (*mcp.CallToolResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	rec, d, err := s.devtoolsScreen(ctx, in.Screen)
	if err != nil {
		return nil, err
	}
	path := in.Path
	if path == "" {
		path = devtools.DefaultPath(rec.Name, "heapsnapshot")
	}
	n, err := d.HeapSnapshot(ctx, in.Page, path)
	if err != nil {
		return nil, err
	}
	return textResult(map[string]any{"path": path, "bytes": n}), nil
}
