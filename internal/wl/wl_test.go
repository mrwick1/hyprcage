package wl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// ---------------------------------------------------------------- fake server

// record is one request the fake compositor received.
type record struct {
	iface string
	name  string
	args  []any
}

func (r record) key() string { return r.iface + "." + r.name }

type bufferInfo struct {
	pool                          uint32
	offset, width, height, stride int32
	format                        uint32
}

// fakeServer is a minimal compositor speaking the real wire protocol over a
// socketpair. It runs in its own goroutine; the client runs in the test's.
type fakeServer struct {
	t       *testing.T
	fd      int
	globals []Global

	mu      sync.Mutex
	records []record
	keymaps []string
	injErr  bool

	// touched by the server goroutine only
	objects map[uint32]string
	pools   map[uint32]int
	buffers map[uint32]bufferInfo
	nextID  uint32

	closing atomic.Bool

	pixels []byte
	stride int
	width  int
	height int

	// wlr-output-management: the bound wl_output, the mode the client asked
	// for, and whether apply must answer failed.
	outputID   uint32
	askedW     int32
	askedH     int32
	askedScale int32
	refuseMode bool

	stopped chan struct{}
}

func newFakeServer(t *testing.T, fd int, globals []Global) *fakeServer {
	return &fakeServer{
		t:       t,
		fd:      fd,
		globals: globals,
		objects: map[uint32]string{displayID: ifaceWlDisplay},
		pools:   map[uint32]int{},
		buffers: map[uint32]bufferInfo{},
		nextID:  firstServerID,
		stopped: make(chan struct{}),
	}
}

func (s *fakeServer) newServerID() uint32 {
	id := s.nextID
	s.nextID++
	return id
}

// event encodes and sends one event, looked up by name so the test never
// hard codes an opcode either.
func (s *fakeServer) event(objID uint32, iface, name string, args ...any) {
	spec, ok := protoIfaces[iface]
	if !ok {
		s.t.Errorf("fake: unknown interface %s", iface)
		return
	}
	var m *msgSpec
	for i := range spec.Events {
		if spec.Events[i].Name == name {
			m = &spec.Events[i]
			break
		}
	}
	if m == nil {
		s.t.Errorf("fake: %s has no event %s", iface, name)
		return
	}
	buf, fds, err := encodeMessage(objID, m, args)
	if err != nil {
		s.t.Errorf("fake: encoding %s.%s: %v", iface, name, err)
		return
	}
	if err := sendAll(s.fd, buf, fds); err != nil && !s.closing.Load() {
		// A write losing the race with the client's shutdown is expected.
		s.t.Errorf("fake: sending %s.%s: %v", iface, name, err)
	}
}

func (s *fakeServer) serve() {
	defer close(s.stopped)
	buf := make([]byte, 4096)
	var in []byte
	var fds []int
	for {
		n, got, err := recvChunk(s.fd, buf)
		if err != nil {
			return
		}
		in = append(in, buf[:n]...)
		fds = append(fds, got...)
		for len(in) >= 8 {
			word := binary.LittleEndian.Uint32(in[4:8])
			size := int(word >> 16)
			if size < 8 || size%4 != 0 {
				s.t.Errorf("fake: bogus message size %d", size)
				return
			}
			if len(in) < size {
				break
			}
			id := binary.LittleEndian.Uint32(in[0:4])
			op := int(word & 0xffff)
			body := append([]byte(nil), in[8:size]...)
			in = in[size:]
			s.handle(id, op, body, &fds)
		}
	}
}

