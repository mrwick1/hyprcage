package headless

import (
	"os/exec"
	"syscall"
)

// setupCmd puts Chrome in its own process group.
func setupCmd(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// killTree kills the whole process group of Chrome.
func killTree(c *exec.Cmd) { syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
