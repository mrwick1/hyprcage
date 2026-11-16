package wl

import (
	"fmt"
	"os"
)

// ShmPool is a memory file both compositors can map: cage's screencopy
// writes a frame into it and the human's compositor reads the very same
// bytes as a wl_buffer. Nothing is copied by offstage in between. Each
// Client that uses the pool creates its own wl_shm_pool object over the
// same file descriptor with ImportPool.
type ShmPool struct {
	file *os.File
	size int
	mem  []byte // mapped on demand, for the overlay drawing
}

// NewShmPool creates a sealed memory file of size bytes.
func NewShmPool(size int) (*ShmPool, error) {
	f, err := runtimeTempFile("offstage-pool-*")
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(int64(size)); err != nil {
		f.Close()
		return nil, fmt.Errorf("wl: sizing the shared pool: %w", err)
	}
	return &ShmPool{file: f, size: size}, nil
}

// Size is the pool's size in bytes.
func (p *ShmPool) Size() int { return p.size }

// Fd is the pool's file descriptor.
func (p *ShmPool) Fd() int { return int(p.file.Fd()) }

// Bytes maps the pool into memory, once, and returns it.
func (p *ShmPool) Bytes() ([]byte, error) {
	if p.mem != nil {
		return p.mem, nil
	}
	mem, err := mmapShared(p.Fd(), p.size)
	if err != nil {
		return nil, fmt.Errorf("wl: mapping the shared pool: %w", err)
	}
	p.mem = mem
	return mem, nil
}

// Close unmaps and closes the pool. Buffers created from it must have been
// destroyed first.
func (p *ShmPool) Close() error {
	if p.mem != nil {
		_ = munmap(p.mem)
		p.mem = nil
	}
	return p.file.Close()
}

// ImportPool creates this client's wl_shm_pool over the shared file and
// returns its object id.
func (c *Client) ImportPool(p *ShmPool) (uint32, error) {
	if c.shm == 0 {
		return 0, missingErr(ifaceWlShm)
	}
	id := c.allocID()
	c.register(id, ifaceWlShmPool, 1, nil)
	c.send(c.shm, reqWlShmCreatePool, id, p.Fd(), int32(p.size))
	return id, c.flush()
}

// CreateBuffer carves a wl_buffer out of an imported pool. release, when
// not nil, is called when the compositor is done reading the buffer.
func (c *Client) CreateBuffer(pool uint32, offset, width, height, stride int, format uint32, release func()) uint32 {
	id := c.allocID()
	c.register(id, ifaceWlBuffer, 1, func(op int, args []any) error {
		if op == evtWlBufferRelease && release != nil {
			release()
		}
		return nil
	})
	c.send(pool, reqWlShmPoolCreateBuffer, id, int32(offset), int32(width), int32(height), int32(stride), format)
	return id
}

// DestroyBuffer destroys a wl_buffer.
func (c *Client) DestroyBuffer(id uint32) { c.destroyObject(id, reqWlBufferDestroy) }

// DestroyPool destroys a wl_shm_pool object (the memory lives on while
// buffers reference it).
func (c *Client) DestroyPool(id uint32) { c.destroyObject(id, reqWlShmPoolDestroy) }