func (s *fakeServer) handle(id uint32, op int, body []byte, fds *[]int) {
	iface, ok := s.objects[id]
	if !ok {
		s.t.Errorf("fake: request on unknown object %d", id)
		return
	}
	spec := protoIfaces[iface]
	if op < 0 || op >= len(spec.Requests) {
		s.t.Errorf("fake: %s has no request %d", iface, op)
		return
	}
	m := &spec.Requests[op]
	args, err := decodeArgs(m.Args, body, fds)
	if err != nil {
		s.t.Errorf("fake: decoding %s.%s: %v", iface, m.Name, err)
		return
	}
	// Every new_id argument creates an object.
	for i, a := range m.Args {
		if a.Type != argNewID {
			continue
		}
		if a.Interface == "" {
			nid := args[i].(newIDAny)
			s.objects[nid.ID] = nid.Interface
		} else {
			s.objects[args[i].(uint32)] = a.Interface
		}
	}

	s.mu.Lock()
	s.records = append(s.records, record{iface: iface, name: m.Name, args: args})
	inject := s.injErr
	s.mu.Unlock()

	switch iface + "." + m.Name {
	case "wl_display.sync":
		cb := args[0].(uint32)
		if inject {
			s.event(displayID, ifaceWlDisplay, "error", uint32(id), uint32(7), "fake failure")
		}
		s.event(cb, ifaceWlCallback, "done", uint32(0))
		s.event(displayID, ifaceWlDisplay, "delete_id", cb)
		delete(s.objects, cb)

	case "wl_display.get_registry":
		reg := args[0].(uint32)
		for _, g := range s.globals {
			s.event(reg, ifaceWlRegistry, "global", g.Name, g.Interface, g.Version)
		}

	case "wl_registry.bind":
		nid := args[1].(newIDAny)
		s.onBind(nid)

	case "wl_shm.create_pool":
		s.pools[args[0].(uint32)] = args[1].(int)

	case "wl_shm_pool.create_buffer":
		s.buffers[args[0].(uint32)] = bufferInfo{
			pool:   id,
			offset: args[1].(int32),
			width:  args[2].(int32),
			height: args[3].(int32),
			stride: args[4].(int32),
			format: args[5].(uint32),
		}

	case "zwlr_screencopy_manager_v1.capture_output":
		frame := args[0].(uint32)
		s.event(frame, ifaceZwlrScreencopyFrameV1, "buffer",
			uint32(shmFormatXRGB8888), uint32(s.width), uint32(s.height), uint32(s.stride))
		s.event(frame, ifaceZwlrScreencopyFrameV1, "buffer_done")

	case "hyprland_toplevel_export_manager_v1.capture_toplevel":
		frame := args[0].(uint32)
		s.event(frame, ifaceHyprlandToplevelExportFrameV1, "buffer",
			uint32(shmFormatXRGB8888), uint32(s.width), uint32(s.height), uint32(s.stride))
		s.event(frame, ifaceHyprlandToplevelExportFrameV1, "buffer_done")

	case "zwlr_screencopy_frame_v1.copy", "hyprland_toplevel_export_frame_v1.copy":
		bufID := args[0].(uint32)
		info, ok := s.buffers[bufID]
		if !ok {
			s.t.Errorf("fake: copy into unknown buffer %d", bufID)
			return
		}
		fd, ok := s.pools[info.pool]
		if !ok {
			s.t.Errorf("fake: buffer %d has no pool", bufID)
			return
		}
		if _, err := syscall.Pwrite(fd, s.pixels, int64(info.offset)); err != nil {
			s.t.Errorf("fake: writing pixels: %v", err)
			return
		}
		s.event(id, iface, "flags", uint32(frameFlagYInvert))
		s.event(id, iface, "ready", uint32(0), uint32(0), uint32(0))

	case "zwlr_output_configuration_head_v1.set_custom_mode":
		s.askedW, s.askedH = args[0].(int32), args[1].(int32)

	case "zwlr_output_configuration_head_v1.set_scale":
		s.askedScale = int32(args[0].(fixed))

	case "zwlr_output_configuration_v1.apply":
		if s.refuseMode {
			s.event(id, ifaceZwlrOutputConfigurationV1, "failed")
			return
		}
		s.event(id, ifaceZwlrOutputConfigurationV1, "succeeded")
		s.event(s.outputID, ifaceWlOutput, "mode", uint32(0x1), s.askedW, s.askedH, int32(60000))
		s.event(s.outputID, ifaceWlOutput, "done")

	case "zwp_virtual_keyboard_v1.keymap":
		fd := args[1].(int)
		size := args[2].(uint32)
		if fd < 0 {
			s.t.Errorf("fake: keymap without a file descriptor")
			return
		}
		data := make([]byte, size)
		f := os.NewFile(uintptr(fd), "keymap")
		if _, err := f.ReadAt(data, 0); err != nil {
			s.t.Errorf("fake: reading the keymap: %v", err)
		}
		f.Close()
		text := strings.TrimRight(string(data), "\x00")
		if !strings.Contains(text, "xkb_keymap") {
			s.t.Errorf("fake: keymap does not look like xkb: %q", text)
		}
		s.mu.Lock()
		s.keymaps = append(s.keymaps, text)
		s.mu.Unlock()
	}
}

// onBind sends the events a freshly bound global would send.
func (s *fakeServer) onBind(nid newIDAny) {
	switch nid.Interface {
	case ifaceZwlrOutputManagerV1:
		h := s.newServerID()
		s.objects[h] = ifaceZwlrOutputHeadV1
		s.event(nid.ID, ifaceZwlrOutputManagerV1, "head", h)
		s.event(h, ifaceZwlrOutputHeadV1, "name", "HEADLESS-1")
		m := s.newServerID()
		s.objects[m] = ifaceZwlrOutputModeV1
		s.event(h, ifaceZwlrOutputHeadV1, "mode", m)
		s.event(m, ifaceZwlrOutputModeV1, "size", int32(1280), int32(720))
		s.event(m, ifaceZwlrOutputModeV1, "refresh", int32(60000))
		s.event(h, ifaceZwlrOutputHeadV1, "enabled", int32(1))
		s.event(h, ifaceZwlrOutputHeadV1, "current_mode", m)
		s.event(nid.ID, ifaceZwlrOutputManagerV1, "done", uint32(7))
	case ifaceWlOutput:
		s.outputID = nid.ID
		s.event(nid.ID, ifaceWlOutput, "mode", uint32(0x1), int32(1280), int32(800), int32(60000))
		s.event(nid.ID, ifaceWlOutput, "scale", int32(1))
		s.event(nid.ID, ifaceWlOutput, "done")
	case ifaceWlSeat:
		s.event(nid.ID, ifaceWlSeat, "capabilities", uint32(3))
		s.event(nid.ID, ifaceWlSeat, "name", "seat0")
	case ifaceWlShm:
		s.event(nid.ID, ifaceWlShm, "format", uint32(shmFormatXRGB8888))
	case ifaceZwlrForeignToplevelManagerV1:
		h := s.newServerID()
		s.objects[h] = ifaceZwlrForeignToplevelHandleV1
		s.event(nid.ID, ifaceZwlrForeignToplevelManagerV1, "toplevel", h)
		s.event(h, ifaceZwlrForeignToplevelHandleV1, "title", "Sandbox — page")
		s.event(h, ifaceZwlrForeignToplevelHandleV1, "app_id", "firefox")
		state := make([]byte, 8)
		binary.LittleEndian.PutUint32(state[0:4], 2) // activated
		binary.LittleEndian.PutUint32(state[4:8], 0) // maximized
		s.event(h, ifaceZwlrForeignToplevelHandleV1, "state", state)
		s.event(h, ifaceZwlrForeignToplevelHandleV1, "done")
	}
}

