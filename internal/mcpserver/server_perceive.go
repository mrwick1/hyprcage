package mcpserver

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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
func (s *Server) table(rec *registry.Screen) *perceive.Table {
	if s.tables == nil {
		s.tables = map[string]*perceive.Table{}
	}
	k := tableKey(rec)
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
	want := in.Source
	if want == "" {
		want = "auto"
	}
	_, src, t, _, err := s.source(ctx, in.Screen, want)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	out, _, err := perceive.Snapshot(ctx, src, t, perceive.SnapOpts{Mode: m, RootRef: in.Root, MaxNodes: in.MaxNodes})
	if err != nil {
		return nil, err
	}
	return plain(out), nil
}

func (s *Server) act(in actIn) (*mcp.CallToolResult, error) {
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
	rec, cl, err := s.resolve(in.Screen, true)
	if err != nil {
		return nil, err
	}
	t := s.table(rec)
	var nodes []perceive.Node
	if want := t.SourceName(); want != "auto" {
		var src perceive.Source
		if src, err = perceive.Choose(ctx, rec, cl, want); err != nil {
			return nil, err
		}
		defer src.Close()
		nodes, err = perceive.Find(ctx, src, t, re, in.Role, timeout)
	} else {
		// No recorded source: the app may not be on the a11y bus yet, so choose on every poll.
		choose := func(ctx context.Context) (perceive.Source, error) { return perceive.Choose(ctx, rec, cl, "auto") }
		nodes, _, err = perceive.FindAuto(ctx, choose, t, re, in.Role, timeout)
	}
	if err != nil {
		return nil, err
	}
	return plain(perceive.RenderMatches(nodes)), nil
}
