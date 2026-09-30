package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/screen"
)

type clipIn struct {
	Screen string `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
	Text   string `json:"text,omitempty" jsonschema:"text to put on the clipboard (clipboard_set only)"`
}

// registerClip adds the clipboard tools.
func (s *Server) registerClip(srv *mcp.Server) {
	tool(s, srv, "clipboard_get", "Read the clipboard of a screen (not the human's clipboard).", s.clipboardGet)
	tool(s, srv, "clipboard_set", "Put text on the clipboard of a screen (not the human's clipboard); paste it in the app with key ctrl+v.", s.clipboardSet)
}

func (s *Server) clipboardGet(in clipIn) (*mcp.CallToolResult, error) {
	if err := refuseDesktop(in.Screen, "the clipboard tools have no desktop equivalent"); err != nil {
		return nil, err
	}
	rec, _, err := s.resolve(in.Screen, false)
	if err != nil {
		return nil, err
	}
	text, err := screen.ClipboardGet(rec)
	if err != nil {
		return nil, err
	}
	return textResult(map[string]any{"screen": rec.Name, "text": text}), nil
}

func (s *Server) clipboardSet(in clipIn) (*mcp.CallToolResult, error) {
	if err := refuseDesktop(in.Screen, "the clipboard tools have no desktop equivalent"); err != nil {
		return nil, err
	}
	rec, _, err := s.resolve(in.Screen, false)
	if err != nil {
		return nil, err
	}
	if err := screen.ClipboardSet(rec, in.Text); err != nil {
		return nil, err
	}
	return textResult(map[string]string{"status": "ok"}), nil
}