func (s *fakeServer) log() []record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]record, len(s.records))
	copy(out, s.records)
	return out
}

func (s *fakeServer) reset() {
	s.mu.Lock()
	s.records = nil
	s.mu.Unlock()
}

func (s *fakeServer) setInjectError(v bool) {
	s.mu.Lock()
	s.injErr = v
	s.mu.Unlock()
}

func (s *fakeServer) lastKeymap() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.keymaps) == 0 {
		return ""
	}
	return s.keymaps[len(s.keymaps)-1]
}

func (s *fakeServer) keymapCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.keymaps)
}

// ----------------------------------------------------------------- harness

func fullGlobals() []Global {
	return []Global{
		{Name: 1, Interface: ifaceWlShm, Version: 1},
		{Name: 2, Interface: ifaceWlSeat, Version: 7},
		{Name: 3, Interface: ifaceWlOutput, Version: 4},
		{Name: 4, Interface: ifaceZwlrVirtualPointerManagerV1, Version: 2},
		{Name: 5, Interface: ifaceZwpVirtualKeyboardManagerV1, Version: 1},
		{Name: 6, Interface: ifaceZwlrScreencopyManagerV1, Version: 3},
		{Name: 7, Interface: ifaceZwlrForeignToplevelManagerV1, Version: 3},
		{Name: 8, Interface: ifaceZwlrOutputManagerV1, Version: 4},
	}
}

func startFake(t *testing.T, globals []Global) (*Client, *fakeServer) {
	t.Helper()
	pair, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	if err := setReadTimeout(pair[0], 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := setReadTimeout(pair[1], 5*time.Second); err != nil {
		t.Fatal(err)
	}
	s := newFakeServer(t, pair[1], globals)
	// A 4x2 XRGB8888 frame (bytes B, G, R, X), announced y_invert.
	s.width, s.height, s.stride = 4, 2, 16
	s.pixels = make([]byte, s.stride*s.height)
	for x := 0; x < 4; x++ {
		put := func(row, x int, r, g, b byte) {
			o := row*s.stride + x*4
			s.pixels[o+0], s.pixels[o+1], s.pixels[o+2], s.pixels[o+3] = b, g, r, 0
		}
		put(0, x, byte(10+x*30), byte(20+x*30), byte(30+x*30))
		put(1, x, byte(1+x*3), byte(2+x*3), byte(3+x*3))
	}
	go s.serve()

	c := newClient(pair[0], "fake")
	if err := c.setup(true); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		s.closing.Store(true)
		c.Close()
		select {
		case <-s.stopped:
		case <-time.After(2 * time.Second):
		}
		syscall.Close(pair[1])
	})
	return c, s
}

func requestsNamed(recs []record, key string) []record {
	var out []record
	for _, r := range recs {
		if r.key() == key {
			out = append(out, r)
		}
	}
	return out
}

func onlyRequest(t *testing.T, recs []record, key string) record {
	t.Helper()
	got := requestsNamed(recs, key)
	if len(got) != 1 {
		t.Fatalf("want exactly one %s, got %d in %s", key, len(got), formatLog(recs))
	}
	return got[0]
}

func formatLog(recs []record) string {
	var b strings.Builder
	b.WriteString("[")
	for i, r := range recs {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s%v", r.key(), r.args)
	}
	b.WriteString("]")
	return b.String()
}

func indexOf(recs []record, key string) int {
	for i, r := range recs {
		if r.key() == key {
			return i
		}
	}
	return -1
}

// ------------------------------------------------------------------- tests

