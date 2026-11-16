package wl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// Window is the mirror's side on the human's compositor: one xdg toplevel
// showing the agent screen at its own proportions. The toplevel's surface is
// a backdrop, a single dark pixel scaled to the window by a viewport; the
// frame goes on a subsurface above it, scaled by its own viewport to the
// largest size that keeps the screen's aspect ratio, and centred. A window
// of another shape than the screen shows bars, never a stretched or cropped
// image. Each frame screencopy writes into the shared pool is attached as
// is, so nothing is copied here. Nothing here is specific to Hyprland:
// xdg-shell, viewporter and subsurfaces are what every Wayland compositor
// speaks.
type Window struct {
	c *Client

	comp, wmBase, viewporter, subcomp uint32
	shmFormats                        map[uint32]bool

	surface  uint32 // the toplevel's surface: the backdrop
	xdg      uint32
	toplevel uint32
	viewport uint32 // scales the backdrop pixel to the window

	backdropPool *ShmPool
	backdropPl   uint32
	backdrop     uint32 // the 1x1 buffer

	image    uint32 // the subsurface carrying the frame
	imageSub uint32
	imageVp  uint32

	// layout is what the last frame was laid out for; a change re-commits
	// the backdrop and moves the image.
	layout struct{ winW, winH, fitW, fitH int }

	// Configured is the size the compositor gave the window, 0x0 until the
	// first configure.
	Configured struct{ W, H int }
	configured bool
	closed     bool

	// OnConfigure runs when the compositor sets the window's size.
	OnConfigure func(w, h int)
	// OnClose runs when the human closes the window.
	OnClose func()
	// OnFrame runs when the compositor is ready for the image's next frame,
	// which it only is while the window is visible.
	OnFrame func()
}

// OpenWindow connects to the human's compositor (display, or
// $WAYLAND_DISPLAY when empty) and creates the toplevel, unmapped. The
// caller sets the callbacks and then calls Start.
func OpenWindow(display, appID, title string) (*Window, error) {
	fd, err := dial(display)
	if err != nil {
		return nil, err
	}
	c := newClient(fd, display)
	w := &Window{c: c, shmFormats: map[uint32]bool{}}
	if err := w.setup(appID, title); err != nil {
		c.Close()
		return nil, err
	}
	return w, nil
}

func (w *Window) setup(appID, title string) error {
	c := w.c
	if err := c.initRegistry(); err != nil {
		return err
	}
	w.comp, _ = c.bind(ifaceWlCompositor, nil)
	w.subcomp, _ = c.bind(ifaceWlSubcompositor, nil)
	c.shm, _ = c.bind(ifaceWlShm, func(op int, args []any) error {
		if op == evtWlShmFormat {
			w.shmFormats[args[0].(uint32)] = true
		}
		return nil
	})
	w.wmBase, _ = c.bind(ifaceXdgWmBase, func(op int, args []any) error {
		if op == evtXdgWmBasePing {
			c.send(w.wmBase, reqXdgWmBasePong, args[0].(uint32))
		}
		return nil
	})
	w.viewporter, _ = c.bind(ifaceWpViewporter, nil)
	for iface, id := range map[string]uint32{
		ifaceWlCompositor: w.comp, ifaceWlShm: c.shm, ifaceWlSubcompositor: w.subcomp,
		ifaceXdgWmBase: w.wmBase, ifaceWpViewporter: w.viewporter,
	} {
		if id == 0 {
			return missingErr(iface)
		}
	}
	if err := c.Roundtrip(); err != nil {
		return err
	}

	w.surface = w.newSurface(nil)
	w.xdg = c.allocID()
	c.register(w.xdg, ifaceXdgSurface, protoIfaces[ifaceXdgSurface].Version, func(op int, args []any) error {
		if op == evtXdgSurfaceConfigure {
			c.send(w.xdg, reqXdgSurfaceAckConfigure, args[0].(uint32))
			w.configured = true
			if w.OnConfigure != nil {
				w.OnConfigure(w.Configured.W, w.Configured.H)
			}
		}
		return nil
	})
	c.send(w.wmBase, reqXdgWmBaseGetXdgSurface, w.xdg, w.surface)
	w.toplevel = c.allocID()
	c.register(w.toplevel, ifaceXdgToplevel, protoIfaces[ifaceXdgToplevel].Version, func(op int, args []any) error {
		switch op {
		case evtXdgToplevelConfigure:
			w.Configured.W, w.Configured.H = int(args[0].(int32)), int(args[1].(int32))
		case evtXdgToplevelClose:
			w.closed = true
			if w.OnClose != nil {
				w.OnClose()
			}
		}
		return nil
	})
	c.send(w.xdg, reqXdgSurfaceGetToplevel, w.toplevel)
	c.send(w.toplevel, reqXdgToplevelSetAppId, appID)
	c.send(w.toplevel, reqXdgToplevelSetTitle, title)
	return c.flush()
}

