package desktop

import (
	"image"
	"strconv"

	"github.com/hexadecimil/hyprcage/internal/hypr"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// Conn is a Wayland connection to the desktop instance.
type Conn struct {
	H  *hypr.Instance
	CL *wl.Client
}

// Open connects to the Wayland socket of h. It refuses while the session is
// locked.
func Open(h *hypr.Instance) (*Conn, error) {
	if err := (Desktop{H: h}).session(); err != nil {
		return nil, err
	}
	display, err := h.WaylandDisplay()
	if err != nil {
		return nil, screen.Errf(screen.CodeHyprland, "", "%v", err)
	}
	cl, err := wl.Connect(display)
	if err != nil {
		return nil, screen.Errf(screen.CodeHyprland, "", "%v", err)
	}
	return &Conn{H: h, CL: cl}, nil
}

// Window returns the client with address addr.
func (c *Conn) Window(addr string) (hypr.Client, error) {
	if err := CheckAddress(addr); err != nil {
		return hypr.Client{}, err
	}
	all, err := c.H.Clients()
	if err != nil {
		return hypr.Client{}, screen.Errf(screen.CodeHyprland, "", "%v", err)
	}
	for _, w := range all {
		if w.Address == addr {
			return w, nil
		}
	}
	return hypr.Client{}, screen.Errf(screen.CodeAddress, "take the address from desktop_windows", "no window %s", addr)
}

// Visible reports whether the window's workspace is shown on a monitor.
func (c *Conn) Visible(addr string) (bool, error) {
	w, err := c.Window(addr)
	if err != nil {
		return false, err
	}
	mons, err := c.H.Monitors()
	if err != nil {
		return false, screen.Errf(screen.CodeHyprland, "", "%v", err)
	}
	return visible(w.Workspace.ID, mons), nil
}

// VisibleWindows returns the clients on a workspace that a monitor shows.
func (c *Conn) VisibleWindows() ([]hypr.Client, error) {
	all, err := c.H.Clients()
	if err != nil {
		return nil, screen.Errf(screen.CodeHyprland, "", "%v", err)
	}
	mons, err := c.H.Monitors()
	if err != nil {
		return nil, screen.Errf(screen.CodeHyprland, "", "%v", err)
	}
	var out []hypr.Client
	for _, w := range all {
		if visible(w.Workspace.ID, mons) {
			out = append(out, w)
		}
	}
	return out, nil
}

// visible reports whether workspace ws is the active or the open special
// workspace of a monitor.
func visible(ws int, mons []hypr.Monitor) bool {
	for _, m := range mons {
		if m.ActiveWorkspace.ID == ws || (m.SpecialWorkspace.ID != 0 && m.SpecialWorkspace.ID == ws) {
			return true
		}
	}
	return false
}

// handle is the toplevel export handle of a window: the low 32 bits of its
// address.
func handle(addr string) (uint32, error) {
	v, err := strconv.ParseUint(addr[2:], 16, 64)
	if err != nil {
		return 0, screen.Errf(screen.CodeAddress, "take the address from desktop_windows", "%q is not a window address", addr)
	}
	return uint32(v), nil
}

// Shot captures the desktop output, or the window addr on any workspace, and
// places the capture in the global logical layout.
func (c *Conn) Shot(addr string, o screen.ShotOptions) (*screen.ShotResult, error) {
	var img *image.RGBA
	var origin [2]int
	var logicalW float64
	if addr == "" {
		mons, err := c.H.Monitors()
		if err != nil {
			return nil, screen.Errf(screen.CodeHyprland, "", "%v", err)
		}
		if len(mons) == 0 {
			return nil, screen.Errf(screen.CodeHyprland, "", "no monitor")
		}
		// ponytail: one monitor. wl.Client binds the first wl_output only,
		// so a second monitor is not captured; bind each output when one
		// is in use.
		m := mons[0]
		if img, err = c.CL.Capture(o.Cursor); err != nil {
			return nil, screen.Errf(screen.CodeCapture, "", "%v", err)
		}
		origin, logicalW = [2]int{m.X, m.Y}, float64(m.Width)/m.Scale
	} else {
		w, err := c.Window(addr)
		if err != nil {
			return nil, err
		}
		h, err := handle(addr)
		if err != nil {
			return nil, err
		}
		if img, err = c.CL.CaptureToplevel(h, o.Cursor); err != nil {
			return nil, screen.Errf(screen.CodeCapture, "", "%v", err)
		}
		origin, logicalW = w.At, float64(w.Size[0])
	}
	res, err := screen.ShotImage(img, o)
	if err != nil {
		return nil, err
	}
	res.Origin = &origin
	res.LogicalPerPixel = logicalW / float64(res.ScreenW)
	return res, nil
}
