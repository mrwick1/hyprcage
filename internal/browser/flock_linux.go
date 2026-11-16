//go:build linux

package browser

import (
	"os"
	"syscall"
)

// flockExclusive takes an exclusive lock on f and fails at once when another
// process holds it.
func flockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
