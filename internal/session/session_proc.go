//go:build !windows

package session

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ProcStart returns the start time of pid (clock ticks since boot, field 22
// of /proc/<pid>/stat), which tells a reused pid from the original process.
func ProcStart(pid int) (uint64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	return parseStartTime(string(data))
}

// parseStartTime extracts field 22 of a /proc/<pid>/stat line; the command
// name in parentheses may itself contain spaces and parentheses.
func parseStartTime(s string) (uint64, error) {
	i := strings.LastIndex(s, ")")
	if i < 0 {
		return 0, errors.New("malformed /proc stat")
	}
	fields := strings.Fields(s[i+1:]) // fields[0] is the state, field 3
	if len(fields) < 20 {
		return 0, errors.New("short /proc stat")
	}
	return strconv.ParseUint(fields[19], 10, 64)
}

func procComm(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
