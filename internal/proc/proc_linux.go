//go:build linux

// Package proc holds the process calls that differ between Linux and the
// other systems.
package proc

import (
	"os/exec"
	"syscall"
)

// Detach starts c in its own session, so it outlives its parent and a signal
// to the parent does not reach it.
func Detach(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

// Kill sends sig to pid. A negative pid targets the process group. Signal 0
// only tests that the process exists.
func Kill(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }

// KillTree kills pid and every process of its group. It expects a process
// that Detach started, which leads its own group.
func KillTree(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