func TestConnectBindsAndReportsOutput(t *testing.T) {
	c, s := startFake(t, fullGlobals())

	if got := c.Missing(); len(got) != 0 {
		t.Errorf("Missing() = %v, want none", got)
	}
	if len(c.Globals()) != 8 {
		t.Errorf("Globals() = %d entries, want 8", len(c.Globals()))
	}
	w, h, err := c.OutputSize()
	if err != nil {
		t.Fatalf("OutputSize: %v", err)
	}
	if w != 1280 || h != 800 {
		t.Errorf("OutputSize = %dx%d, want 1280x800", w, h)
	}

	recs := s.log()
	// The output manager and the virtual devices are created on demand, not
	// at connection time (a headless cage would drop devices made before the
	// mode is set), so only the globals are bound now.
	if n := len(requestsNamed(recs, "wl_registry.bind")); n != 7 {
		t.Errorf("bound %d globals, want 7", n)
	}
	// The first input use creates the devices, the pointer without an output.
	if err := c.Move(0, 0); err != nil {
		t.Fatalf("Move: %v", err)
	}
	recs = s.log()
	onlyRequest(t, recs, "zwlr_virtual_pointer_manager_v1.create_virtual_pointer")
	onlyRequest(t, recs, "zwp_virtual_keyboard_manager_v1.create_virtual_keyboard")
	if s.keymapCount() != 1 {
		t.Errorf("first input use uploaded %d keymaps, want 1", s.keymapCount())
	}
}

func TestPointerMove(t *testing.T) {
	c, s := startFake(t, fullGlobals())
	s.reset()
	if err := c.Move(10, 20); err != nil {
		t.Fatalf("Move: %v", err)
	}
	recs := s.log()
	m := onlyRequest(t, recs, "zwlr_virtual_pointer_v1.motion_absolute")
	if m.args[1].(uint32) != 10 || m.args[2].(uint32) != 20 {
		t.Errorf("motion_absolute position = %v, %v", m.args[1], m.args[2])
	}
	if m.args[3].(uint32) != 1280 || m.args[4].(uint32) != 800 {
		t.Errorf("motion_absolute extents = %v, %v; want 1280, 800", m.args[3], m.args[4])
	}
	mi, fi := indexOf(recs, "zwlr_virtual_pointer_v1.motion_absolute"), indexOf(recs, "zwlr_virtual_pointer_v1.frame")
	if fi < 0 || fi < mi {
		t.Errorf("frame must follow motion_absolute: %s", formatLog(recs))
	}
}

func TestPointerMoveClamps(t *testing.T) {
	c, s := startFake(t, fullGlobals())
	s.reset()
	if err := c.Move(5000, -12); err != nil {
		t.Fatalf("Move: %v", err)
	}
	m := onlyRequest(t, s.log(), "zwlr_virtual_pointer_v1.motion_absolute")
	if m.args[1].(uint32) != 1279 || m.args[2].(uint32) != 0 {
		t.Errorf("clamped position = %v, %v; want 1279, 0", m.args[1], m.args[2])
	}
}

func TestPointerButton(t *testing.T) {
	c, s := startFake(t, fullGlobals())
	s.reset()
	if err := c.PressButton(ButtonLeft, true); err != nil {
		t.Fatalf("PressButton: %v", err)
	}
	b := onlyRequest(t, s.log(), "zwlr_virtual_pointer_v1.button")
	if b.args[1].(uint32) != 0x110 {
		t.Errorf("button code = %#x, want 0x110", b.args[1])
	}
	if b.args[2].(uint32) != 1 {
		t.Errorf("button state = %v, want 1 (pressed)", b.args[2])
	}
	s.reset()
	if err := c.PressButton(ButtonRight, false); err != nil {
		t.Fatalf("PressButton: %v", err)
	}
	b = onlyRequest(t, s.log(), "zwlr_virtual_pointer_v1.button")
	if b.args[1].(uint32) != 0x111 || b.args[2].(uint32) != 0 {
		t.Errorf("right release = %v, %v", b.args[1], b.args[2])
	}
}

func TestScroll(t *testing.T) {
	c, s := startFake(t, fullGlobals())
	s.reset()
	if err := c.Scroll(AxisVertical, -2); err != nil {
		t.Fatalf("Scroll: %v", err)
	}
	recs := s.log()
	if n := len(requestsNamed(recs, "zwlr_virtual_pointer_v1.axis_source")); n != 1 {
		t.Errorf("%d axis_source requests, want 1", n)
	}
	discrete := requestsNamed(recs, "zwlr_virtual_pointer_v1.axis_discrete")
	if len(discrete) != 2 {
		t.Fatalf("%d axis_discrete requests, want 2: %s", len(discrete), formatLog(recs))
	}
	for _, d := range discrete {
		if d.args[1].(uint32) != 0 {
			t.Errorf("axis = %v, want 0 (vertical)", d.args[1])
		}
		if d.args[2].(fixed) != fixedFromInt(-15) {
			t.Errorf("value = %v, want %v", d.args[2], fixedFromInt(-15))
		}
		if d.args[3].(int32) != -1 {
			t.Errorf("discrete = %v, want -1", d.args[3])
		}
	}
	if n := len(requestsNamed(recs, "zwlr_virtual_pointer_v1.frame")); n != 2 {
		t.Errorf("%d frame requests, want 2", n)
	}
}

