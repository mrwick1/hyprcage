//go:build !linux

package screen

import (
	"syscall"

	"github.com/hexadecimil/hyprcage/internal/proc"
)

// pidAlive only tests that pid exists: start times need the Linux /proc.
// Phase 3 of the Windows port replaces it.
func pidAlive(pid int, _ uint64) bool { return pid > 0 && proc.Kill(pid, syscall.Signal(0)) == nil }
