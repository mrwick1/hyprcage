package mcpserver

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/desktop"
	"github.com/hexadecimil/hyprcage/internal/hypr"
	"github.com/hexadecimil/hyprcage/internal/perceive"
	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

type snapIn struct {
	Screen   string `json:"screen,omitempty"`
	Mode     string `json:"mode,omitempty" jsonschema:"interactive (default: controls, landmarks, headings) or full"`
	Root     string `json:"root,omitempty" jsonschema:"ref of a subtree to read instead of the whole screen"`
	MaxNodes int    `json:"max_nodes,omitempty" jsonschema:"cap on the elements returned, default 300"`
	Source   string `json:"source,omitempty" jsonschema:"auto (default), cdp, atspi or ocr"`
	Window   string `json:"window,omitempty" jsonschema:"screen desktop only: a window address from desktop_windows; default every window on a visible workspace"`
}

type actIn struct {
	Screen          string   `json:"screen,omitempty"`
	Ref             string   `json:"ref"`
	Op              string   `json:"op" jsonschema:"click, double_click, type, key, hover or scroll"`
	Text            string   `json:"text,omitempty"`
	Keys            []string `json:"keys,omitempty"`
	Direction       string   `json:"direction,omitempty"`
	ScreenshotAfter bool     `json:"screenshot_after,omitempty"`
}

type findIn struct {
	Screen    string `json:"screen,omitempty"`
	Text      string `json:"text" jsonschema:"regular expression on the element name or value"`
	Role      string `json:"role,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty" jsonschema:"wait up to this long for a match, default 0"`
	Window    string `json:"window,omitempty" jsonschema:"screen desktop only: a window address from desktop_windows; default every window on a visible workspace"`
}

const perceiveTimeout = 30 * time.Second

// registerPerceive adds snapshot, act and find.
func (s *Server) registerPerceive(srv *mcp.Server) {
	tool(s, srv, "snapshot", "List the elements of a screen as text with refs and screen coordinates. Read this before a screenshot: it costs a fraction of the tokens.", s.snapshot)
	tool(s, srv, "act", "Click, type, press keys, hover or scroll on an element by its ref from snapshot. Returns what changed on the screen as text.", s.act)
	tool(s, srv, "find", "Find elements by name or value, optionally waiting until one appears. Use it instead of wait plus screenshot.", s.find)
}

// tableKey names one screen instance: a reused screen name never inherits
// the refs of an older screen.
func tableKey(rec *registry.Screen) string {
	return fmt.Sprintf("%s@%d", rec.Name, rec.CreatedAt.UnixNano())
}

// table returns the screen's ref table. s.mu guards s.tables: every tool
// call holds it.
func (s *Server) table(rec *registry.Screen) *perceive.Table { return s.tableAt(tableKey(rec)) }

// desktopKey names the ref table of a desktop window, or of every visible
// window when window is empty.
func desktopKey(window string) string {
	if window == "" {
		window = "*"
	}
	return desktop.Name + ":" + window
}

// tableAt returns the ref table of key.
func (s *Server) tableAt(k string) *perceive.Table {
	if s.tables == nil {
		s.tables = map[string]*perceive.Table{}
	}
	t := s.tables[k]
	if t == nil {
		t = perceive.NewTable()
		s.tables[k] = t
	}
	return t
}

func (s *Server) dropTable(rec *registry.Screen) { delete(s.tables, tableKey(rec)) }

// source resolves the screen and chooses a source: want, or the source of
// the table's last read when want is empty. The caller closes it.
func (s *Server) source(ctx context.Context, name, want string) (*wl.Client, perceive.Source, *perceive.Table, *registry.Screen, error) {
	rec, cl, err := s.resolve(name, true)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	t := s.table(rec)
	if want == "" {
		want = t.SourceName()
	}
	src, err := perceive.Choose(ctx, rec, cl, want)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return cl, src, t, rec, nil
}

// desktopTarget opens the desktop and resolves window, or every window on
// a visible workspace when window is empty. It refuses while the session
// is locked. The caller closes conn.CL.
func (s *Server) desktopTarget(window string) (*desktop.Conn, perceive.Target, error) {
	d, err := s.desktop()
	if err != nil {
		return nil, perceive.Target{}, err
	}
	conn, err := desktop.Open(d.H)
	if err != nil {
		return nil, perceive.Target{}, err
	}
	var wins []hypr.Client
	if window != "" {
		var w hypr.Client
		w, err = conn.Window(window)
		wins = []hypr.Client{w}
	} else {
		wins, err = conn.VisibleWindows()
	}
	if err != nil {
		conn.CL.Close()
		return nil, perceive.Target{}, err
	}
	return conn, perceive.Target{Windows: wins}, nil
}

