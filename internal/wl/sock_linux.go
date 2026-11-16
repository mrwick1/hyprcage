//go:build linux

package wl

import (
	"errors"
	"fmt"
	"syscall"
	"time"
)

// dial opens the Unix socket of a compositor and returns its fd.
func dial(display string) (int, error) {
	path, err := socketPath(display)
	if err != nil {
		return -1, err
	}
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("wl: socket: %w", err)
	}
	if err := syscall.Connect(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
		syscall.Close(fd)
		return -1, fmt.Errorf("wl: connect %s: %w", path, err)
	}
	if err := setReadTimeout(fd, readTimeout); err != nil {
		syscall.Close(fd)
		return -1, err
	}
	return fd, nil
}

func setReadTimeout(fd int, d time.Duration) error {
	tv := syscall.NsecToTimeval(int64(d))
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		return fmt.Errorf("wl: SO_RCVTIMEO: %w", err)
	}
	return nil
}

// sendAll writes buf on fd, attaching fds as SCM_RIGHTS to the same sendmsg
// as the first byte of buf.
func sendAll(fd int, buf []byte, fds []int) error {
	var oob []byte
	if len(fds) > 0 {
		oob = syscall.UnixRights(fds...)
	}
	for len(buf) > 0 {
		n, err := syscall.SendmsgN(fd, buf, oob, nil, 0)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			return fmt.Errorf("wl: sendmsg: %w", err)
		}
		buf = buf[n:]
		oob = nil // ancillary data travels once, with the first chunk
	}
	return nil
}

// recvChunk reads bytes and any ancillary file descriptors from fd.
func recvChunk(fd int, buf []byte) (int, []int, error) {
	oob := make([]byte, syscall.CmsgSpace(4*16))
	for {
		n, oobn, _, _, err := syscall.Recvmsg(fd, buf, oob, syscall.MSG_CMSG_CLOEXEC)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				return 0, nil, errors.New("wl: timed out waiting for the compositor")
			}
			return 0, nil, fmt.Errorf("wl: recvmsg: %w", err)
		}
		var fds []int
		if oobn > 0 {
			scms, perr := syscall.ParseSocketControlMessage(oob[:oobn])
			if perr != nil {
				return 0, nil, fmt.Errorf("wl: control message: %w", perr)
			}
			for _, scm := range scms {
				got, perr := syscall.ParseUnixRights(&scm)
				if perr != nil {
					continue
				}
				fds = append(fds, got...)
			}
		}
		if n == 0 && oobn == 0 {
			return 0, nil, errors.New("wl: connection closed by the compositor")
		}
		return n, fds, nil
	}
}

// closeFD closes a file descriptor.
func closeFD(fd int) error { return syscall.Close(fd) }
