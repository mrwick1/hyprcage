//go:build linux

package wl

import "syscall"

func mmapShared(fd, size int) ([]byte, error) {
	return syscall.Mmap(fd, 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
}

func munmap(b []byte) error { return syscall.Munmap(b) }
