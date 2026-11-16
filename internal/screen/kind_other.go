//go:build !linux && !windows

package screen

import (
	"syscall"

	"github.com/hexadecimil/hyprcage/internal/proc"
)

// pidAlive only tests that pid exists: start times need /proc on Linux and
// the process times on Windows.
func pidAlive(pid int, _ uint64) bool { return pid > 0 && proc.Kill(pid, syscall.Signal(0)) == nil }
