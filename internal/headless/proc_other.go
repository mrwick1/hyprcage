//go:build !linux && !windows

package headless

import "os/exec"

func setupCmd(c *exec.Cmd) {}

// killTree kills Chrome. Its children end with it.
func killTree(c *exec.Cmd) { c.Process.Kill() }
