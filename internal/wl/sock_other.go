//go:build !linux

package wl

import (
	"errors"
	"time"
)

var errUnsupported = errors.New("wl: Wayland needs Linux")

func dial(display string) (int, error) { return -1, errUnsupported }

func setReadTimeout(fd int, d time.Duration) error { return errUnsupported }

func sendAll(fd int, buf []byte, fds []int) error { return errUnsupported }

func recvChunk(fd int, buf []byte) (int, []int, error) { return 0, nil, errUnsupported }

// closeFD does nothing: no file descriptor of this package exists here.
func closeFD(fd int) error { return nil }
