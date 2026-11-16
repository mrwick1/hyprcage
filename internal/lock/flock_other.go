//go:build !linux

package lock

import (
	"errors"
	"os"
)

func tryLock(f *os.File) (bool, error) {
	return false, errors.New("lock: file locks need Linux")
}

func unlock(f *os.File) {}
