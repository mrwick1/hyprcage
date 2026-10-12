package mcpserver

import (
	"fmt"
	"image"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/desktop"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

type desktopIn struct {
	Ack       string   `json:"ack,omitempty" jsonschema:"the reason for an action the guard blocked with ack_required; repeat the call with it"`
	Address   string   `json:"address,omitempty" jsonschema:"window address from desktop_windows, e.g. 0x55ebac116320"`
	Workspace int      `json:"workspace,omitempty" jsonschema:"target workspace (desktop_move, desktop_workspace)"`
	Text      string   `json:"text,omitempty" jsonschema:"text to type (desktop_type); US layout characters only"`
	Keys      []string `json:"keys,omitempty" jsonschema:"combinations such as [\"ctrl+l\", \"Return\"] (desktop_key)"`
	Verify    *bool    `json:"verify,omitempty" jsonschema:"desktop_type and desktop_key: judge the action by diffing the window before and after it, see the tool description"`
}

// registerDesktop adds the tools that act on the human's own windows.
func (s *Server) registerDesktop(srv *mcp.Server) {
	tool(s, srv, "desktop_windows", "List the human's own windows (address, class, title, workspace). Use an agent screen for your own apps; these are the human's.", s.desktopWindows)
	tool(s, srv, "desktop_focus", "Give keyboard focus to one of the human's windows. This moves the human's focus: only when the task needs it.", s.desktopFocus)
	tool(s, srv, "desktop_move", "Move one of the human's windows to a workspace, without following it.", s.desktopMove)
	tool(s, srv, "desktop_type", "Type text into one of the human's windows without focusing it. Each key briefly takes the human's keyboard focus and gives it back. US layout characters only. Verify: "+verifyDoc+" (the diff is of that window)", s.desktopType)
	tool(s, srv, "desktop_workspace", "Switch the human's visible workspace. This is the only tool that changes the human's view: only when the human asks for it.", s.desktopWorkspace)
	tool(s, srv, "desktop_key", "Press key combinations in one of the human's windows without focusing it (same focus blip as desktop_type). Verify: "+verifyDoc+" (the diff is of that window)", s.desktopKey)
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

// desktopShot captures the desktop output, or one window on any workspace.
func (s *Server) desktopShot(window string, settleMs int, path string, o screen.ShotOptions) (*mcp.CallToolResult, error) {
	d, err := s.desktop()
	if err != nil {
		return nil, err
	}
	conn, err := desktop.Open(d.H)
	if err != nil {
		return nil, err
	}
	defer conn.CL.Close()
	if settleMs > 0 {
		time.Sleep(time.Duration(min(settleMs, 10000)) * time.Millisecond)
	}
	res, err := conn.Shot(window, o)
	if err != nil {
		return nil, err
	}
	return shotResult(res, desktop.Name, path)
}

// desktopPointer runs pointer input on the desktop and restores the cursor.
// The window is the one that verify diffs; pts are the points of the action
// in global logical pixels.
func (s *Server) desktopPointer(window string, shotAfter bool, settleMs int, verify *bool, pts []image.Point, fn func(screen.PointerInput) error) (*mcp.CallToolResult, error) {
	d, err := s.desktop()
	if err != nil {
		return nil, err
	}
	conn, err := desktop.Open(d.H)
	if err != nil {
		return nil, err
	}
	defer conn.CL.Close()
	v := s.startDesktopVerify(verify, conn, window, pts...)
	if err := (desktop.Pointer{C: conn, D: d.D}).Do(window, fn); err != nil {
		return nil, err
	}
	ver := v.finish(verifySettle)
	if !shotAfter {
		res := textResult(map[string]string{"status": "ok"})
		noteVerdict(res, ver)
		return res, nil
	}
	if settleMs <= 0 {
		settleMs = 150
	}
	res, err := s.desktopShot("", settleMs, "", screen.ShotOptions{Cursor: true, MaxSide: s.cfg.ShotMaxSide, MaxBytes: s.cfg.ShotMaxBytes})
	if err == nil {
		noteVerdict(res, ver)
	}
	return res, err
}

// desktopHover moves the desktop cursor and leaves it there.
func (s *Server) desktopHover(window string, x, y int, verify *bool) (*mcp.CallToolResult, error) {
	d, err := s.desktop()
	if err != nil {
		return nil, err
	}
	conn, err := desktop.Open(d.H)
	if err != nil {
		return nil, err
	}
	defer conn.CL.Close()
	v := s.startDesktopVerify(verify, conn, window, image.Pt(x, y))
	if err := (desktop.Pointer{C: conn, D: d.D}).Hover(window, x, y); err != nil {
		return nil, err
	}
	res := textResult(map[string]string{"status": "ok"})
	noteVerdict(res, v.finish(verifySettle))
	return res, nil
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
	if err := s.guard(guardCall{Screen: desktop.Name, Window: in.Address, Ack: in.Ack}); err != nil {
		return nil, err
	}
	d, err := s.desktop()
	if err != nil {
		return nil, err
	}
	v := s.startDesktopKeyVerify(in.Verify, d, in.Address)
	defer v.release()
	if err := d.Type(in.Address, in.Text); err != nil {
		return nil, err
	}
	res := textResult(map[string]string{"status": "ok"})
	noteVerdict(res, v.finish(verifySettle))
	return res, nil
}

func (s *Server) desktopKey(in desktopIn) (*mcp.CallToolResult, error) {
	if err := s.guard(guardCall{Screen: desktop.Name, Window: in.Address, Keys: in.Keys, Ack: in.Ack}); err != nil {
		return nil, err
	}
	d, err := s.desktop()
	if err != nil {
		return nil, err
	}
	v := s.startDesktopKeyVerify(in.Verify, d, in.Address)
	defer v.release()
	if err := d.Key(in.Address, in.Keys); err != nil {
		return nil, err
	}
	res := textResult(map[string]string{"status": "ok"})
	noteVerdict(res, v.finish(verifySettle))
	return res, nil
}

func (s *Server) desktopWorkspace(in desktopIn) (*mcp.CallToolResult, error) {
	d, err := s.desktop()
	if err != nil {
		return nil, err
	}
	if err := d.Workspace(in.Workspace); err != nil {
		return nil, err
	}
	return textResult(map[string]string{"status": "ok"}), nil
}

// desktopLaunch runs app_launch on the desktop, on a workspace without
// switching to it.
func (s *Server) desktopLaunch(in launchIn) (*mcp.CallToolResult, error) {
	if in.Debug {
		return nil, screen.Errf(screen.CodeUnsupported, "launch it on an agent screen", "debug is for agent screens only")
	}
	d, err := s.desktop()
	if err != nil {
		return nil, err
	}
	wait := s.cfg.WindowTimeout
	if in.WaitWindowMs != nil {
		wait = time.Duration(*in.WaitWindowMs) * time.Millisecond
	}
	pid, addr, byWindow, err := d.Launch(in.Command, in.Env, in.Cwd, in.Workspace, wait)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"screen": desktop.Name}
	if addr == "" {
		out["note"] = fmt.Sprintf("launched; no new window within %d ms; the app may still be starting (see desktop_windows)", wait.Milliseconds())
		return textResult(out), nil
	}
	out["pid"], out["window"] = pid, addr
	if byWindow {
		out["note"] = "matched by new window, not by pid"
	}
	return textResult(out), nil
}
