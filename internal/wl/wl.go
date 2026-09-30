// Package wl is hyprcage's Wayland client to the nested cage compositor:
// virtual pointer and keyboard for input, screencopy for capture, foreign
// toplevel for the window list. Pure Go, no cgo, no external dependency
// (cahier §4.4, §4.5, §4.9 and spike S7).
//
// This file fixes the API the rest of hyprcage relies on. The wire protocol
// implementation replaces the stubs below.
package wl

//go:generate go run ./gen

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"os"
	"sort"
	"syscall"
	"time"
)

// wlDebug traces every request and event on stderr when HYPRCAGE_WL_DEBUG is
// set. There is no other way to see this wire protocol: WAYLAND_DEBUG is a
// libwayland feature and this client does not use libwayland.
var wlDebug = os.Getenv("HYPRCAGE_WL_DEBUG") != ""

// ErrNotImplemented is returned by the stubs until the protocol code lands.
var ErrNotImplemented = errors.New("wl: not implemented yet (spike S7)")

// ErrMissingProtocol is the root of the errors returned when the compositor
// does not advertise an interface hyprcage needs. Test it with errors.Is; the
// wrapping error names the interface.
var ErrMissingProtocol = errors.New("wl: protocol not advertised by the compositor")

func missingErr(iface string) error {
	return fmt.Errorf("wl: compositor does not advertise %s: %w", iface, ErrMissingProtocol)
}

// RequiredGlobals are the interfaces cage must advertise for hyprcage to work.
var RequiredGlobals = []string{
	"zwlr_virtual_pointer_manager_v1",
	"zwp_virtual_keyboard_manager_v1",
	"zwlr_screencopy_manager_v1",
	"zwlr_foreign_toplevel_manager_v1",
}

// Global is one interface advertised by the compositor's registry.
type Global struct {
	Name      uint32
	Interface string
	Version   uint32
}

// Toplevel is a window known through zwlr_foreign_toplevel_management_v1.
type Toplevel struct {
	ID         uint32
	Title      string
	AppID      string
	Activated  bool
	Fullscreen bool
	Maximized  bool
	Minimized  bool
}

// Button is a pointer button.
type Button int

const (
	ButtonLeft Button = iota
	ButtonRight
	ButtonMiddle
)

// evdev button codes, as expected by zwlr_virtual_pointer_v1.button.
const (
	btnLeft   = 0x110
	btnRight  = 0x111
	btnMiddle = 0x112
)

// Axis is a scroll axis.
type Axis int

const (
	AxisVertical Axis = iota
	AxisHorizontal
)

// toplevelHandle tracks one zwlr_foreign_toplevel_handle_v1. Events are
// double buffered: title/app_id/state accumulate, done commits.
type toplevelHandle struct {
	id        uint32
	pending   Toplevel
	current   Toplevel
	committed bool
}

func (h *toplevelHandle) snapshot() Toplevel {
	if h.committed {
		return h.current
	}
	return h.pending
}

// Client is one connection to a compositor.
type Client struct {
	display string

	fd     int
	closed bool

	out    []byte // requests waiting to be flushed
	outFDs []int  // file descriptors travelling with the pending requests
	in     []byte // bytes received, not yet framed
	inFDs  []int  // file descriptors received, not yet consumed

	objects map[uint32]*object
	nextID  uint32
	freeIDs []uint32

	start time.Time

	sendErr  error
	protoErr error
	wireErr  error

	globals []Global
	missing []string

	registry uint32

	shm      uint32
	seat     uint32
	output   uint32
	vpMgr    uint32
	vpMgrVer int
	vkMgr    uint32
	scMgr    uint32
	scMgrVer int
	teMgr    uint32 // Hyprland toplevel export, absent under cage
	teMgrVer int
	ftMgr    uint32

	pointer  uint32
	keyboard uint32

	modeW, modeH int
	outScale     int

	handles map[uint32]*toplevelHandle

	om *omState // wlr-output-management, bound on first SetMode

	// heldMods are the modifier keys kept pressed by HoldModifiers.
	heldMods []comboModifier

	// wantInput records that this connection may drive input; the virtual
	// devices are created on first use, not at connection time.
	wantInput bool
}

// newClient wraps an already connected socket.
func newClient(fd int, display string) *Client {
	c := &Client{
		display:  display,
		fd:       fd,
		objects:  make(map[uint32]*object),
		nextID:   2,
		start:    time.Now(),
		handles:  make(map[uint32]*toplevelHandle),
		outScale: 1,
	}
	c.objects[displayID] = &object{id: displayID, iface: ifaceWlDisplay, version: 1, handler: c.handleDisplay}
	return c
}

