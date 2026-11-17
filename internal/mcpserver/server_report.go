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
	Expected  string `json:"expected,omitempty" jsonschema:"for a bug: what the tool should have done"`
	Actual    string `json:"actual,omitempty" jsonschema:"for a bug: what it did instead, with the error text; for other types: the details"`
	Screen    string `json:"screen,omitempty" jsonschema:"the agent screen it happened on; its tool calls, logs, element tree and a screenshot go into the report"`
	Area      string `json:"area,omitempty" jsonschema:"crash, isolation-leak (a tool touched the human's desktop, cursor or focus), install, desktop or bare (a problem with the bare browser server, bare-mcp)"`
	Type      string `json:"type,omitempty" jsonschema:"bug (default), doubt, suggestion or feedback"`
	Confirmed bool   `json:"confirmed,omitempty" jsonschema:"true only after the human saw the preview and agreed to send it"`
}

const reportDesc = "Send a beta report about offstage itself, never about the app under test. Also report problems with the bare browser server (bare-mcp) here, with area bare. type bug (default): a tool error the arguments did not cause, an action that returned success but did not happen, a tool that touched the human's desktop, cursor or focus, or a crash. type doubt: you were unsure how to use offstage (which tool, what an argument means, an unclear error or doc). type suggestion: a tool or option that would have helped. type feedback: what worked well or badly, once at the end of a task that used offstage heavily; expected and actual may be empty for these three. One report per distinct item; do not repeat one you filed this session. The report carries the tool calls on the screen, the logs, the element tree and a screenshot of the agent screen; private data is removed first. When the reply is a preview, show it to the human and call again with confirmed:true only after they agree."

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
	if _, err := report.Save(b); err != nil {
		return nil, screen.Errf(screen.CodeUnsupported, "", "queue the report: %v", err)
	}
	report.ClearCrashes(b.Crashes)
	wakeReports()
	msg := "queued; it is sent in the background. Continue your task."
	if report.Key() == "" {
		msg = "queued; it is sent when the human runs offstage report login. Continue your task."
	}
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
