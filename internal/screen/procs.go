package screen

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/session"
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

// listenInode returns the socket inode that listens on 127.0.0.1:port,
// or "" when nothing listens there.
func listenInode(port int) (string, error) {
	data, err := os.ReadFile("/proc/net/tcp")
	if err != nil {
		return "", err
	}
	local := fmt.Sprintf("0100007F:%04X", port)
	for _, line := range strings.Split(string(data), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) > 9 && f[1] == local && f[3] == "0A" { // 0A is LISTEN
			return f[9], nil
		}
	}
	return "", nil
}

// holds reports whether one of the fds of pid is the socket inode.
func holds(pid int, inode string) bool {
	fds, _ := filepath.Glob(fmt.Sprintf("/proc/%d/fd/*", pid))
	target := "socket:[" + inode + "]"
	for _, fd := range fds {
		if link, err := os.Readlink(fd); err == nil && link == target {
			return true
		}
	}
	return false
}

// ListenerPID returns the PID of the process that listens on
// 127.0.0.1:port, or 0 when nothing listens there. It returns an error
// when a listener exists but its owner is not visible to this user.
// Linux only. It scans the fds of every process: call it once per port,
// not per read.
func ListenerPID(port int) (int, error) {
	inode, err := listenInode(port)
	if err != nil || inode == "" {
		return 0, err
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

// OwnsPort reports whether the process recorded as the owner of
// rec.DebugPort still listens on it: same PID, same start time. It reads
// only that process's fds.
func OwnsPort(rec *registry.Screen) bool {
	if rec.DebugPID <= 0 || rec.DebugPIDStart == 0 {
		return false
	}
	inode, err := listenInode(rec.DebugPort)
	if err != nil || inode == "" || !holds(rec.DebugPID, inode) {
		return false
	}
	st, err := session.ProcStart(rec.DebugPID)
	return err == nil && st == rec.DebugPIDStart
}

// killDebug stops the process recorded as the owner of the DevTools port
// when it is still the same process: SIGTERM, up to 2 s, then SIGKILL.
// Chromium leaves the screen's cgroup and environment, so
// killScreenProcesses misses it.
func killDebug(rec *registry.Screen) {
	alive := func() bool {
		st, err := session.ProcStart(rec.DebugPID)
		return err == nil && st == rec.DebugPIDStart
	}
	if rec.DebugPID <= 0 || rec.DebugPIDStart == 0 || !alive() {
		return
	}
	_ = syscall.Kill(rec.DebugPID, syscall.SIGTERM)
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if !alive() || zombie(rec.DebugPID) {
			return
		}
	}
	_ = syscall.Kill(rec.DebugPID, syscall.SIGKILL)
}

// zombie reports whether pid has exited and waits for its parent to reap it.
func zombie(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	s := string(data)
	i := strings.LastIndex(s, ")")
	return i >= 0 && strings.HasPrefix(s[i+1:], " Z")
}
