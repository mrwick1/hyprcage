//go:build linux

package mirror

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/screen"
	"github.com/hexadecimil/offstage/internal/session"
	"github.com/hexadecimil/offstage/internal/wl"
	"golang.org/x/sys/unix"
)

// buffer is one slot of the shared pool: a wl_buffer on each side.
type buffer struct {
	cage, human uint32
	held        bool // attached on the human's side, not released yet
}

// Mirror is one running mirror.
type Mirror struct {
	rec  *registry.Screen
	opts Options
	cage *wl.Client
	win  *wl.Window

	pool    *wl.ShmPool
	poolW   uint32 // frame geometry the pool was made for
	poolH   uint32
	stride  uint32
	format  uint32
	bufs    []buffer
	cagePl  uint32 // cage's wl_shm_pool over the shared file
	humanPl uint32

	sigR, sigW int       // self pipe: a signal wakes the poll loop
	inFlight   bool      // a screencopy frame is pending
	wanted     bool      // the human's compositor asked for a frame
	first      bool      // the first frame is copied without waiting for damage
	lastAsk    time.Time // when the last frame was asked for, for the fps cap
	shown      int       // frames shown, for the log
	placed     bool
	pending    int // buffer slot cage is copying into for the frame in flight
	winW, winH int
}

// Run mirrors the screen until the window is closed or a side goes away.
// It returns the process exit code.
func Run(rec *registry.Screen, opts Options) int {
	if opts.FPS <= 0 {
		opts.FPS = 30
	}
	if opts.Log == nil {
		opts.Log = func(string, ...any) {}
	}
	m := &Mirror{rec: rec, opts: opts, first: true, wanted: true}
	if err := m.open(); err != nil {
		opts.Log("mirror: %v", err)
		return ExitError
	}
	defer m.close()
	code := m.loop()
	opts.Log("mirror: exit %d after %d frames", code, m.shown)
	return code
}

func (m *Mirror) open() error {
	inner, err := registry.ReadInner(m.rec.Name)
	if err != nil {
		return fmt.Errorf("screen %s has no inner socket", m.rec.Name)
	}
	m.cage, err = wl.ConnectCapture(inner["WAYLAND_DISPLAY"])
	if err != nil {
		return fmt.Errorf("connecting to cage: %w", err)
	}
	if missing := m.cage.Missing(); len(missing) > 0 {
		return fmt.Errorf("cage lacks %v", missing)
	}
	m.win, err = wl.OpenWindow(m.opts.Display, "offstage-mirror", "offstage mirror: "+m.rec.Name)
	if err != nil {
		return fmt.Errorf("opening the window: %w", err)
	}
	m.win.OnConfigure = m.onConfigure
	m.win.OnFrame = func() { m.wanted = true }
	m.win.OnClose = func() {}
	if err := m.win.Start(); err != nil {
		return fmt.Errorf("mapping the window: %w", err)
	}
	m.opts.Log("mirror: opened, configured=%v size=%dx%d", m.win.IsConfigured(), m.win.Configured.W, m.win.Configured.H)
	// A self pipe carries SIGTERM into the poll loop, so that the mirror
	// shuts down cleanly (sidecar removed, count logged) when the screen is
	// destroyed rather than being killed mid-frame.
	fds := make([]int, 2)
	if err := unix.Pipe2(fds, unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		return fmt.Errorf("signal pipe: %w", err)
	}
	m.sigR, m.sigW = fds[0], fds[1]
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		<-ch
		_, _ = unix.Write(m.sigW, []byte{1})
	}()
	return nil
}

func (m *Mirror) close() {
	if m.sigW != 0 {
		_ = unix.Close(m.sigW)
		_ = unix.Close(m.sigR)
	}
	m.freePool()
	if m.win != nil {
		m.win.Close()
	}
	if m.cage != nil {
		m.cage.Close()
	}
	_ = os.Remove(screen.MirrorPath(m.rec.Name))
}

