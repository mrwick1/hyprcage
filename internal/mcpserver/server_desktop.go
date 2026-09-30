package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/desktop"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

type desktopIn struct {
	Address   string   `json:"address,omitempty" jsonschema:"window address from desktop_windows, e.g. 0x55ebac116320"`
	Workspace int      `json:"workspace,omitempty" jsonschema:"target workspace (desktop_move)"`
	Text      string   `json:"text,omitempty" jsonschema:"text to type (desktop_type); US layout characters only"`
	Keys      []string `json:"keys,omitempty" jsonschema:"combinations such as [\"ctrl+l\", \"Return\"] (desktop_key)"`
}

// registerDesktop adds the tools that act on the human's own windows.
func (s *Server) registerDesktop(srv *mcp.Server) {
	tool(s, srv, "desktop_windows", "List the human's own windows (address, class, title, workspace). Use an agent screen for your own apps; these are the human's.", s.desktopWindows)
	tool(s, srv, "desktop_focus", "Give keyboard focus to one of the human's windows. This moves the human's focus: only when the task needs it.", s.desktopFocus)
	tool(s, srv, "desktop_move", "Move one of the human's windows to a workspace, without following it.", s.desktopMove)
	tool(s, srv, "desktop_type", "Type text into one of the human's windows without focusing it. Each key briefly takes the human's keyboard focus and gives it back. US layout characters only.", s.desktopType)
	tool(s, srv, "desktop_key", "Press key combinations in one of the human's windows without focusing it (same focus blip as desktop_type).", s.desktopKey)
}

// desktop acts on the instance that desktop.Instance names. The driver is
// the context's own when that instance is the context's.
func (s *Server) desktop() (desktop.Desktop, error) {
	c, err := s.hypr()
	if err != nil {
		return desktop.Desktop{}, err
	}
	h, err := desktop.Instance(c.Hypr)
	if err != nil {
		return desktop.Desktop{}, err
	}
	if h == nil || h == c.Hypr {
		return desktop.Desktop{H: h, D: c.Driver}, nil
	}
	return desktop.Desktop{H: h, D: h.Driver()}, nil
}

// refuseDesktop refuses screen "desktop" in a tool made for agent screens.
func refuseDesktop(name, hint string) error {
	if desktop.IsDesktop(name) {
		return screen.Errf(screen.CodeUnsupported, hint, "this tool acts on agent screens, not on the desktop")
	}
	return nil
}

func (s *Server) desktopWindows(in struct{}) (*mcp.CallToolResult, error) {
	d, err := s.desktop()
	if err != nil {
		return nil, err
	}
	wins, err := d.Windows()
	if err != nil {
		return nil, err
	}
	return textResult(wins), nil
}

func (s *Server) desktopFocus(in desktopIn) (*mcp.CallToolResult, error) {
	d, err := s.desktop()
	if err != nil {
		return nil, err
	}
	if err := d.Focus(in.Address); err != nil {
		return nil, err
	}
	return textResult(map[string]string{"status": "ok"}), nil
}

func (s *Server) desktopMove(in desktopIn) (*mcp.CallToolResult, error) {
	d, err := s.desktop()
	if err != nil {
		return nil, err
	}
	if err := d.Move(in.Address, in.Workspace); err != nil {
		return nil, err
	}
	return textResult(map[string]string{"status": "ok"}), nil
}

func (s *Server) desktopType(in desktopIn) (*mcp.CallToolResult, error) {
	d, err := s.desktop()
	if err != nil {
		return nil, err
	}
	if err := d.Type(in.Address, in.Text); err != nil {
		return nil, err
	}
	return textResult(map[string]string{"status": "ok"}), nil
}

func (s *Server) desktopKey(in desktopIn) (*mcp.CallToolResult, error) {
	d, err := s.desktop()
	if err != nil {
		return nil, err
	}
	if err := d.Key(in.Address, in.Keys); err != nil {
		return nil, err
	}
	return textResult(map[string]string{"status": "ok"}), nil
}