// Start asks for the initial configure and waits for it. The caller sets
// OnConfigure, OnClose and OnFrame first. The first commit carries no
// buffer, as xdg-shell requires before the surface may be mapped; Hyprland
// sends the configure a tick later, so a short wait follows the round trip.
func (w *Window) Start() error {
	w.c.send(w.surface, reqWlSurfaceCommit)
	if err := w.c.Roundtrip(); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for !w.configured && time.Now().Before(deadline) {
		if err := w.c.Roundtrip(); err != nil {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !w.configured {
		return errors.New("wl: the compositor did not configure the window")
	}
	// The viewports and the subsurface are created after the role is
	// configured: on Hyprland either one added to an unconfigured surface
	// keeps the first configure from ever arriving.
	w.viewport = w.newViewport(w.surface)
	w.image = w.newSurface(nil)
	w.imageSub = w.c.allocID()
	w.c.register(w.imageSub, ifaceWlSubsurface, protoIfaces[ifaceWlSubsurface].Version, nil)
	w.c.send(w.subcomp, reqWlSubcompositorGetSubsurface, w.imageSub, w.image, w.surface)
	// Desynchronised: a frame shows as soon as the image surface commits,
	// without a commit of the backdrop each time.
	w.c.send(w.imageSub, reqWlSubsurfaceSetDesync)
	w.imageVp = w.newViewport(w.image)
	if err := w.makeBackdrop(); err != nil {
		return err
	}
	return w.c.flush()
}

// makeBackdrop allocates the single dark pixel the viewport stretches over
// the window, behind the frame.
func (w *Window) makeBackdrop() error {
	pool, err := NewShmPool(4)
	if err != nil {
		return err
	}
	mem, err := pool.Bytes()
	if err != nil {
		pool.Close()
		return err
	}
	copy(mem, []byte{0x10, 0x10, 0x10, 0xff}) // B, G, R, X: near black
	id, err := w.c.ImportPool(pool)
	if err != nil {
		pool.Close()
		return err
	}
	w.backdropPool, w.backdropPl = pool, id
	w.backdrop = w.c.CreateBuffer(id, 0, 1, 1, 4, shmFormatXRGB8888, nil)
	return nil
}

// fit is the largest size of the frame's proportions that fits the window.
func fit(bufW, bufH, winW, winH int) (int, int) {
	if bufW <= 0 || bufH <= 0 || winW <= 0 || winH <= 0 {
		return winW, winH
	}
	if winW*bufH <= winH*bufW { // the window is narrower than the frame
		return winW, max(1, winW*bufH/bufW)
	}
	return max(1, winH*bufW/bufH), winH
}

func (w *Window) newSurface(handler func(int, []any) error) uint32 {
	id := w.c.allocID()
	w.c.register(id, ifaceWlSurface, protoIfaces[ifaceWlSurface].Version, handler)
	w.c.send(w.comp, reqWlCompositorCreateSurface, id)
	return id
}

func (w *Window) newViewport(surf uint32) uint32 {
	id := w.c.allocID()
	w.c.register(id, ifaceWpViewport, 1, nil)
	w.c.send(w.viewporter, reqWpViewporterGetViewport, id, surf)
	return id
}

// Client is the underlying connection, for pools and buffers.
func (w *Window) Client() *Client { return w.c }

// Closed reports whether the human closed the window.
func (w *Window) Closed() bool { return w.closed }

// IsConfigured reports whether the compositor has configured the window.
func (w *Window) IsConfigured() bool { return w.configured }

// SupportsShmFormat reports whether the compositor accepts a wl_shm format.
func (w *Window) SupportsShmFormat(format uint32) bool {
	return format == shmFormatARGB8888 || format == shmFormatXRGB8888 || w.shmFormats[format]
}

// ShowFrame attaches a captured frame to the image surface, scaled by its
// viewport to the largest size of the frame's proportions that fits the
// window and centred on the backdrop, flipped when the capture said so.
// Then it asks for a frame callback: the compositor answers it when it is
// ready for the next frame, which it only is while the window is visible.
// bufW x bufH is the buffer size; winW x winH is the window.
func (w *Window) ShowFrame(buf uint32, bufW, bufH, winW, winH int, yInverted bool) error {
	c := w.c
	fitW, fitH := fit(bufW, bufH, winW, winH)
	relaid := w.layout.winW != winW || w.layout.winH != winH || w.layout.fitW != fitW || w.layout.fitH != fitH
	if relaid {
		w.layout.winW, w.layout.winH, w.layout.fitW, w.layout.fitH = winW, winH, fitW, fitH
		c.send(w.viewport, reqWpViewportSetDestination, int32(winW), int32(winH))
		c.send(w.surface, reqWlSurfaceAttach, w.backdrop, int32(0), int32(0))
		c.send(w.surface, reqWlSurfaceDamageBuffer, int32(0), int32(0), int32(1), int32(1))
		c.send(w.xdg, reqXdgSurfaceSetWindowGeometry, int32(0), int32(0), int32(winW), int32(winH))
		c.send(w.imageSub, reqWlSubsurfaceSetPosition, int32((winW-fitW)/2), int32((winH-fitH)/2))
	}
	transform := int32(0)
	if yInverted {
		transform = 6 // WL_OUTPUT_TRANSFORM_FLIPPED_180: a vertical flip
	}
	c.send(w.image, reqWlSurfaceSetBufferTransform, transform)
	c.send(w.imageVp, reqWpViewportSetDestination, int32(fitW), int32(fitH))
	c.send(w.image, reqWlSurfaceAttach, buf, int32(0), int32(0))
	c.send(w.image, reqWlSurfaceDamageBuffer, int32(0), int32(0), int32(bufW), int32(bufH))
	cb := c.allocID()
	c.register(cb, ifaceWlCallback, 1, func(op int, args []any) error {
		if op == evtWlCallbackDone && w.OnFrame != nil {
			w.OnFrame()
		}
		return nil
	})
	c.send(w.image, reqWlSurfaceFrame, cb)
	c.send(w.image, reqWlSurfaceCommit)
	if relaid {
		// The position and the backdrop are state of the parent: they apply
		// on its commit, which also maps the window the first time.
		c.send(w.surface, reqWlSurfaceCommit)
	}
	return c.flush()
}

// SetFullscreen asks the compositor to make the window fullscreen on the
// output it is on. Sent once mapped, so a placement rule applied at map
// time (a silent workspace) keeps precedence.
func (w *Window) SetFullscreen() error {
	w.c.send(w.toplevel, reqXdgToplevelSetFullscreen, uint32(0))
	return w.c.flush()
}

// Close tears the window down.
func (w *Window) Close() error {
	if w.backdropPool != nil {
		w.backdropPool.Close()
		w.backdropPool = nil
	}
	return w.c.Close()
}

// MainDevice connects to a compositor (display, or $WAYLAND_DISPLAY when
// empty), reads which DRM device it renders with and disconnects. See
// Client.MainDevice.
func MainDevice(display string) (uint64, error) {
	fd, err := dial(display)
	if err != nil {
		return 0, err
	}
	c := newClient(fd, display)
	defer c.Close()
	if err := c.initRegistry(); err != nil {
		return 0, err
	}
	return c.MainDevice()
}

// MainDevice returns the DRM device the compositor renders with, as the
// dev_t of its node, through the linux-dmabuf feedback. It is how a client
// learns which GPU to allocate on, and how hyprcage learns which GPU cage
// should render on to stay on the human's. Zero when the compositor does
// not say (linux-dmabuf older than version 4).
func (c *Client) MainDevice() (uint64, error) {
	g, ok := c.findGlobal(ifaceZwpLinuxDmabufV1)
	if !ok {
		return 0, missingErr(ifaceZwpLinuxDmabufV1)
	}
	if g.Version < 4 {
		return 0, nil
	}
	mgr, _ := c.bind(ifaceZwpLinuxDmabufV1, nil)
	fb := c.allocID()
	var dev uint64
	done := false
	c.register(fb, ifaceZwpLinuxDmabufFeedbackV1, 4, func(op int, args []any) error {
		switch op {
		case evtZwpLinuxDmabufFeedbackV1MainDevice:
			if b := args[0].([]byte); len(b) == 8 {
				dev = binary.LittleEndian.Uint64(b)
			}
		case evtZwpLinuxDmabufFeedbackV1FormatTable:
			closeFD(args[0].(int)) // the table is not needed here
		case evtZwpLinuxDmabufFeedbackV1Done:
			done = true
		}
		return nil
	})
	c.send(mgr, reqZwpLinuxDmabufV1GetDefaultFeedback, fb)
	err := c.waitFor(func() bool { return done })
	c.destroyObject(fb, reqZwpLinuxDmabufFeedbackV1Destroy)
	c.destroyObject(mgr, reqZwpLinuxDmabufV1Destroy)
	if err != nil {
		return 0, fmt.Errorf("wl: reading the dmabuf feedback: %w", err)
	}
	return dev, c.flush()
}
