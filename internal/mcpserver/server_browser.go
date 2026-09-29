package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/browser"
)

type browserIn struct {
	Screen string `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
	URL    string `json:"url,omitempty" jsonschema:"page to open"`
}

// registerBrowser adds browser_open.
func (s *Server) registerBrowser(srv *mcp.Server) {
	tool(s, srv, "browser_open", "Open the agent's Chrome on a screen with DevTools on 127.0.0.1:9222 and a throwaway profile. Then drive it with the agent-chrome MCP tools (DOM, console, network, JavaScript). One agent Chrome at a time; it closes with its screen.", s.browserOpen)
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
	return textResult(info), nil
}
