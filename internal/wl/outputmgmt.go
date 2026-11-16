package wl

import (
	"errors"
	"fmt"
)

// wlr-output-management client: the way offstage gives cage's headless
// output the size the screen was asked for. wlroots creates that output at
// 1280x720 and cage takes no size option, but cage implements the manager
// side of this protocol and honours a custom mode set through it (verified
// on cage 0.3 with wlr-randr --custom-mode).

// omHead is one zwlr_output_head_v1 with the modes it announced.
type omHead struct {
	id      uint32
	name    string
	enabled bool
	modes   []uint32
}

// omState is what the manager announced, refreshed by every done event.
type omState struct {
	mgr    uint32
	ver    int
	serial uint32
	done   bool
	heads  map[uint32]*omHead
	order  []uint32 // heads in announcement order
}

func (c *Client) bindOutputManager() error {
	if c.om != nil {
		return nil
	}
	st := &omState{heads: map[uint32]*omHead{}}
	st.mgr, st.ver = c.bind(ifaceZwlrOutputManagerV1, func(op int, args []any) error {
		switch op {
		case evtZwlrOutputManagerV1Head:
			id := args[0].(uint32)
			h := &omHead{id: id}
			st.heads[id] = h
			st.order = append(st.order, id)
			c.register(id, ifaceZwlrOutputHeadV1, st.ver, func(op int, args []any) error {
				switch op {
				case evtZwlrOutputHeadV1Name:
					h.name = args[0].(string)
				case evtZwlrOutputHeadV1Enabled:
					h.enabled = args[0].(int32) != 0
				case evtZwlrOutputHeadV1Mode:
					mid := args[0].(uint32)
					h.modes = append(h.modes, mid)
					c.register(mid, ifaceZwlrOutputModeV1, st.ver, nil)
				case evtZwlrOutputHeadV1Finished:
					delete(st.heads, id)
				}
				return nil
			})
		case evtZwlrOutputManagerV1Done:
			st.serial = args[0].(uint32)
			st.done = true
		case evtZwlrOutputManagerV1Finished:
			st.mgr = 0
		}
		return nil
	})
	if st.mgr == 0 {
		return missingErr(ifaceZwlrOutputManagerV1)
	}
	c.om = st
	return c.waitFor(func() bool { return st.done })
}

// SetMode asks the compositor to run its output at width x height pixels,
// hz Hz and the given scale, and waits until the output announces the new
// mode. The output is the compositor's first head: cage has exactly one.
func (c *Client) SetMode(width, height, hz int, scale float64) error {
	if width <= 0 || height <= 0 || hz <= 0 || scale <= 0 {
		return fmt.Errorf("wl: invalid mode %dx%d@%d scale %v", width, height, hz, scale)
	}
	if err := c.bindOutputManager(); err != nil {
		return err
	}
	st := c.om
	if len(st.order) == 0 {
		return errors.New("wl: the compositor announced no output head")
	}
	head := st.order[0]

	// One configuration, one head enabled with a custom mode, applied.
	result := 0 // 1 succeeded, 2 failed, 3 cancelled
	cfg := c.allocID()
	c.register(cfg, ifaceZwlrOutputConfigurationV1, st.ver, func(op int, args []any) error {
		switch op {
		case evtZwlrOutputConfigurationV1Succeeded:
			result = 1
		case evtZwlrOutputConfigurationV1Failed:
			result = 2
		case evtZwlrOutputConfigurationV1Cancelled:
			result = 3
		}
		return nil
	})
	c.send(st.mgr, reqZwlrOutputManagerV1CreateConfiguration, cfg, st.serial)
	ch := c.allocID()
	c.register(ch, ifaceZwlrOutputConfigurationHeadV1, st.ver, nil)
	c.send(cfg, reqZwlrOutputConfigurationV1EnableHead, ch, head)
	c.send(ch, reqZwlrOutputConfigurationHeadV1SetCustomMode, int32(width), int32(height), int32(hz*1000))
	c.send(ch, reqZwlrOutputConfigurationHeadV1SetScale, fixed(scale*256))
	c.send(cfg, reqZwlrOutputConfigurationV1Apply)
	err := c.waitFor(func() bool { return result != 0 })
	c.destroyObject(cfg, reqZwlrOutputConfigurationV1Destroy)
	if err != nil {
		return err
	}
	switch result {
	case 2:
		return fmt.Errorf("wl: the compositor refused the mode %dx%d@%d scale %v", width, height, hz, scale)
	case 3:
		// The manager state moved under the configuration: read it again
		// and let the caller retry.
		return errors.New("wl: the output configuration was cancelled by a concurrent change")
	}
	// The mode is applied when wl_output says so, which arrives on its own.
	return c.waitFor(func() bool { return c.modeW == width && c.modeH == height })
}
