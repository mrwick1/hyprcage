package wl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// argType is the type of one argument in a protocol message signature.
type argType uint8

const (
	argInt argType = iota
	argUint
	argFixed
	argString
	argObject
	argNewID
	argArray
	argFD
)

// argSpec is one argument of a request or event, as declared in the XML.
type argSpec struct {
	Name      string
	Type      argType
	Interface string // empty for new_id without interface (wl_registry.bind)
	Nullable  bool
}

// msgSpec is one request or event: its opcode (index in the XML order) and
// its argument signature.
type msgSpec struct {
	Name       string
	Opcode     int
	Since      int
	Destructor bool
	Args       []argSpec
}

// ifaceSpec is one protocol interface.
type ifaceSpec struct {
	Name     string
	Version  int
	Requests []msgSpec
	Events   []msgSpec
}

// fixed is a 24.8 signed fixed point number as carried on the wire.
type fixed int32

func fixedFromInt(v int) fixed { return fixed(v) << 8 }

func (f fixed) float() float64 { return float64(f) / 256 }

// newIDAny is the value of a new_id argument whose interface is not known
// from the XML: wl_registry.bind carries interface name, version and id.
type newIDAny struct {
	Interface string
	Version   uint32
	ID        uint32
}

// object is one live protocol object on this connection.
type object struct {
	id      uint32
	iface   string
	version int
	handler func(op int, args []any) error
}

// firstServerID is the start of the id range the server allocates.
const firstServerID = 0xff000000

// displayID is the well known id of wl_display.
const displayID = 1

// readTimeout bounds every blocking read on the socket so a silent
// compositor cannot wedge the caller forever.
var readTimeout = 10 * time.Second

// ErrClosed is returned once the client has been closed.
var ErrClosed = errors.New("wl: client is closed")

// ---------------------------------------------------------------- encoding

// encodeMessage builds the wire bytes of one message (header included) and
// returns the file descriptors that must travel with it.
func encodeMessage(objID uint32, m *msgSpec, args []any) ([]byte, []int, error) {
	if len(args) != countWireArgs(m) {
		return nil, nil, fmt.Errorf("wl: %s: got %d arguments, want %d", m.Name, len(args), countWireArgs(m))
	}
	buf := make([]byte, 8, 64)
	var fds []int
	for i, a := range m.Args {
		v := args[i]
		switch a.Type {
		case argInt:
			n, err := toInt32(v)
			if err != nil {
				return nil, nil, argErr(m, a, err)
			}
			buf = putU32(buf, uint32(n))
		case argUint:
			n, err := toUint32(v)
			if err != nil {
				return nil, nil, argErr(m, a, err)
			}
			buf = putU32(buf, n)
		case argFixed:
			f, ok := v.(fixed)
			if !ok {
				n, err := toInt32(v)
				if err != nil {
					return nil, nil, argErr(m, a, err)
				}
				f = fixedFromInt(int(n))
			}
			buf = putU32(buf, uint32(int32(f)))
		case argString:
			s, ok := v.(string)
			if !ok {
				return nil, nil, argErr(m, a, errors.New("not a string"))
			}
			buf = putString(buf, s)
		case argObject:
			n, err := toUint32(v)
			if err != nil {
				return nil, nil, argErr(m, a, err)
			}
			buf = putU32(buf, n)
		case argNewID:
			if a.Interface == "" {
				nid, ok := v.(newIDAny)
				if !ok {
					return nil, nil, argErr(m, a, errors.New("not a newIDAny"))
				}
				buf = putString(buf, nid.Interface)
				buf = putU32(buf, nid.Version)
				buf = putU32(buf, nid.ID)
				continue
			}
			n, err := toUint32(v)
			if err != nil {
				return nil, nil, argErr(m, a, err)
			}
			buf = putU32(buf, n)
		case argArray:
			b, ok := v.([]byte)
			if !ok {
				return nil, nil, argErr(m, a, errors.New("not a []byte"))
			}
			buf = putArray(buf, b)
		case argFD:
			fd, ok := v.(int)
			if !ok {
				return nil, nil, argErr(m, a, errors.New("not an fd"))
			}
			fds = append(fds, fd)
		}
	}
	if len(buf) > 0xffff {
		return nil, nil, fmt.Errorf("wl: %s: message too large (%d bytes)", m.Name, len(buf))
	}
	binary.LittleEndian.PutUint32(buf[0:4], objID)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(buf))<<16|uint32(m.Opcode))
	return buf, fds, nil
}

