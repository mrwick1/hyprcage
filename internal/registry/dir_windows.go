package registry

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

// defaultDir is %LOCALAPPDATA%\offstage\run, which the login session does not clear.
func defaultDir() string {
	base := os.Getenv("LocalAppData")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "offstage", "run")
}

// retry runs f again for up to a second while another process holds the
// file. Go opens files without FILE_SHARE_DELETE, so a reader blocks the
// replace of a record, and the replace blocks a reader for a moment.
func retry(f func() error) error {
	err := f()
	for i := 0; i < 100 && (errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED)); i++ {
		time.Sleep(10 * time.Millisecond)
		err = f()
	}
	return err
}
