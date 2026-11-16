package headless

import (
	"os/exec"
	"strconv"
)

func setupCmd(c *exec.Cmd) {}

// killTree kills Chrome and its children.
func killTree(c *exec.Cmd) {
	exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(c.Process.Pid)).Run()
}