func TestTypeText(t *testing.T) {
	c, s := startFake(t, fullGlobals())
	s.reset()
	if err := c.Type("aé\n"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	km := s.lastKeymap()
	for _, want := range []string{"xkb_keymap", "U0061", "U00E9", "Return", "modifier_map Control"} {
		if !strings.Contains(km, want) {
			t.Errorf("keymap lacks %q:\n%s", want, km)
		}
	}
	recs := s.log()
	keys := requestsNamed(recs, "zwp_virtual_keyboard_v1.key")
	if len(keys) != 6 {
		t.Fatalf("%d key requests, want 6 (3 press/release pairs): %s", len(keys), formatLog(recs))
	}
	var codes []uint32
	for i, k := range keys {
		wantState := uint32(1)
		if i%2 == 1 {
			wantState = 0
		}
		if k.args[2].(uint32) != wantState {
			t.Errorf("key %d state = %v, want %d", i, k.args[2], wantState)
		}
		if i%2 == 0 {
			codes = append(codes, k.args[1].(uint32))
		} else if k.args[1].(uint32) != codes[len(codes)-1] {
			t.Errorf("release %d is on keycode %v, press was %v", i, k.args[1], codes[len(codes)-1])
		}
	}
	if codes[0] == codes[1] || codes[1] == codes[2] || codes[0] == codes[2] {
		t.Errorf("keycodes are not distinct: %v", codes)
	}
	// evdev code = xkb keycode - 8, and generated keycodes start at 9.
	for _, code := range codes {
		if code < 1 || code > 247 {
			t.Errorf("evdev code %d out of range", code)
		}
	}
}

func TestTypeChunksLongText(t *testing.T) {
	c, s := startFake(t, fullGlobals())
	if err := c.Move(0, 0); err != nil { // create the devices and upload the initial keymap
		t.Fatal(err)
	}
	s.reset()
	before := s.keymapCount()
	var sb strings.Builder
	for r := rune(0x4e00); r < rune(0x4e00+250); r++ { // 250 distinct keysyms
		sb.WriteRune(r)
	}
	if err := c.Type(sb.String()); err != nil {
		t.Fatalf("Type: %v", err)
	}
	if want := (250 + maxKeysyms - 1) / maxKeysyms; s.keymapCount()-before != want {
		t.Errorf("%d keymaps uploaded for 250 distinct keysyms, want %d", s.keymapCount()-before, want)
	}
	if n := len(requestsNamed(s.log(), "zwp_virtual_keyboard_v1.key")); n != 500 {
		t.Errorf("%d key requests, want 500", n)
	}
}

func TestKeyCombo(t *testing.T) {
	c, s := startFake(t, fullGlobals())
	s.reset()
	if err := c.Key("ctrl+l"); err != nil {
		t.Fatalf("Key: %v", err)
	}
	recs := s.log()
	mods := requestsNamed(recs, "zwp_virtual_keyboard_v1.modifiers")
	if len(mods) != 2 {
		t.Fatalf("%d modifiers requests, want 2: %s", len(mods), formatLog(recs))
	}
	if mods[0].args[0].(uint32) != 4 {
		t.Errorf("depressed = %v, want 4 (Control)", mods[0].args[0])
	}
	if mods[0].args[1].(uint32) != 0 || mods[0].args[2].(uint32) != 0 || mods[0].args[3].(uint32) != 0 {
		t.Errorf("latched/locked/group = %v", mods[0].args[1:])
	}
	if mods[1].args[0].(uint32) != 0 {
		t.Errorf("final depressed = %v, want 0", mods[1].args[0])
	}
	mi := indexOf(recs, "zwp_virtual_keyboard_v1.modifiers")
	ki := indexOf(recs, "zwp_virtual_keyboard_v1.key")
	if mi < 0 || ki < 0 || mi > ki {
		t.Fatalf("modifiers must come before the key events: %s", formatLog(recs))
	}
	keys := requestsNamed(recs, "zwp_virtual_keyboard_v1.key")
	if len(keys) != 4 {
		t.Fatalf("%d key requests, want 4: %s", len(keys), formatLog(recs))
	}
	// Control down, key down, key up, Control up.
	wantCtrl := uint32(kcControl - 8)
	if keys[0].args[1].(uint32) != wantCtrl || keys[0].args[2].(uint32) != 1 {
		t.Errorf("first key = %v state %v, want Control (%d) pressed", keys[0].args[1], keys[0].args[2], wantCtrl)
	}
	if keys[3].args[1].(uint32) != wantCtrl || keys[3].args[2].(uint32) != 0 {
		t.Errorf("last key = %v state %v, want Control released", keys[3].args[1], keys[3].args[2])
	}
	if keys[1].args[1].(uint32) != keys[2].args[1].(uint32) {
		t.Errorf("press/release keycodes differ: %v %v", keys[1].args[1], keys[2].args[1])
	}
	if keys[1].args[1].(uint32) == wantCtrl {
		t.Errorf("the combination key must not be the modifier itself")
	}
	if !strings.Contains(s.lastKeymap(), "U006C") { // 'l'
		t.Errorf("keymap lacks U006C:\n%s", s.lastKeymap())
	}
}

func TestKeyRejectsUnknownNames(t *testing.T) {
	c, _ := startFake(t, fullGlobals())
	if err := c.Key("ctrl+nosuchkey"); err == nil {
		t.Errorf("Key accepted an unknown keysym")
	}
	if err := c.Key("hyper+a"); err == nil {
		t.Errorf("Key accepted an unknown modifier")
	}
	if err := c.Key("ctrl+"); err == nil {
		t.Errorf("Key accepted a malformed combination")
	}
	if err := c.Key("super+Page_Up"); err != nil {
		t.Errorf("Key(super+Page_Up) = %v", err)
	}
}

func TestToplevels(t *testing.T) {
	c, s := startFake(t, fullGlobals())
	tops, err := c.Toplevels()
	if err != nil {
		t.Fatalf("Toplevels: %v", err)
	}
	if len(tops) != 1 {
		t.Fatalf("%d toplevels, want 1: %+v", len(tops), tops)
	}
	got := tops[0]
	if got.Title != "Sandbox — page" || got.AppID != "firefox" {
		t.Errorf("toplevel = %+v", got)
	}
	if !got.Activated || !got.Maximized || got.Minimized || got.Fullscreen {
		t.Errorf("toplevel state = %+v", got)
	}
	if got.ID < firstServerID {
		t.Errorf("toplevel id %d is not a server allocated id", got.ID)
	}

	s.reset()
	if err := c.CloseToplevel(got.ID); err != nil {
		t.Fatalf("CloseToplevel: %v", err)
	}
	onlyRequest(t, s.log(), "zwlr_foreign_toplevel_handle_v1.close")

	if err := c.CloseToplevel(1234); err == nil {
		t.Errorf("CloseToplevel accepted an unknown id")
	}
}

func TestCapture(t *testing.T) {
	c, s := startFake(t, fullGlobals())
	s.reset()
	img, err := c.Capture(true)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if img.Bounds().Dx() != 4 || img.Bounds().Dy() != 2 {
		t.Fatalf("image is %v, want 4x2", img.Bounds())
	}
	co := onlyRequest(t, s.log(), "zwlr_screencopy_manager_v1.capture_output")
	if co.args[1].(int32) != 1 {
		t.Errorf("overlay_cursor = %v, want 1", co.args[1])
	}
	// y_invert: the last source row comes out first.
	for x := 0; x < 4; x++ {
		r, g, b, a := img.At(x, 0).RGBA()
		wantR, wantG, wantB := uint32(1+x*3), uint32(2+x*3), uint32(3+x*3)
		if r>>8 != wantR || g>>8 != wantG || b>>8 != wantB || a>>8 != 0xff {
			t.Errorf("pixel (%d,0) = %d,%d,%d,%d; want %d,%d,%d,255",
				x, r>>8, g>>8, b>>8, a>>8, wantR, wantG, wantB)
		}
		r, g, b, a = img.At(x, 1).RGBA()
		wantR, wantG, wantB = uint32(10+x*30), uint32(20+x*30), uint32(30+x*30)
		if r>>8 != wantR || g>>8 != wantG || b>>8 != wantB || a>>8 != 0xff {
			t.Errorf("pixel (%d,1) = %d,%d,%d,%d; want %d,%d,%d,255",
				x, r>>8, g>>8, b>>8, a>>8, wantR, wantG, wantB)
		}
	}
	for _, want := range []string{"wl_shm.create_pool", "wl_shm_pool.create_buffer", "zwlr_screencopy_frame_v1.copy"} {
		onlyRequest(t, s.log(), want)
	}
}

func TestCaptureToplevelRequest(t *testing.T) {
	c, s := startFake(t, append(fullGlobals(), Global{Name: 9, Interface: ifaceHyprlandToplevelExportManagerV1, Version: 2}))
	s.reset()
	img, err := c.CaptureToplevel(0xac116320, true)
	if err != nil {
		t.Fatalf("CaptureToplevel: %v", err)
	}
	if img.Bounds().Dx() != 4 || img.Bounds().Dy() != 2 {
		t.Fatalf("image is %v, want 4x2", img.Bounds())
	}
	ct := onlyRequest(t, s.log(), "hyprland_toplevel_export_manager_v1.capture_toplevel")
	if ct.args[1].(int32) != 1 || ct.args[2].(uint32) != 0xac116320 {
		t.Errorf("capture_toplevel args = %v, want overlay_cursor 1 and handle 0xac116320", ct.args)
	}
	// ignore_damage: a window on a hidden workspace never sends damage.
	if cp := onlyRequest(t, s.log(), "hyprland_toplevel_export_frame_v1.copy"); cp.args[1].(int32) != 1 {
		t.Errorf("copy ignore_damage = %v, want 1", cp.args[1])
	}
	if err := c.Roundtrip(); err != nil {
		t.Fatal(err)
	}
	onlyRequest(t, s.log(), "hyprland_toplevel_export_frame_v1.destroy")
}

func TestCaptureToplevelMissing(t *testing.T) {
	c, _ := startFake(t, fullGlobals())
	if _, err := c.CaptureToplevel(1, false); !errors.Is(err, ErrMissingProtocol) {
		t.Fatalf("CaptureToplevel error = %v, want ErrMissingProtocol", err)
	}
}

func TestProtocolErrorSurfaces(t *testing.T) {
	c, s := startFake(t, fullGlobals())
	s.setInjectError(true)
	err := c.Move(1, 1)
	if err == nil {
		t.Fatalf("Move succeeded despite wl_display.error")
	}
	want := "protocol error on wl_display#1: code 7: fake failure"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err, want)
	}
	// The client stays unusable.
	if err2 := c.Move(2, 2); err2 == nil || !strings.Contains(err2.Error(), "protocol error") {
		t.Errorf("second call = %v, want the latched protocol error", err2)
	}
}