// onConfigure records the window size the compositor gave and, on the first
// one, fullscreens the window and publishes the sidecar. The frame is
// scaled to this size by the viewport in ShowFrame; a zero size (the
// compositor leaving the choice to us) means the screen's own size.
func (m *Mirror) onConfigure(w, h int) {
	m.opts.Log("mirror: onConfigure %dx%d", w, h)
	if w == 0 || h == 0 {
		w, h = m.rec.Width, m.rec.Height
	}
	m.winW, m.winH = w, h
	if !m.placed {
		m.placed = true
		start, _ := session.ProcStart(os.Getpid())
		_ = screen.WriteMirror(m.rec.Name, screen.MirrorState{PID: os.Getpid(), PIDStart: start, Workspace: m.opts.Workspace})
	}
}

// loop is the poll loop: the two sockets, and a timer for the fps cap.
func (m *Mirror) loop() int {
	fds := []unix.PollFd{
		{Fd: int32(m.cage.Fd()), Events: unix.POLLIN},
		{Fd: int32(m.win.Client().Fd()), Events: unix.POLLIN},
		{Fd: int32(m.sigR), Events: unix.POLLIN},
	}
	for {
		if m.win.Closed() {
			return ExitClosed
		}
		timeout := m.kick()
		if err := m.cage.Flush(); err != nil {
			return ExitCageGone
		}
		if err := m.win.Client().Flush(); err != nil {
			return ExitHumanGone
		}
		fds[0].Revents, fds[1].Revents, fds[2].Revents = 0, 0, 0
		n, err := unix.Poll(fds, timeout)
		if err != nil && !errors.Is(err, unix.EINTR) {
			return ExitError
		}
		if n <= 0 {
			continue
		}
		if fds[2].Revents != 0 {
			return ExitClosed
		}
		if fds[0].Revents != 0 {
			if err := m.cage.DispatchPending(); err != nil {
				m.opts.Log("mirror: cage: %v", err)
				return ExitCageGone
			}
		}
		if fds[1].Revents != 0 {
			if err := m.win.Client().DispatchPending(); err != nil {
				m.opts.Log("mirror: compositor: %v", err)
				return ExitHumanGone
			}
		}
	}
}

// kick asks cage for a frame when everything lines up: the window is
// mapped, the compositor asked for a frame, none is in flight, and the fps
// cap allows it. It returns the poll timeout in milliseconds: -1 to wait
// for events, else the time left on the cap.
func (m *Mirror) kick() int {
	if m.inFlight || !m.wanted || !m.placed {
		return -1
	}
	if m.freeBuffer() < 0 && len(m.bufs) > 0 {
		return -1 // both buffers still held by the compositor; wait for release
	}
	minGap := time.Second / time.Duration(m.opts.FPS)
	if wait := minGap - time.Since(m.lastAsk); wait > 0 {
		return int(wait/time.Millisecond) + 1
	}
	m.inFlight = true
	m.lastAsk = time.Now()
	m.wanted = false
	// The first frame has nothing to wait for; later ones wait for the
	// screen to actually change, so a still screen asks nothing of cage.
	opts := wl.FrameOptions{WithDamage: !m.first}
	if err := m.cage.StartFrame(opts, m.announce, m.ready); err != nil {
		m.inFlight = false
		m.opts.Log("mirror: start frame: %v", err)
	}
	return -1
}

// announce is called by the frame machine once cage has described the
// buffer it wants: the mirror sizes its shared pool to match and hands back
// a free wl_buffer for cage to copy into.
func (m *Mirror) announce(f *wl.Frame) uint32 {
	if f.Width == 0 || f.Height == 0 || f.Stride < f.Width*4 {
		return 0
	}
	if !m.win.SupportsShmFormat(wl.OpaqueFormat(f.Format)) {
		m.opts.Log("mirror: the compositor does not accept format 0x%08x", f.Format)
		return 0
	}
	if m.pool == nil || f.Width != m.poolW || f.Height != m.poolH || f.Stride != m.stride || f.Format != m.format {
		if err := m.makePool(f); err != nil {
			m.opts.Log("mirror: pool: %v", err)
			return 0
		}
	}
	slot := m.freeBuffer()
	if slot < 0 {
		return 0
	}
	m.pending = slot
	return m.bufs[slot].cage
}

