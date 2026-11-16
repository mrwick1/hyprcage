//go:build !linux

package browser

import (
	"errors"
	"os"
)

func flockExclusive(f *os.File) error {
	return errors.New("browser: profile locks need Linux")
}
