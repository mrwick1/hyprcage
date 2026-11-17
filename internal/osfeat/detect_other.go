//go:build !linux

package osfeat

import "github.com/hexadecimil/offstage/internal/config"

// Detect returns the backends of this system: none yet.
func Detect(config.Config) Set { return Set{} }
