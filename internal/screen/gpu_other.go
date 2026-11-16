//go:build !linux

package screen

import "os"

func deviceNumber(fi os.FileInfo) (uint64, bool) { return 0, false }
