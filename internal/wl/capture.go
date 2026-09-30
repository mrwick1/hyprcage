package wl

import (
	"errors"
	"fmt"
	"image"
)

// wl_shm pixel formats, plus the two fourcc variants a wlroots compositor may
// announce for a frame.
const (
	shmFormatARGB8888 = 0          // bytes B, G, R, A
	shmFormatXRGB8888 = 1          // bytes B, G, R, X
	fourccABGR8888    = 0x34324241 // 'AB24': bytes R, G, B, A
	fourccXBGR8888    = 0x34324258 // 'XB24': bytes R, G, B, X
)

// frameFlagYInvert is the y_invert flag of both frame interfaces.
const frameFlagYInvert = 1

// frameProto names the opcodes of a frame interface. Screencopy and toplevel
// export frames share the flow, not the opcodes.
type frameProto struct {
	iface   string
	version int
	// waitDone makes the client wait for buffer_done after the buffer event.
	waitDone                                           bool
	evBuffer, evBufferDone, evFlags, evReady, evFailed int
	reqCopy, reqDestroy                                int
	copyArgs                                           []any // after the buffer
}

// frameState collects the events of one frame.
type frameState struct {
	p                             *frameProto
	format, width, height, stride uint32
	flags                         uint32
	gotBuffer                     bool
	bufferDone                    bool
	ready                         bool
	failed                        bool
}

// OpaqueFormat is the alpha-less twin of a wl_shm format, the format itself
// when it has none: same bytes, alpha ignored by the compositor.
func OpaqueFormat(format uint32) uint32 {
	switch format {
	case shmFormatARGB8888:
		return shmFormatXRGB8888
	case fourccABGR8888:
		return fourccXBGR8888
	}
	return format
}

func (s *frameState) handle(op int, args []any) error {
	switch op {
	case s.p.evBuffer:
		s.format = args[0].(uint32)
		s.width = args[1].(uint32)
		s.height = args[2].(uint32)
		s.stride = args[3].(uint32)
		s.gotBuffer = true
	case s.p.evBufferDone:
		s.bufferDone = true
	case s.p.evFlags:
		s.flags = args[0].(uint32)
	case s.p.evReady:
		s.ready = true
	case s.p.evFailed:
		s.failed = true
	}
	return nil
}

// capture implements Client.Capture.
func (c *Client) capture(overlayCursor bool) (*image.RGBA, error) {
	if c.scMgr == 0 {
		return nil, missingErr(ifaceZwlrScreencopyManagerV1)
	}
	if c.output == 0 {
		return nil, missingErr(ifaceWlOutput)
	}
	p := &frameProto{
		iface: ifaceZwlrScreencopyFrameV1, version: c.scMgrVer, waitDone: c.scMgrVer >= 3,
		evBuffer: evtZwlrScreencopyFrameV1Buffer, evBufferDone: evtZwlrScreencopyFrameV1BufferDone,
		evFlags: evtZwlrScreencopyFrameV1Flags, evReady: evtZwlrScreencopyFrameV1Ready, evFailed: evtZwlrScreencopyFrameV1Failed,
		reqCopy: reqZwlrScreencopyFrameV1Copy, reqDestroy: reqZwlrScreencopyFrameV1Destroy,
	}
	return c.captureFrame(p, func(frame uint32) {
		c.send(c.scMgr, reqZwlrScreencopyManagerV1CaptureOutput, frame, cursorArg(overlayCursor), c.output)
	})
}

// captureToplevel implements Client.CaptureToplevel.
func (c *Client) captureToplevel(handle uint32, overlayCursor bool) (*image.RGBA, error) {
	if c.teMgr == 0 {
		return nil, missingErr(ifaceHyprlandToplevelExportManagerV1)
	}
	p := &frameProto{
		iface: ifaceHyprlandToplevelExportFrameV1, version: c.teMgrVer, waitDone: true,
		evBuffer: evtHyprlandToplevelExportFrameV1Buffer, evBufferDone: evtHyprlandToplevelExportFrameV1BufferDone,
		evFlags: evtHyprlandToplevelExportFrameV1Flags, evReady: evtHyprlandToplevelExportFrameV1Ready, evFailed: evtHyprlandToplevelExportFrameV1Failed,
		reqCopy: reqHyprlandToplevelExportFrameV1Copy, reqDestroy: reqHyprlandToplevelExportFrameV1Destroy,
		// ignore_damage: a window on a hidden workspace sends no damage.
		copyArgs: []any{int32(1)},
	}
	return c.captureFrame(p, func(frame uint32) {
		c.send(c.teMgr, reqHyprlandToplevelExportManagerV1CaptureToplevel, frame, cursorArg(overlayCursor), handle)
	})
}

