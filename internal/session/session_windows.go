package session

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// stillActive is the exit code that GetExitCodeProcess gives a running process.
const stillActive = 259

// openLive opens pid for queries. A process that exited but that a handle
// still holds is dead: it fails like a missing one.
func openLive(pid int) (windows.Handle, error) {
	if pid <= 0 {
		return 0, errors.New("invalid pid")
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0, err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil || code != stillActive {
		windows.CloseHandle(h)
		return 0, errors.New("process exited")
	}
	return h, nil
}

// ProcStart returns the creation time of pid (100 ns ticks since 1601), which
// tells a reused pid from the original process. It fails for a process that
// has exited.
func ProcStart(pid int) (uint64, error) {
	h, err := openLive(pid)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return 0, err
	}
	return uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime), nil
}

// procComm is the name of the program of pid without its extension, or "".
func procComm(pid int) string {
	h, err := openLive(pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return ""
	}
	name := filepath.Base(windows.UTF16ToString(buf[:n]))
	return strings.TrimSuffix(strings.ToLower(name), ".exe")
}
