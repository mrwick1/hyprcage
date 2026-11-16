//go:build linux

package screen

import (
	"os"
	"syscall"
)

// deviceNumber returns the device number of a device file.
func deviceNumber(fi os.FileInfo) (uint64, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Rdev), true
}