func cursorArg(overlay bool) int32 {
	if overlay {
		return 1
	}
	return 0
}

// captureFrame creates a frame with start, copies it into a shared memory
// buffer and decodes the pixels.
func (c *Client) captureFrame(p *frameProto, start func(frame uint32)) (*image.RGBA, error) {
	if c.shm == 0 {
		return nil, missingErr(ifaceWlShm)
	}

	st := &frameState{p: p}
	frame := c.allocID()
	c.register(frame, p.iface, p.version, st.handle)
	start(frame)

	// Wait for the buffer announcement (and, when the frame has buffer_done,
	// for the end of the announcements: the linux_dmabuf one is ignored).
	if err := c.waitFor(func() bool {
		if st.failed {
			return true
		}
		if !st.gotBuffer {
			return false
		}
		return !p.waitDone || st.bufferDone
	}); err != nil {
		c.destroyObject(frame, p.reqDestroy)
		return nil, err
	}
	if st.failed {
		c.destroyObject(frame, p.reqDestroy)
		return nil, fmt.Errorf("wl: the compositor refused the %s", p.iface)
	}
	if st.width == 0 || st.height == 0 || st.stride < st.width*4 {
		c.destroyObject(frame, p.reqDestroy)
		return nil, fmt.Errorf("wl: nonsensical %s buffer %dx%d stride %d", p.iface, st.width, st.height, st.stride)
	}

	size := int(st.stride) * int(st.height)
	f, err := runtimeTempFile("hyprcage-shm-*")
	if err != nil {
		c.destroyObject(frame, p.reqDestroy)
		return nil, err
	}
	defer f.Close()
	if err := f.Truncate(int64(size)); err != nil {
		c.destroyObject(frame, p.reqDestroy)
		return nil, fmt.Errorf("wl: sizing the shared buffer: %w", err)
	}

	pool := c.allocID()
	c.register(pool, ifaceWlShmPool, 1, nil)
	c.send(c.shm, reqWlShmCreatePool, pool, int(f.Fd()), int32(size))

	buffer := c.allocID()
	c.register(buffer, ifaceWlBuffer, 1, nil)
	c.send(pool, reqWlShmPoolCreateBuffer, buffer, int32(0), int32(st.width), int32(st.height), int32(st.stride), st.format)

	c.send(frame, p.reqCopy, append([]any{buffer}, p.copyArgs...)...)

	err = c.waitFor(func() bool { return st.ready || st.failed })
	c.destroyObject(buffer, reqWlBufferDestroy)
	c.destroyObject(pool, reqWlShmPoolDestroy)
	c.destroyObject(frame, p.reqDestroy)
	if err != nil {
		return nil, err
	}
	if st.failed {
		return nil, errors.New("wl: the compositor failed to copy the frame")
	}

	data := make([]byte, size)
	if _, err := f.ReadAt(data, 0); err != nil {
		return nil, fmt.Errorf("wl: reading the captured pixels: %w", err)
	}
	img, err := decodePixels(data, int(st.width), int(st.height), int(st.stride), st.format, st.flags)
	if err != nil {
		return nil, err
	}
	if err := c.flush(); err != nil {
		return nil, err
	}
	return img, nil
}

// decodePixels converts a wl_shm buffer into an RGBA image, flipping the rows
// when the frame carries the y_invert flag.
func decodePixels(data []byte, w, h, stride int, format, flags uint32) (*image.RGBA, error) {
	switch format {
	case shmFormatARGB8888, shmFormatXRGB8888, fourccABGR8888, fourccXBGR8888:
	default:
		return nil, fmt.Errorf("wl: unsupported pixel format 0x%08x", format)
	}
	if len(data) < (h-1)*stride+w*4 {
		return nil, fmt.Errorf("wl: short pixel buffer: %d bytes for %dx%d stride %d", len(data), w, h, stride)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		src := y
		if flags&frameFlagYInvert != 0 {
			src = h - 1 - y
		}
		row := data[src*stride:]
		out := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			p := row[x*4 : x*4+4]
			o := out[x*4 : x*4+4]
			switch format {
			case shmFormatARGB8888:
				o[0], o[1], o[2], o[3] = p[2], p[1], p[0], p[3]
			case shmFormatXRGB8888:
				o[0], o[1], o[2], o[3] = p[2], p[1], p[0], 0xff
			case fourccABGR8888:
				o[0], o[1], o[2], o[3] = p[0], p[1], p[2], p[3]
			case fourccXBGR8888:
				o[0], o[1], o[2], o[3] = p[0], p[1], p[2], 0xff
			}
		}
	}
	return img, nil
}
