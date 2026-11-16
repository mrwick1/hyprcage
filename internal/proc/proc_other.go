//go:build !linux

// Package proc holds the process calls that differ between Linux and the
// other systems.
package proc

import (
	"os"
	"os/exec"
	"syscall"
)

// Detach does nothing: there are no sessions on this system.
func Detach(c *exec.Cmd) {}

// Kill stops pid. Every signal but 0 kills the process, and a process group
// is not supported: a negative pid targets the process of the same number.
// Signal 0 only tests that the process exists.
func Kill(pid int, sig syscall.Signal) error {
	if pid < 0 {
		pid = -pid
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if sig == 0 {
		return p.Release()
	}
	return p.Kill()
}