func TestMissingScreencopy(t *testing.T) {
	globals := []Global{
		{Name: 1, Interface: ifaceWlShm, Version: 1},
		{Name: 2, Interface: ifaceWlSeat, Version: 7},
		{Name: 3, Interface: ifaceWlOutput, Version: 4},
		{Name: 4, Interface: ifaceZwlrVirtualPointerManagerV1, Version: 1}, // v1: no with_output
		{Name: 5, Interface: ifaceZwpVirtualKeyboardManagerV1, Version: 1},
		{Name: 7, Interface: ifaceZwlrForeignToplevelManagerV1, Version: 3},
	}
	c, s := startFake(t, globals)

	missing := c.Missing()
	if len(missing) != 1 || missing[0] != ifaceZwlrScreencopyManagerV1 {
		t.Fatalf("Missing() = %v, want [%s]", missing, ifaceZwlrScreencopyManagerV1)
	}
	_, err := c.Capture(false)
	if !errors.Is(err, ErrMissingProtocol) {
		t.Fatalf("Capture error = %v, want ErrMissingProtocol", err)
	}
	if !strings.Contains(err.Error(), ifaceZwlrScreencopyManagerV1) {
		t.Errorf("error %q does not name the interface", err)
	}
	// Input still works, and creates the pointer with the plain constructor.
	if err := c.Move(3, 4); err != nil {
		t.Errorf("Move: %v", err)
	}
	onlyRequest(t, s.log(), "zwlr_virtual_pointer_manager_v1.create_virtual_pointer")
	if n := len(requestsNamed(s.log(), "zwlr_virtual_pointer_manager_v1.create_virtual_pointer_with_output")); n != 0 {
		t.Errorf("a version 1 manager must not get create_virtual_pointer_with_output")
	}
}