// ready is called when cage has copied the frame: the mirror shows the same
// buffer, verbatim, on the human's side.
func (m *Mirror) ready(f *wl.Frame, err error) {
	m.inFlight = false
	if err != nil {
		m.opts.Log("mirror: frame: %v", err)
		return
	}
	slot := m.pending
	if slot < 0 || slot >= len(m.bufs) {
		return
	}
	b := &m.bufs[slot]
	b.held = true
	first := m.first
	m.first = false
	m.shown++
	if e := m.win.ShowFrame(b.human, int(f.Width), int(f.Height), m.winW, m.winH, f.YInverted()); e != nil {
		m.opts.Log("mirror: show: %v", e)
	}
	// The first frame maps the window. Fullscreen is asked for after that:
	// Hyprland does not honour the request from a window that is not mapped
	// yet, and asking once mapped also lets the placement rules (a silent
	// workspace) apply first.
	if first && m.opts.Fullscreen {
		_ = m.win.SetFullscreen()
	}
}

// makePool allocates the shared memory file and two buffers over it, one
// pair of wl_buffers per side, sized for the frame cage announced.
func (m *Mirror) makePool(f *wl.Frame) error {
	m.freePool()
	m.poolW, m.poolH, m.stride, m.format = f.Width, f.Height, f.Stride, f.Format
	// The human's compositor gets the buffer in the alpha-less twin of the
	// format when there is one: the bytes are the same, and a capture whose
	// alpha channel is zero (pixman leaves it so) would otherwise be blended
	// away to the backdrop.
	human := wl.OpaqueFormat(f.Format)
	m.opts.Log("mirror: frames %dx%d stride %d format 0x%08x, shown as 0x%08x", f.Width, f.Height, f.Stride, f.Format, human)
	size := int(f.Stride) * int(f.Height)
	pool, err := wl.NewShmPool(size * 2) // two buffers
	if err != nil {
		return err
	}
	m.pool = pool
	if m.cagePl, err = m.cage.ImportPool(pool); err != nil {
		return err
	}
	if m.humanPl, err = m.win.Client().ImportPool(pool); err != nil {
		return err
	}
	m.bufs = make([]buffer, 2)
	for i := range m.bufs {
		off := i * size
		slot := i
		m.bufs[i].cage = m.cage.CreateBuffer(m.cagePl, off, int(f.Width), int(f.Height), int(f.Stride), f.Format, nil)
		m.bufs[i].human = m.win.Client().CreateBuffer(m.humanPl, off, int(f.Width), int(f.Height), int(f.Stride), human, func() { m.bufs[slot].held = false })
	}
	return nil
}

func (m *Mirror) freePool() {
	for i := range m.bufs {
		m.cage.DestroyBuffer(m.bufs[i].cage)
		m.win.Client().DestroyBuffer(m.bufs[i].human)
	}
	m.bufs = nil
	if m.cagePl != 0 {
		m.cage.DestroyPool(m.cagePl)
		m.cagePl = 0
	}
	if m.humanPl != 0 {
		m.win.Client().DestroyPool(m.humanPl)
		m.humanPl = 0
	}
	if m.pool != nil {
		m.pool.Close()
		m.pool = nil
	}
}

// freeBuffer is a buffer the compositor is not reading, or -1 when both are
// held.
func (m *Mirror) freeBuffer() int {
	for i := range m.bufs {
		if !m.bufs[i].held {
			return i
		}
	}
	return -1
}
