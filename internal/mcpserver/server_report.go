package mcpserver

import (
	"fmt"
	"math"
	"strings"

	"github.com/hexadecimil/offstage/internal/desktop"
	"github.com/hexadecimil/offstage/internal/report"
	"github.com/hexadecimil/offstage/internal/screen"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type reportIn struct {
	Title     string `json:"title" jsonschema:"one line that names the offstage problem"`
	Expected  string `json:"expected" jsonschema:"what the tool should have done"`
	Actual    string `json:"actual" jsonschema:"what it did instead, with the error text"`
	Screen    string `json:"screen,omitempty" jsonschema:"the agent screen it happened on; its tool calls, logs, element tree and a screenshot go into the report"`
	Area      string `json:"area,omitempty" jsonschema:"crash, isolation-leak (a tool touched the human's desktop, cursor or focus), install or desktop"`
	Type      string `json:"type,omitempty" jsonschema:"bug (default), doubt, suggestion or feedback"`
	Confirmed bool   `json:"confirmed,omitempty" jsonschema:"true only after the human saw the preview and agreed to send it"`
}

const reportDesc = "Send a beta report about a problem in offstage itself, not in the app under test: a tool error the arguments did not cause, an action that returned success but did not happen, a tool that touched the human's desktop, cursor or focus, or a crash. One report per distinct problem. The report carries the tool calls on the screen, the logs, the element tree and a screenshot of the agent screen; private data is removed first. When the reply is a preview, show it to the human and call again with confirmed:true only after they agree."

func (s *Server) registerReport(srv *mcp.Server) {
	tool(s, srv, "report_issue", reportDesc, s.reportIssue)
}

func (s *Server) reportIssue(in reportIn) (*mcp.CallToolResult, error) {
	if desktop.IsDesktop(in.Screen) {
		return nil, screen.Errf(screen.CodeUnsupported, "name the agent screen, or leave screen empty", "a report never reads the human's desktop")
	}
	req := report.Request{Title: in.Title, Expected: in.Expected, Actual: in.Actual, Area: in.Area, Type: in.Type, Extra: map[string][]byte{}}
	if in.Screen != "" {
		if rec, _, err := s.resolve(in.Screen, false); err == nil {
			req.Screen = rec.Name
		} else {
			req.Screen = in.Screen // the screen may be gone; its log lines are still useful
		}
	}
	if req.Screen != "" && s.cfg.Report.Screenshots {
		var notes []string
		if res, err := s.snapshot(snapIn{Screen: req.Screen, Mode: "full"}); err == nil {
			req.Extra["snapshot.txt"] = []byte(resultText(res))
		} else {
			notes = append(notes, "snapshot: "+err.Error())
		}
		if _, cl, err := s.resolve(req.Screen, true); err == nil {
			shot, err := screen.Shot(cl, screen.ShotOptions{Scale: 1, Format: "png", Cursor: true, MaxSide: s.cfg.ShotMaxSide, MaxBytes: math.MaxInt32})
			if err == nil {
				req.Extra["screenshot.png"] = shot.Data
			} else {
				notes = append(notes, "screenshot: "+err.Error())
			}
		} else {
			notes = append(notes, "screen: "+err.Error())
		}
		if len(notes) > 0 {
			req.Extra["notes.txt"] = []byte(strings.Join(notes, "\n") + "\n")
		}
	}
	b, err := report.Build(req)
	if err != nil {
		return nil, screen.Errf(screen.CodeUnsupported, "", "%v", err)
	}
	if s.cfg.Report.Confirm && !in.Confirmed {
		return textResult(map[string]any{
			"preview": fmt.Sprintf("Title: %s\nPlatform: %s\nVersion: %s\n\n%s", b.Meta.Title, b.Meta.Platform, b.Meta.Version, b.Meta.Body),
			"files":   b.Files,
			"bytes":   len(b.Zip),
			"next":    "Show the preview and the file list to the human. Send it with the same arguments and confirmed:true only after they agree. Private data (home paths, user and host names, tokens, URL queries, emails, typed text) is already removed.",
		}), nil
	}
	msg, err := report.Deliver(s.cfg.Report.URL, b)
	if err != nil {
		return nil, screen.Errf(screen.CodeUnsupported, "run: offstage report login", "%v", err)
	}
	report.ClearCrashes(b.Crashes)
	return textResult(map[string]string{"result": msg}), nil
}

func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
