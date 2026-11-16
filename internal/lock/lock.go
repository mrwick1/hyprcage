// Package lock serialises create, destroy and gc across processes with a
// flock, reentrant within one process (cahier §5.4).
package lock

import (
	"errors"
	"os"
	"sync"
	"time"

	"github.com/hexadecimil/hyprcage/internal/registry"
)

// ErrBusy is returned when the lock could not be obtained in time.
var ErrBusy = errors.New("hyprcage lock busy (another create, destroy or gc is running)")

var (
	mu    sync.Mutex
	depth int
	file  *os.File
)

// Acquire takes the global lock, waiting up to timeout. Nested calls from the
// same process share the descriptor, so an internal gc inside create never
// deadlocks. Goroutines of one process are serialised by the mutex.
func Acquire(timeout time.Duration) (release func(), err error) {
	mu.Lock()
	defer mu.Unlock()
	if depth > 0 {
		depth++
		return releaseFn, nil
	}
	if _, err := registry.EnsureDir(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(registry.LockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		held, ferr := tryLock(f)
		if ferr != nil {
			f.Close()
			return nil, ferr
		}
		if held {
			break
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, ErrBusy
		}
		time.Sleep(50 * time.Millisecond)
	}
	file = f
	depth = 1
	return releaseFn, nil
}

func releaseFn() {
	mu.Lock()
	defer mu.Unlock()
	if depth == 0 {
		return
	}
	depth--
	if depth == 0 && file != nil {
		unlock(file)
		file.Close()
		file = nil
	}
}
