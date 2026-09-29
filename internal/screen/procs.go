package screen

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
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

// ListenerPID returns the PID of the process that listens on
// 127.0.0.1:port, or 0 when nothing listens there. It returns an error
// when a listener exists but its owner is not visible to this user.
// Linux only: it reads /proc/net/tcp and the fd links of /proc.
func ListenerPID(port int) (int, error) {
	data, err := os.ReadFile("/proc/net/tcp")
	if err != nil {
		return 0, err
	}
	local := fmt.Sprintf("0100007F:%04X", port)
	inode := ""
	for _, line := range strings.Split(string(data), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) > 9 && f[1] == local && f[3] == "0A" { // 0A is LISTEN
			inode = f[9]
			break
		}
	}
	if inode == "" {
		return 0, nil
	}
	target := "socket:[" + inode + "]"
	fds, _ := filepath.Glob("/proc/[0-9]*/fd/*")
	for _, fd := range fds {
		if link, err := os.Readlink(fd); err == nil && link == target {
			return strconv.Atoi(strings.Split(fd, "/")[2])
		}
	}
	return 0, fmt.Errorf("the owner of 127.0.0.1:%d is not visible", port)
}

// OwnsPort reports whether the process that listens on 127.0.0.1:port is
// a process of the screen name. No listener, or a hidden one, is not owned.
func OwnsPort(name string, port int) bool {
	pid, err := ListenerPID(port)
	return err == nil && pid > 0 && slices.Contains(ScreenProcesses(name), pid)
}
