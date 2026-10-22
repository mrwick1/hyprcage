package wl

import (
	"errors"
	"image"
)

// Asynchronous screencopy for the mirror: the compositor describes the
// buffer it wants, the caller hands it one, and the copy completes on its
// own time. With damage requested, the copy waits until the output changed,
// so a mirror of a still screen costs nothing. Everything here is driven
// by DispatchPending from a poll loop; nothing blocks.

// Frame describes one screencopy frame as the compositor announced it.
type Frame struct {
	Format, Width, Height, Stride uint32
	Flags                         uint32          // frameFlagYInvert and friends
	Damage                        image.Rectangle // union of the damage events
	id                            uint32
}

// YInverted reports whether the rows of the frame are stored bottom first.
func (f *Frame) YInverted() bool { return f.Flags&frameFlagYInvert != 0 }

// FrameOptions selects what is captured and when.
type FrameOptions struct {
	WithDamage bool // wait for the output to change before copying
	Cursor     bool // paint the compositor's cursor into the frame
}

// StartFrame begins a capture. announce runs once the compositor has
// described the buffer and must return the wl_buffer to copy into, or 0 to
// abort. done runs on completion, with err set when the compositor gave
// up. Both run from DispatchPending.
func (c *Client) StartFrame(opts FrameOptions, announce func(f *Frame) uint32, done func(f *Frame, err error)) error {
	if c.scMgr == 0 {
		return missingErr(ifaceZwlrScreencopyManagerV1)
	}
	if opts.WithDamage && c.scMgrVer < 2 {
		return errors.New("wl: copy_with_damage needs screencopy version 2")
	}
	f := &Frame{id: c.allocID()}
	announced := false
	c.register(f.id, ifaceZwlrScreencopyFrameV1, c.scMgrVer, func(op int, args []any) error {
		switch op {
		case evtZwlrScreencopyFrameV1Buffer:
			f.Format = args[0].(uint32)
			f.Width = args[1].(uint32)
			f.Height = args[2].(uint32)
			f.Stride = args[3].(uint32)
			if c.scMgrVer < 3 {
				c.copyFrame(f, opts, announce, done, &announced)
			}
		case evtZwlrScreencopyFrameV1BufferDone:
			c.copyFrame(f, opts, announce, done, &announced)
		case evtZwlrScreencopyFrameV1Flags:
			f.Flags = args[0].(uint32)
		case evtZwlrScreencopyFrameV1Damage:
			x, y := int(args[0].(uint32)), int(args[1].(uint32))
			r := image.Rect(x, y, x+int(args[2].(uint32)), y+int(args[3].(uint32)))
			f.Damage = f.Damage.Union(r)
		case evtZwlrScreencopyFrameV1Ready:
			c.destroyObject(f.id, reqZwlrScreencopyFrameV1Destroy)
			done(f, nil)
		case evtZwlrScreencopyFrameV1Failed:
			c.destroyObject(f.id, reqZwlrScreencopyFrameV1Destroy)
			done(f, errors.New("wl: the compositor failed to copy the frame"))
		}
		return nil
	})
	var cursor int32
	if opts.Cursor {
		cursor = 1
	}
	c.send(c.scMgr, reqZwlrScreencopyManagerV1CaptureOutput, f.id, cursor, c.output)
	return c.flush()
}

func (c *Client) copyFrame(f *Frame, opts FrameOptions, announce func(*Frame) uint32, done func(*Frame, error), announced *bool) {
	if *announced {
		return
	}
	*announced = true
	buf := announce(f)
	if buf == 0 {
		c.destroyObject(f.id, reqZwlrScreencopyFrameV1Destroy)
		done(f, errors.New("wl: no buffer for the frame"))
		return
	}
	if opts.WithDamage {
		c.send(f.id, reqZwlrScreencopyFrameV1CopyWithDamage, buf)
	} else {
		c.send(f.id, reqZwlrScreencopyFrameV1Copy, buf)
	}
}

// Fd is the connection's socket, for poll.
func (c *Client) Fd() int { return c.fd }

// Flush writes the queued requests.
func (c *Client) Flush() error { return c.flush() }

// DispatchPending reads what the socket holds (one receive, so call it
// after poll reported the socket readable) and dispatches every complete
// event. A compositor that hung up is an error.
func (c *Client) DispatchPending() error {
	if c.closed {
		return ErrClosed
	}
	if err := c.fill(); err != nil {
		return err
	}
	for {
		id, op, body, ok := c.takeMessage()
		if c.wireErr != nil {
			return c.wireErr
		}
		if !ok {
			return c.flush()
		}
		if err := c.dispatch(id, op, body); err != nil {
			return err
		}
		if c.protoErr != nil {
			return c.protoErr
		}
	}
}