// countWireArgs is the number of Go values encodeMessage expects: every
// declared argument, file descriptors included (they are passed out of band
// but still given by the caller).
func countWireArgs(m *msgSpec) int { return len(m.Args) }

func argErr(m *msgSpec, a argSpec, err error) error {
	return fmt.Errorf("wl: %s argument %q: %w", m.Name, a.Name, err)
}

func putU32(b []byte, v uint32) []byte {
	return binary.LittleEndian.AppendUint32(b, v)
}

func putString(b []byte, s string) []byte {
	n := len(s) + 1 // the trailing NUL is part of the length
	b = putU32(b, uint32(n))
	b = append(b, s...)
	b = append(b, 0)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func putArray(b []byte, a []byte) []byte {
	b = putU32(b, uint32(len(a)))
	b = append(b, a...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func toUint32(v any) (uint32, error) {
	switch n := v.(type) {
	case uint32:
		return n, nil
	case uint:
		return uint32(n), nil
	case int:
		return uint32(n), nil
	case int32:
		return uint32(n), nil
	case bool:
		if n {
			return 1, nil
		}
		return 0, nil
	case nil:
		return 0, nil
	}
	return 0, fmt.Errorf("cannot use %T as uint", v)
}

func toInt32(v any) (int32, error) {
	switch n := v.(type) {
	case int32:
		return n, nil
	case int:
		return int32(n), nil
	case uint32:
		return int32(n), nil
	case bool:
		if n {
			return 1, nil
		}
		return 0, nil
	case nil:
		return 0, nil
	}
	return 0, fmt.Errorf("cannot use %T as int", v)
}

// ---------------------------------------------------------------- decoding

// decoder walks the body of a received message.
type decoder struct {
	b   []byte
	off int
}

func (d *decoder) u32() (uint32, error) {
	if d.off+4 > len(d.b) {
		return 0, errors.New("wl: truncated message")
	}
	v := binary.LittleEndian.Uint32(d.b[d.off : d.off+4])
	d.off += 4
	return v, nil
}

func (d *decoder) str() (string, error) {
	n, err := d.u32()
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	pad := int((n + 3) &^ 3)
	if d.off+pad > len(d.b) {
		return "", errors.New("wl: truncated string")
	}
	s := string(d.b[d.off : d.off+int(n)-1])
	d.off += pad
	return s, nil
}

func (d *decoder) array() ([]byte, error) {
	n, err := d.u32()
	if err != nil {
		return nil, err
	}
	pad := int((n + 3) &^ 3)
	if d.off+pad > len(d.b) {
		return nil, errors.New("wl: truncated array")
	}
	out := make([]byte, n)
	copy(out, d.b[d.off:d.off+int(n)])
	d.off += pad
	return out, nil
}

// decodeArgs decodes a message body according to a signature. Received file
// descriptors are popped from fds in order.
func decodeArgs(args []argSpec, body []byte, fds *[]int) ([]any, error) {
	d := &decoder{b: body}
	out := make([]any, 0, len(args))
	for _, a := range args {
		switch a.Type {
		case argInt:
			v, err := d.u32()
			if err != nil {
				return nil, err
			}
			out = append(out, int32(v))
		case argUint, argObject, argNewID:
			if a.Type == argNewID && a.Interface == "" {
				s, err := d.str()
				if err != nil {
					return nil, err
				}
				ver, err := d.u32()
				if err != nil {
					return nil, err
				}
				id, err := d.u32()
				if err != nil {
					return nil, err
				}
				out = append(out, newIDAny{Interface: s, Version: ver, ID: id})
				continue
			}
			v, err := d.u32()
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		case argFixed:
			v, err := d.u32()
			if err != nil {
				return nil, err
			}
			out = append(out, fixed(int32(v)))
		case argString:
			s, err := d.str()
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		case argArray:
			b, err := d.array()
			if err != nil {
				return nil, err
			}
			out = append(out, b)
		case argFD:
			if len(*fds) == 0 {
				return nil, errors.New("wl: message expects a file descriptor, none received")
			}
			fd := (*fds)[0]
			*fds = (*fds)[1:]
			out = append(out, fd)
		}
	}
	return out, nil
}

// ------------------------------------------------------------------ socket

// socketPath resolves a display name the way libwayland does.
func socketPath(display string) (string, error) {
	if display == "" {
		display = os.Getenv("WAYLAND_DISPLAY")
	}
	if display == "" {
		display = "wayland-0"
	}
	if filepath.IsAbs(display) {
		return display, nil
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return "", errors.New("wl: XDG_RUNTIME_DIR is not set and the display is not an absolute path")
	}
	return filepath.Join(dir, display), nil
}
