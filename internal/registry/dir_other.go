//go:build !windows

package registry

import (
	"fmt"
	"os"
	"path/filepath"
)

func defaultDir() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("hyprcage-%d", os.Getuid()))
}

// retry runs f once: another process never blocks a file here.
func retry(f func() error) error { return f() }