func TestMissingInputProtocols(t *testing.T) {
	globals := []Global{
		{Name: 1, Interface: ifaceWlShm, Version: 1},
		{Name: 2, Interface: ifaceWlSeat, Version: 7},
		{Name: 3, Interface: ifaceWlOutput, Version: 4},
	}
	c, _ := startFake(t, globals)
	if len(c.Missing()) != 4 {
		t.Fatalf("Missing() = %v, want the four required globals", c.Missing())
	}
	for name, err := range map[string]error{
		"Move":          c.Move(1, 1),
		"PressButton":   c.PressButton(ButtonLeft, true),
		"Scroll":        c.Scroll(AxisVertical, 1),
		"Type":          c.Type("x"),
		"Key":           c.Key("a"),
		"CloseToplevel": c.CloseToplevel(1),
	} {
		if !errors.Is(err, ErrMissingProtocol) {
			t.Errorf("%s error = %v, want ErrMissingProtocol", name, err)
		}
	}
	if _, err := c.Toplevels(); !errors.Is(err, ErrMissingProtocol) {
		t.Errorf("Toplevels error = %v, want ErrMissingProtocol", err)
	}
}

// TestProbeLive is the only test that talks to the user's own compositor, and
// only when asked: it binds nothing and merely lists the globals.
func TestProbeLive(t *testing.T) {
	if os.Getenv("HYPRCAGE_WL_PROBE") != "1" {
		t.Skip("set HYPRCAGE_WL_PROBE=1 to probe the running compositor")
	}
	globals, err := Probe(os.Getenv("WAYLAND_DISPLAY"))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if len(globals) < 10 {
		t.Errorf("%d globals, want at least 10", len(globals))
	}
	found := false
	for _, g := range globals {
		t.Logf("global %d: %s v%d", g.Name, g.Interface, g.Version)
		if g.Interface == ifaceWlCompositor {
			found = true
		}
	}
	if !found {
		t.Errorf("wl_compositor is not advertised")
	}
}

func TestSetMode(t *testing.T) {
	c, s := startFake(t, fullGlobals())
	if err := c.SetMode(1920, 1200, 60, 2); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if err := c.Roundtrip(); err != nil { // the fake logs the destroy on its own goroutine
		t.Fatal(err)
	}
	recs := s.log()
	cfg := onlyRequest(t, recs, "zwlr_output_manager_v1.create_configuration")
	if got := cfg.args[1].(uint32); got != 7 {
		t.Errorf("configuration created with serial %d, want the manager's 7", got)
	}
	mode := onlyRequest(t, recs, "zwlr_output_configuration_head_v1.set_custom_mode")
	if mode.args[0].(int32) != 1920 || mode.args[1].(int32) != 1200 || mode.args[2].(int32) != 60000 {
		t.Errorf("set_custom_mode args %v", mode.args)
	}
	if sc := onlyRequest(t, recs, "zwlr_output_configuration_head_v1.set_scale"); int32(sc.args[0].(fixed)) != 512 {
		t.Errorf("set_scale = %v, want 2.0 as fixed 512", sc.args[0])
	}
	if i := indexOf(recs, "zwlr_output_configuration_v1.apply"); i < 0 || i < indexOf(recs, "zwlr_output_configuration_head_v1.set_scale") {
		t.Error("apply must come after the head is configured")
	}
	if indexOf(recs, "zwlr_output_configuration_v1.destroy") < 0 {
		t.Error("the configuration must be destroyed once answered")
	}
	// The wl_output scale event did not change in the fake (still 1), so the
	// logical size equals the mode.
	if w, h, err := c.OutputSize(); err != nil || w != 1920 || h != 1200 {
		t.Errorf("OutputSize after SetMode = %dx%d, %v", w, h, err)
	}
}