// noWindow refuses window on an agent screen.
func noWindow(name, window string) error {
	if window != "" && !desktop.IsDesktop(name) {
		return fmt.Errorf("window applies to screen desktop only")
	}
	return nil
}

func plain(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func (s *Server) snapshot(in snapIn) (*mcp.CallToolResult, error) {
	m, err := perceive.ParseMode(in.Mode)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), perceiveTimeout)
	defer cancel()
	if err := noWindow(in.Screen, in.Window); err != nil {
		return nil, err
	}
	want := in.Source
	if want == "" {
		want = "auto"
	}
	var src perceive.Source
	var t *perceive.Table
	var addrs []string
	if desktop.IsDesktop(in.Screen) {
		conn, target, err := s.desktopTarget(in.Window)
		if err != nil {
			return nil, err
		}
		defer conn.CL.Close()
		if src, err = perceive.ChooseDesktop(ctx, conn, target, want); err != nil {
			return nil, err
		}
		t = s.tableAt(desktopKey(in.Window))
		addrs = []string{}
		for _, w := range target.Windows {
			addrs = append(addrs, w.Address)
		}
	} else if _, src, t, _, err = s.source(ctx, in.Screen, want); err != nil {
		return nil, err
	}
	defer src.Close()
	out, _, err := perceive.Snapshot(ctx, src, t, perceive.SnapOpts{Mode: m, RootRef: in.Root, MaxNodes: in.MaxNodes})
	if err != nil {
		return nil, err
	}
	if addrs != nil { // the header lists the desktop windows read
		head, rest, ok := strings.Cut(out, "\n")
		out = head + " windows=" + strings.Join(addrs, ",")
		if ok {
			out += "\n" + rest
		}
	}
	return plain(out), nil
}

func (s *Server) act(in actIn) (*mcp.CallToolResult, error) {
	if err := refuseDesktop(in.Screen, "act on the desktop comes in a later build"); err != nil {
		return nil, err
	}
	if in.Ref == "" {
		return nil, fmt.Errorf("ref must not be empty: take it from snapshot or find")
	}
	if !perceive.ValidOp(in.Op) {
		return nil, fmt.Errorf("unknown op %q (click, double_click, type, key, hover, scroll)", in.Op)
	}
	ctx, cancel := context.WithTimeout(context.Background(), perceiveTimeout)
	defer cancel()
	cl, src, t, rec, err := s.source(ctx, in.Screen, "")
	if err != nil {
		return nil, err
	}
	defer src.Close()
	d, err := perceive.Act(ctx, cl, src, t, perceive.ActOp{Ref: in.Ref, Op: in.Op, Text: in.Text, Keys: in.Keys, Direction: in.Direction})
	if err != nil {
		return nil, err
	}
	res := plain(d.String())
	if in.ScreenshotAfter {
		shot, err := s.afterAction(rec, cl, true, 0)
		if err != nil {
			return nil, err
		}
		res.Content = append(res.Content, shot.Content...)
	}
	return res, nil
}

func (s *Server) find(in findIn) (*mcp.CallToolResult, error) {
	re, err := regexp.Compile(in.Text)
	if err != nil {
		return nil, fmt.Errorf("text is not a valid regular expression: %v", err)
	}
	timeout := time.Duration(max(in.TimeoutMs, 0)) * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout+perceiveTimeout)
	defer cancel()
	if err := noWindow(in.Screen, in.Window); err != nil {
		return nil, err
	}
	var t *perceive.Table
	var choose func(ctx context.Context, want string) (perceive.Source, error)
	if desktop.IsDesktop(in.Screen) {
		conn, target, err := s.desktopTarget(in.Window)
		if err != nil {
			return nil, err
		}
		defer conn.CL.Close()
		t = s.tableAt(desktopKey(in.Window))
		choose = func(ctx context.Context, want string) (perceive.Source, error) {
			return perceive.ChooseDesktop(ctx, conn, target, want)
		}
	} else {
		rec, cl, err := s.resolve(in.Screen, true)
		if err != nil {
			return nil, err
		}
		t = s.table(rec)
		choose = func(ctx context.Context, want string) (perceive.Source, error) {
			return perceive.Choose(ctx, rec, cl, want)
		}
	}
	var nodes []perceive.Node
	if want := t.SourceName(); want != "auto" {
		var src perceive.Source
		if src, err = choose(ctx, want); err != nil {
			return nil, err
		}
		defer src.Close()
		nodes, err = perceive.Find(ctx, src, t, re, in.Role, timeout)
	} else {
		// No recorded source: the app may not be on the a11y bus yet, so choose on every poll.
		auto := func(ctx context.Context) (perceive.Source, error) { return choose(ctx, "auto") }
		nodes, err = perceive.FindAuto(ctx, auto, t, re, in.Role, timeout)
	}
	if err != nil {
		return nil, err
	}
	return plain(perceive.RenderMatches(nodes)), nil
}
