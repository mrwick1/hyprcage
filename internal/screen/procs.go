package screen

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// ScreenProcesses lists the processes that belong to a screen, by the
// HYPRCAGE_SCREEN variable every one of them carries: cage, the mirror,
// the applications. It is the inventory when systemd is not there to keep
// one, and the last resort after it.
func ScreenProcesses(name string) []int {
	want := []byte("HYPRCAGE_SCREEN=" + name + "\x00")
	self := os.Getpid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		env, err := os.ReadFile(filepath.Join("/proc", e.Name(), "environ"))
		if err != nil {
			continue
		}
		if bytes.Contains(env, want) {
			pids = append(pids, pid)
		}
	}
	return pids
}

// killScreenProcesses signals every process of the screen. Applications
// must see their SIGTERM before their compositor dies, so cage, the parent
// of every other process's session, is signalled last.
func killScreenProcesses(name string, sig syscall.Signal) {
	pids := ScreenProcesses(name)
	var cage int
	for _, pid := range pids {
		if cmd, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm")); err == nil && string(bytes.TrimSpace(cmd)) == "cage" {
			cage = pid
			continue
		}
		_ = syscall.Kill(pid, sig)
	}
	if cage != 0 {
		_ = syscall.Kill(cage, sig)
	}
}
