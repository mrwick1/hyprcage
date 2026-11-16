//go:build !linux

package wl

func mmapShared(fd, size int) ([]byte, error) { return nil, errUnsupported }

func munmap(b []byte) error { return nil }