// Connect opens the Wayland socket named display (relative to
// $XDG_RUNTIME_DIR, or an absolute path), fetches the registry and binds
// what hyprcage needs, input devices included.
func Connect(display string) (*Client, error) {
	return connect(display, true)
}

// ConnectCapture connects for capture only: no virtual pointer, no virtual
// keyboard, no seat. It is the mirror's connection, which must not be able
// to inject anything into the screen.
func ConnectCapture(display string) (*Client, error) {
	return connect(display, false)
}

func connect(display string, input bool) (*Client, error) {
	fd, err := dial(display)
	if err != nil {
		return nil, err
	}
	c := newClient(fd, display)
	if err := c.setup(input); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// Probe connects, reads the registry and disconnects. It binds nothing and
// creates no device: it is safe to run against any compositor.
func Probe(display string) ([]Global, error) {
	fd, err := dial(display)
	if err != nil {
		return nil, err
	}
	c := newClient(fd, display)
	defer c.Close()
	if err := c.initRegistry(); err != nil {
		return nil, err
	}
	return c.Globals(), nil
}

// initRegistry asks for the registry and waits for the globals.
func (c *Client) initRegistry() error {
	c.registry = c.allocID()
	c.register(c.registry, ifaceWlRegistry, 1, c.handleRegistry)
	c.send(displayID, reqWlDisplayGetRegistry, c.registry)
	return c.Roundtrip()
}

// setup runs the whole connection sequence: registry, binds and, when
// input is set, the virtual devices.
func (c *Client) setup(input bool) error {
	if err := c.initRegistry(); err != nil {
		return err
	}

	c.shm, _ = c.bind(ifaceWlShm, nil)
	c.output, _ = c.bind(ifaceWlOutput, c.handleOutput)
	c.scMgr, c.scMgrVer = c.bind(ifaceZwlrScreencopyManagerV1, nil)
	c.teMgr, c.teMgrVer = c.bind(ifaceHyprlandToplevelExportManagerV1, nil)
	if input {
		c.seat, _ = c.bind(ifaceWlSeat, nil)
		c.vpMgr, c.vpMgrVer = c.bind(ifaceZwlrVirtualPointerManagerV1, nil)
		c.vkMgr, _ = c.bind(ifaceZwpVirtualKeyboardManagerV1, nil)
		c.ftMgr, _ = c.bind(ifaceZwlrForeignToplevelManagerV1, c.handleToplevelManager)
	}

	c.wantInput = input
	required := RequiredGlobals
	if !input {
		required = []string{ifaceZwlrScreencopyManagerV1}
	}
	for _, want := range required {
		if _, ok := c.findGlobal(want); !ok {
			c.missing = append(c.missing, want)
		}
	}

	// Second roundtrip: output mode, seat capabilities, initial toplevels.
	// The virtual pointer and keyboard are created later, lazily, by
	// ensureInput: on a headless cage the seat has no real devices, so the
	// virtual ones are its only capabilities, and creating them here, before
	// SetMode reconfigures the output, would make that reconfiguration drop
	// them and flap the seat under every client.
	return c.Roundtrip()
}

// EnsureInput creates the virtual devices now rather than on first use, so
// that the seat already has a keyboard and a pointer when an application
// starts: an application that has to bind them mid-session can miss the
// first key sent right after.
func (c *Client) EnsureInput() error { return c.ensureInput() }

// ensureInput creates the virtual pointer and keyboard on first use. The
// pointer is not tied to an output: motion carries the geometry, and a
// pointer bound to the output would die when SetMode changes it.
func (c *Client) ensureInput() error {
	if !c.wantInput {
		return errors.New("wl: this connection was opened without input")
	}
	if c.pointer == 0 && c.vpMgr != 0 && c.seat != 0 {
		c.pointer = c.allocID()
		c.register(c.pointer, ifaceZwlrVirtualPointerV1, c.vpMgrVer, nil)
		c.send(c.vpMgr, reqZwlrVirtualPointerManagerV1CreateVirtualPointer, c.seat, c.pointer)
	}
	if c.keyboard == 0 && c.vkMgr != 0 && c.seat != 0 {
		c.keyboard = c.allocID()
		c.register(c.keyboard, ifaceZwpVirtualKeyboardV1, 1, nil)
		c.send(c.vkMgr, reqZwpVirtualKeyboardManagerV1CreateVirtualKeyboard, c.seat, c.keyboard)
		if err := c.sendKeymap(newKeymap()); err != nil {
			return err
		}
	}
	return c.Roundtrip()
}

// Close releases every object and the socket.
func (c *Client) Close() error {
	if c.closed {
		return nil
	}
	if c.protoErr == nil && c.sendErr == nil {
		if c.pointer != 0 {
			c.send(c.pointer, reqZwlrVirtualPointerV1Destroy)
		}
		if c.keyboard != 0 {
			c.send(c.keyboard, reqZwpVirtualKeyboardV1Destroy)
		}
		if err := c.flushLocked(); err != nil {
			c.sendErr = err
		}
	}
	c.closed = true
	for _, fd := range c.inFDs {
		syscall.Close(fd)
	}
	c.inFDs = nil
	err := syscall.Close(c.fd)
	c.fd = -1
	return err
}

// Globals returns the registry as advertised at connection time.
func (c *Client) Globals() []Global {
	out := make([]Global, len(c.globals))
	copy(out, c.globals)
	return out
}

// Missing returns the RequiredGlobals the compositor does not advertise.
func (c *Client) Missing() []string {
	out := make([]string, len(c.missing))
	copy(out, c.missing)
	return out
}

// OutputSize returns the logical size of the compositor's (single) output.
func (c *Client) OutputSize() (width, height int, err error) {
	if c.output == 0 {
		return 0, 0, missingErr(ifaceWlOutput)
	}
	if c.modeW <= 0 || c.modeH <= 0 {
		return 0, 0, errors.New("wl: the compositor reported no current output mode")
	}
	w, h := c.modeW, c.modeH
	if c.outScale > 1 {
		w /= c.outScale
		h /= c.outScale
	}
	return w, h, nil
}

// ------------------------------------------------------------------ pointer

// pointerErr explains why there is no virtual pointer.
func (c *Client) pointerErr() error {
	if c.vpMgr == 0 {
		return missingErr(ifaceZwlrVirtualPointerManagerV1)
	}
	return missingErr(ifaceWlSeat)
}

// Move warps the virtual pointer to absolute screen coordinates.
func (c *Client) Move(x, y int) error {
	w, h, err := c.OutputSize()
	if err != nil {
		return err
	}
	return c.MoveIn(x, y, w, h)
}

// MoveIn warps the virtual pointer to (x, y) in an extent of w by h.
// Hyprland maps the extent over its whole layout.
func (c *Client) MoveIn(x, y, w, h int) error {
	if err := c.ensureInput(); err != nil {
		return err
	}
	if c.pointer == 0 {
		return c.pointerErr()
	}
	x = clamp(x, 0, w-1)
	y = clamp(y, 0, h-1)
	c.send(c.pointer, reqZwlrVirtualPointerV1MotionAbsolute, c.now(), uint32(x), uint32(y), uint32(w), uint32(h))
	c.send(c.pointer, reqZwlrVirtualPointerV1Frame)
	return c.Roundtrip()
}

// PressButton presses or releases a pointer button at the current position.
func (c *Client) PressButton(b Button, pressed bool) error {
	if err := c.ensureInput(); err != nil {
		return err
	}
	if c.pointer == 0 {
		return c.pointerErr()
	}
	var code uint32
	switch b {
	case ButtonLeft:
		code = btnLeft
	case ButtonRight:
		code = btnRight
	case ButtonMiddle:
		code = btnMiddle
	default:
		return fmt.Errorf("wl: unknown pointer button %d", int(b))
	}
	var state uint32
	if pressed {
		state = 1
	}
	c.send(c.pointer, reqZwlrVirtualPointerV1Button, c.now(), code, state)
	c.send(c.pointer, reqZwlrVirtualPointerV1Frame)
	return c.Roundtrip()
}

// Scroll emits steps wheel clicks on an axis; positive is down or right.
func (c *Client) Scroll(axis Axis, steps int) error {
	if err := c.ensureInput(); err != nil {
		return err
	}
	if c.pointer == 0 {
		return c.pointerErr()
	}
	var ax uint32
	switch axis {
	case AxisVertical:
		ax = 0
	case AxisHorizontal:
		ax = 1
	default:
		return fmt.Errorf("wl: unknown scroll axis %d", int(axis))
	}
	if steps == 0 {
		return nil
	}
	sign := 1
	if steps < 0 {
		sign = -1
	}
	n := steps * sign
	// zwlr_virtual_pointer_v1 has no axis_value120: the distance goes in
	// value, the number of detents in discrete.
	c.send(c.pointer, reqZwlrVirtualPointerV1AxisSource, uint32(0)) // wheel
	for i := 0; i < n; i++ {
		c.send(c.pointer, reqZwlrVirtualPointerV1AxisDiscrete, c.now(), ax, fixedFromInt(15*sign), int32(sign))
		c.send(c.pointer, reqZwlrVirtualPointerV1Frame)
	}
	return c.Roundtrip()
}

// ----------------------------------------------------------------- windows

// Toplevels lists the compositor's windows.
func (c *Client) Toplevels() ([]Toplevel, error) {
	if c.ftMgr == 0 {
		return nil, missingErr(ifaceZwlrForeignToplevelManagerV1)
	}
	if err := c.Roundtrip(); err != nil {
		return nil, err
	}
	out := make([]Toplevel, 0, len(c.handles))
	for _, h := range c.handles {
		out = append(out, h.snapshot())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// CloseToplevel asks a window to close.
func (c *Client) CloseToplevel(id uint32) error {
	if c.ftMgr == 0 {
		return missingErr(ifaceZwlrForeignToplevelManagerV1)
	}
	if _, ok := c.handles[id]; !ok {
		return fmt.Errorf("wl: unknown toplevel %d", id)
	}
	c.send(id, reqZwlrForeignToplevelHandleV1Close)
	return c.Roundtrip()
}

// Capture grabs the output through screencopy.
func (c *Client) Capture(overlayCursor bool) (*image.RGBA, error) {
	return c.capture(overlayCursor)
}

// CaptureToplevel captures one window by its Hyprland address (the low 32
// bits are the handle of hyprland_toplevel_export_manager_v1), on any workspace.
func (c *Client) CaptureToplevel(handle uint32, overlayCursor bool) (*image.RGBA, error) {
	return c.captureToplevel(handle, overlayCursor)
}

// ----------------------------------------------------------------- handlers

func (c *Client) handleDisplay(op int, args []any) error {
	switch op {
	case evtWlDisplayError:
		objID := args[0].(uint32)
		code := args[1].(uint32)
		msg := args[2].(string)
		iface := "unknown"
		if o, ok := c.objects[objID]; ok {
			iface = o.iface
		}
		c.protoErr = fmt.Errorf("wl: protocol error on %s#%d: code %d: %s", iface, objID, code, msg)
	case evtWlDisplayDeleteId:
		id := args[0].(uint32)
		if _, ok := c.objects[id]; ok {
			delete(c.objects, id)
			if id < firstServerID {
				c.freeIDs = append(c.freeIDs, id)
			}
		}
	}
	return nil
}

func (c *Client) handleRegistry(op int, args []any) error {
	switch op {
	case evtWlRegistryGlobal:
		c.globals = append(c.globals, Global{
			Name:      args[0].(uint32),
			Interface: args[1].(string),
			Version:   args[2].(uint32),
		})
	case evtWlRegistryGlobalRemove:
		name := args[0].(uint32)
		for i, g := range c.globals {
			if g.Name == name {
				c.globals = append(c.globals[:i], c.globals[i+1:]...)
				break
			}
		}
	}
	return nil
}

func (c *Client) handleOutput(op int, args []any) error {
	switch op {
	case evtWlOutputMode:
		flags := args[0].(uint32)
		if flags&0x1 != 0 { // current
			c.modeW = int(args[1].(int32))
			c.modeH = int(args[2].(int32))
		}
	case evtWlOutputScale:
		if f := int(args[0].(int32)); f > 0 {
			c.outScale = f
		}
	}
	return nil
}

func (c *Client) handleToplevelManager(op int, args []any) error {
	switch op {
	case evtZwlrForeignToplevelManagerV1Toplevel:
		id := args[0].(uint32)
		h := &toplevelHandle{id: id}
		h.pending.ID = id
		c.handles[id] = h
		c.register(id, ifaceZwlrForeignToplevelHandleV1, protoIfaces[ifaceZwlrForeignToplevelHandleV1].Version,
			func(op int, args []any) error { return c.handleToplevel(h, op, args) })
	case evtZwlrForeignToplevelManagerV1Finished:
		c.ftMgr = 0
	}
	return nil
}

func (c *Client) handleToplevel(h *toplevelHandle, op int, args []any) error {
	switch op {
	case evtZwlrForeignToplevelHandleV1Title:
		h.pending.Title = args[0].(string)
	case evtZwlrForeignToplevelHandleV1AppId:
		h.pending.AppID = args[0].(string)
	case evtZwlrForeignToplevelHandleV1State:
		raw := args[0].([]byte)
		h.pending.Maximized = false
		h.pending.Minimized = false
		h.pending.Activated = false
		h.pending.Fullscreen = false
		for i := 0; i+4 <= len(raw); i += 4 {
			switch binary.LittleEndian.Uint32(raw[i : i+4]) {
			case 0:
				h.pending.Maximized = true
			case 1:
				h.pending.Minimized = true
			case 2:
				h.pending.Activated = true
			case 3:
				h.pending.Fullscreen = true
			}
		}
	case evtZwlrForeignToplevelHandleV1Done:
		h.current = h.pending
		h.committed = true
	case evtZwlrForeignToplevelHandleV1Closed:
		delete(c.handles, h.id)
		c.send(h.id, reqZwlrForeignToplevelHandleV1Destroy)
		delete(c.objects, h.id)
	}
	return nil
}

// ------------------------------------------------------------------ plumbing

// findGlobal returns the advertised global for an interface name.
func (c *Client) findGlobal(iface string) (Global, bool) {
	for _, g := range c.globals {
		if g.Interface == iface {
			return g, true
		}
	}
	return Global{}, false
}

// bind binds a global at min(advertised version, version known from the XML).
// It returns 0 when the compositor does not advertise the interface.
func (c *Client) bind(iface string, handler func(int, []any) error) (uint32, int) {
	g, ok := c.findGlobal(iface)
	if !ok {
		return 0, 0
	}
	spec, ok := protoIfaces[iface]
	if !ok {
		return 0, 0
	}
	version := int(g.Version)
	if version > spec.Version {
		version = spec.Version
	}
	id := c.allocID()
	c.register(id, iface, version, handler)
	c.send(c.registry, reqWlRegistryBind, g.Name, newIDAny{Interface: iface, Version: uint32(version), ID: id})
	return id, version
}

// register adds an object to the dispatch table. Ids at or above
// firstServerID were allocated by the compositor.
func (c *Client) register(id uint32, iface string, version int, handler func(int, []any) error) {
	c.objects[id] = &object{id: id, iface: iface, version: version, handler: handler}
}

func (c *Client) allocID() uint32 {
	if n := len(c.freeIDs); n > 0 {
		id := c.freeIDs[n-1]
		c.freeIDs = c.freeIDs[:n-1]
		return id
	}
	id := c.nextID
	c.nextID++
	return id
}

// destroyObject sends a destructor request and forgets the object. The id is
// only recycled when the compositor acknowledges with wl_display.delete_id.
func (c *Client) destroyObject(id uint32, op int) {
	if _, ok := c.objects[id]; !ok {
		return
	}
	c.send(id, op)
}

// now is the client's monotonic clock in milliseconds, as the input
// protocols expect.
func (c *Client) now() uint32 {
	return uint32(time.Since(c.start).Milliseconds())
}

// send queues a request. Errors are latched and reported by the next flush,
// the way bufio.Writer does.
func (c *Client) send(id uint32, op int, args ...any) {
	if c.sendErr != nil {
		return
	}
	if c.closed {
		c.sendErr = ErrClosed
		return
	}
	obj, ok := c.objects[id]
	if !ok {
		c.sendErr = fmt.Errorf("wl: request on unknown object %d", id)
		return
	}
	spec, ok := protoIfaces[obj.iface]
	if !ok || op >= len(spec.Requests) {
		c.sendErr = fmt.Errorf("wl: no request %d on %s", op, obj.iface)
		return
	}
	m := &spec.Requests[op]
	if wlDebug {
		fmt.Fprintf(os.Stderr, "wl-> %s#%d.%s %v\n", obj.iface, id, m.Name, args)
	}
	buf, fds, err := encodeMessage(id, m, args)
	if err != nil {
		c.sendErr = err
		return
	}
	if len(fds) > 0 {
		// A file descriptor must travel in the same sendmsg as its message:
		// flush what is pending, then send this message alone.
		if err := c.flushLocked(); err != nil {
			c.sendErr = err
			return
		}
		if err := sendAll(c.fd, buf, fds); err != nil {
			c.sendErr = err
		}
		return
	}
	c.out = append(c.out, buf...)
}

// flush writes the queued requests and reports any latched encoding error.
func (c *Client) flush() error {
	if c.sendErr != nil {
		err := c.sendErr
		c.sendErr = nil
		c.out = c.out[:0]
		return err
	}
	return c.flushLocked()
}

func (c *Client) flushLocked() error {
	if len(c.out) == 0 {
		return nil
	}
	buf := c.out
	c.out = nil
	return sendAll(c.fd, buf, nil)
}

// Roundtrip flushes the pending requests, then reads and dispatches events
// until the compositor answers a wl_display.sync of its own.
func (c *Client) Roundtrip() error {
	if c.protoErr != nil {
		return c.protoErr
	}
	if c.closed {
		return ErrClosed
	}
	cb := c.allocID()
	done := false
	c.register(cb, ifaceWlCallback, 1, func(op int, args []any) error {
		if op == evtWlCallbackDone {
			done = true
		}
		return nil
	})
	c.send(displayID, reqWlDisplaySync, cb)
	if err := c.flush(); err != nil {
		return err
	}
	for !done {
		if err := c.readDispatch(); err != nil {
			return err
		}
		if c.protoErr != nil {
			return c.protoErr
		}
	}
	return c.protoErr
}

// waitFor dispatches events until pred holds. Unlike Roundtrip it does not
// send a sync: it is used when the compositor answers on its own.
func (c *Client) waitFor(pred func() bool) error {
	if err := c.flush(); err != nil {
		return err
	}
	for !pred() {
		if err := c.readDispatch(); err != nil {
			return err
		}
		if c.protoErr != nil {
			return c.protoErr
		}
	}
	return nil
}

// readDispatch handles exactly one event, reading from the socket if needed.
func (c *Client) readDispatch() error {
	for {
		id, op, body, ok := c.takeMessage()
		if c.wireErr != nil {
			return c.wireErr
		}
		if ok {
			return c.dispatch(id, op, body)
		}
		if err := c.fill(); err != nil {
			return err
		}
	}
}

// takeMessage pops one complete message from the receive buffer.
func (c *Client) takeMessage() (id uint32, op int, body []byte, ok bool) {
	if len(c.in) < 8 {
		return 0, 0, nil, false
	}
	id = binary.LittleEndian.Uint32(c.in[0:4])
	word := binary.LittleEndian.Uint32(c.in[4:8])
	size := int(word >> 16)
	op = int(word & 0xffff)
	if size < 8 || size%4 != 0 {
		// Unrecoverable: the stream is desynchronised.
		c.in = nil
		c.wireErr = fmt.Errorf("wl: desynchronised stream: message of %d bytes", size)
		return 0, 0, nil, false
	}
	if len(c.in) < size {
		return 0, 0, nil, false
	}
	body = make([]byte, size-8)
	copy(body, c.in[8:size])
	c.in = c.in[size:]
	return id, op, body, true
}

// fill reads one chunk from the socket.
func (c *Client) fill() error {
	buf := make([]byte, 4096)
	n, fds, err := recvChunk(c.fd, buf)
	if err != nil {
		return err
	}
	c.in = append(c.in, buf[:n]...)
	c.inFDs = append(c.inFDs, fds...)
	return nil
}

// dispatch decodes one event and hands it to its object's handler.
func (c *Client) dispatch(id uint32, op int, body []byte) error {
	obj, ok := c.objects[id]
	if !ok {
		// Event for an object we already destroyed: legitimate race.
		return nil
	}
	spec, ok := protoIfaces[obj.iface]
	if !ok {
		return fmt.Errorf("wl: unknown interface %q for object %d", obj.iface, id)
	}
	if op < 0 || op >= len(spec.Events) {
		return fmt.Errorf("wl: %s#%d: unknown event opcode %d", obj.iface, id, op)
	}
	m := &spec.Events[op]
	args, err := decodeArgs(m.Args, body, &c.inFDs)
	if err != nil {
		return fmt.Errorf("wl: %s.%s: %w", obj.iface, m.Name, err)
	}
	if wlDebug {
		fmt.Fprintf(os.Stderr, "wl<- %s#%d.%s %v\n", obj.iface, id, m.Name, args)
	}
	if obj.handler == nil {
		return nil
	}
	return obj.handler(op, args)
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
