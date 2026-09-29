package mcpserver

import (
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/record"
)

type recordIn struct {
	Target string `json:"target,omitempty" jsonschema:"a screen name, or \"desktop\" for the human's own screen; optional when the session owns exactly one screen"`
}

// registerFork adds the tools of the mrwick1 fork.
func (s *Server) registerFork(srv *mcp.Server) {
	tool(s, srv, "record_start", "Start recording a screen (or the human's desktop with target=\"desktop\") to an MP4 file. It stops by itself after the configured maximum (30 min by default), when the screen closes, or with record_stop. Desktop recording is refused while the human's session is locked.", s.recordStart)
	tool(s, srv, "record_stop", "Stop a recording and return the path of the MP4 file.", s.recordStop)
	s.registerBrowser(srv)
}

func (s *Server) recordTarget(target string) (string, bool, error) {
	if target == record.Desktop {
		return record.Desktop, false, nil
	}
	rec, _, err := s.resolve(target, false)
	if err != nil {
		return "", false, err
	}
	return rec.Name, true, nil
}

func (s *Server) recordStart(in recordIn) (*mcp.CallToolResult, error) {
	target, isScreen, err := s.recordTarget(in.Target)
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	st, err := record.Start(exe, target, isScreen, s.cfg)
	if err != nil {
		return nil, err
	}
	return textResult(map[string]any{"target": st.Target, "path": st.Path, "max": s.cfg.RecordMax.String()}), nil
}

func (s *Server) recordStop(in recordIn) (*mcp.CallToolResult, error) {
	target, _, err := s.recordTarget(in.Target)
	if err != nil {
		return nil, err
	}
	st, err := record.Stop(target)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"target": st.Target, "path": st.Path}
	if fi, err := os.Stat(st.Path); err == nil {
		out["bytes"] = fi.Size()
	}
	return textResult(out), nil
}
