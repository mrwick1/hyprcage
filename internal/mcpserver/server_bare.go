package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hexadecimil/offstage/internal/audit"
	"github.com/hexadecimil/offstage/internal/bare"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/screen"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerBare copies the bundled engine's schemas rather than maintaining
// another set of browser commands. Native app tools keep their screen schemas.
func (s *Server) registerBare(ctx context.Context, srv *mcp.Server) error {
	b, err := bare.Start(ctx, "")
	if err != nil {
		return err
	}
	if s.bare == nil {
		s.bare = map[string]*mcp.ClientSession{}
	}
	s.bare[""] = b
	for t, err := range b.Tools(ctx, nil) {
		if err != nil {
			s.closeBare()
			return err
		}
		original := t.Name
		copy := *t
		copy.Name = "browser_" + original
		copy.Description = "Bare browser. Use browser_* tools for this session. With screen, the browser runs inside that owned offstage screen; without screen it runs headlessly. " + t.Description
		raw, err := json.Marshal(t.InputSchema)
		if err != nil {
			s.closeBare()
			return err
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			s.closeBare()
			return err
		}
		props, _ := schema["properties"].(map[string]any)
		if props == nil {
			props = map[string]any{}
			schema["properties"] = props
		}
		props["screen"] = map[string]any{"type": "string", "description": "owned offstage cage screen for a visible browser; omit for headless"}
		copy.InputSchema = schema
		_, hasWindow := props["window"]
		if hasWindow {
			props["window"] = map[string]any{"type": "boolean", "description": "Managed by offstage: pass screen to show the browser in a cage; headless otherwise"}
		}
		srv.AddTool(&copy, func(ctx context.Context, req *mcp.CallToolRequest) (result *mcp.CallToolResult, err error) {
			// Handler errors are tool failures, not JSON-RPC protocol failures.
			defer func() {
				if err != nil {
					result = plain(err.Error())
					result.IsError = true
					err = nil
				}
			}()
			var entry *audit.Entry
			s.mu.Lock()
			defer func() {
				s.mu.Unlock()
				if entry != nil {
					s.recordTimeline(*entry, result)
				}
			}()
			var args map[string]any
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return nil, err
			}
			if args == nil {
				args = map[string]any{}
			}
			metadata := timelineMetadata(args)
			start := time.Now()
			result, err = s.callBare(ctx, original, args, hasWindow)
			normalizeBareResult(original, result)
			// The audit keeps only the code of err, never its text.
			failed := err
			if err == nil && result != nil && result.IsError {
				failed = fmt.Errorf("Bare tool failed")
			}
			// Bare arguments can contain credentials and scripts. Log metadata only.
			metadata = append(metadata, audit.WithDuration(time.Since(start)))
			recordedEntry := audit.Record(copy.Name, struct{}{}, failed, metadata...)
			entry = &recordedEntry
			if failed != nil {
				logBlock(recordedEntry, err, result)
			}
			return result, err
		})
	}
	return nil
}

func (s *Server) callBare(ctx context.Context, name string, args map[string]any, hasWindow bool) (*mcp.CallToolResult, error) {
	key := ""
	if v, ok := args["screen"]; ok {
		screenName, ok := v.(string)
		if !ok || screenName == "" {
			return nil, fmt.Errorf("screen must name an owned cage screen")
		}
		rec, _, err := s.resolve(screenName, false)
		if err != nil {
			return nil, err
		}
		if rec.Kind == screen.KindHeadless {
			return nil, fmt.Errorf("use browser_open without screen for a headless Bare session")
		}
		key = tableKey(rec)
		if s.bare[key] == nil {
			display := rec.InnerDisplay
			if display == "" {
				inner, err := registry.ReadInner(rec.Name)
				if err != nil {
					return nil, err
				}
				display = inner["WAYLAND_DISPLAY"]
			}
			if display == "" {
				return nil, fmt.Errorf("screen %s has no Wayland socket", rec.Name)
			}
			b, err := bare.Start(ctx, display)
			if err != nil {
				return nil, err
			}
			s.bare[key] = b
		}
	}
	delete(args, "screen")
	// Window creation is controlled by the cage binding, never by the host desktop.
	if hasWindow {
		args["window"] = key != ""
	}
	b := s.bare[key]
	if b == nil {
		return nil, fmt.Errorf("Bare is unavailable; restart the offstage MCP server")
	}
	result, err := b.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err == nil && result != nil {
		if s.bareSessionHints == nil {
			s.bareSessionHints = map[string]string{}
		}
		sessionName, _ := args["session"].(string)
		if sessionName == "" {
			sessionName = "main"
		}
		if name == "close" && !result.IsError {
			delete(s.bareSessionHints, key)
		} else {
			s.bareSessionHints[key] = sessionName
		}
	}
	return result, err
}

func (s *Server) closeBare() {
	for key, b := range s.bare {
		_ = b.Close()
		delete(s.bare, key)
	}
}

// Bare's report tools use a documented compact receipt or a JSON report.
// Page text and diagnostic tools must never be classified from their contents.
func normalizeBareResult(name string, result *mcp.CallToolResult) {
	if result == nil {
		return
	}
	switch name {
	case "open", "act", "step", "run_steps", "run_suite", "close":
	default:
		return
	}
	text := strings.TrimSpace(textResultText(result))
	var report struct {
		OK *bool `json:"ok"`
	}
	failed := false
	known := false
	if json.Unmarshal([]byte(text), &report) == nil && report.OK != nil {
		failed = !*report.OK
		known = true
	} else if strings.HasPrefix(text, "FAILED ") {
		failed = true
		known = true
	} else if strings.HasPrefix(text, "ok (") {
		known = true
	}
	if failed {
		result.IsError = true
	}
	if known && result.StructuredContent == nil {
		result.StructuredContent = map[string]any{"ok": !failed, "result_source": "bare_report"}
	}
}

func textResultText(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			return t.Text
		}
	}
	return ""
}