func TestSetModeRefused(t *testing.T) {
	c, s := startFake(t, fullGlobals())
	s.refuseMode = true
	if err := c.SetMode(4096, 4096, 60, 1); err == nil {
		t.Fatal("a refused configuration must be an error")
	}
	if w, h, _ := c.OutputSize(); w != 1280 || h != 800 {
		t.Errorf("OutputSize after a refusal = %dx%d, want the old 1280x800", w, h)
	}
}

func TestSetModeWithoutManager(t *testing.T) {
	c, _ := startFake(t, fullGlobals()[:7])
	if err := c.SetMode(1280, 800, 60, 1); !errors.Is(err, ErrMissingProtocol) {
		t.Errorf("want ErrMissingProtocol, got %v", err)
	}
}

func TestFit(t *testing.T) {
	cases := []struct{ bw, bh, ww, wh, fw, fh int }{
		{1280, 800, 1920, 1080, 1728, 1080}, // 16:10 screen on a 16:9 monitor: bars left and right
		{1280, 800, 1280, 800, 1280, 800},   // same shape: fills
		{1600, 600, 1280, 800, 1280, 480},   // wide screen in a narrower window: bars top and bottom
		{1280, 800, 950, 530, 848, 530},     // a tile
		{1280, 800, 0, 0, 0, 0},             // nothing to fit into
	}
	for _, c := range cases {
		if fw, fh := fit(c.bw, c.bh, c.ww, c.wh); fw != c.fw || fh != c.fh {
			t.Errorf("fit(%dx%d in %dx%d) = %dx%d, want %dx%d", c.bw, c.bh, c.ww, c.wh, fw, fh, c.fw, c.fh)
		}
	}
}

// Chromium and Electron read the DOM keyCode of anything but an ASCII letter
// or digit from the physical key: a keysym on the Escape, BackSpace, Tab or
// Enter key is acted on, not inserted. Every keysym must land on a printable
// key, its own US key when it has one.
func TestKeymapUsesPrintableKeys(t *testing.T) {
	control := map[int]bool{1: true, 14: true, 15: true, 28: true, 29: true, 42: true, 54: true, 56: true, 58: true,
		69: true, 70: true, 87: true, 88: true, 96: true, 97: true, 100: true, 102: true, 103: true, 104: true,
		105: true, 106: true, 107: true, 108: true, 109: true, 110: true, 111: true, 125: true, 126: true, 127: true}
	for code := 59; code <= 68; code++ {
		control[code] = true // F1..F10
	}
	km := newKeymap()
	for _, sym := range []string{"U002F", "U0061", "U003F", "U00E9", "U4E00", "U0020", "U0031"} {
		if _, ok := km.add(sym); !ok {
			t.Fatalf("add(%s) refused", sym)
		}
	}
	want := map[string]int{"U002F": 53, "U0061": 30, "U0020": 57, "U0031": 2} // / a space 1 on their US keys
	for sym, code := range want {
		if got := km.keycode(sym) - 8; got != code {
			t.Errorf("%s on evdev %d, want %d", sym, got, code)
		}
	}
	if got := km.keycode("U003F") - 8; got == 53 || control[got] {
		t.Errorf("? on evdev %d: its US key is taken by /, it must move to a free printable key", got)
	}
	for _, sym := range []string{"U00E9", "U4E00"} {
		if got := km.keycode(sym) - 8; control[got] {
			t.Errorf("%s landed on control key %d", sym, got)
		}
	}
	// The whole pool, then a refusal: never a control key, never twice the same.
	km = newKeymap()
	seen := map[int]bool{}
	for r := rune(0x4e00); ; r++ {
		if _, ok := km.add(keysymForRune(r)); !ok {
			break
		}
		code := km.keycode(keysymForRune(r)) - 8
		if control[code] || seen[code] {
			t.Errorf("U%04X on evdev %d (control %v, seen %v)", r, code, control[code], seen[code])
		}
		seen[code] = true
	}
	if len(seen) != maxKeysyms {
		t.Errorf("pool holds %d keys, want %d", len(seen), maxKeysyms)
	}
	// A named key of Key goes on its own physical key.
	km = newKeymap()
	km.add("Return")
	km.add("slash")
	if km.keycode("Return")-8 != 28 || km.keycode("slash")-8 != 53 {
		t.Errorf("Return on %d, slash on %d, want 28 and 53", km.keycode("Return")-8, km.keycode("slash")-8)
	}
}
