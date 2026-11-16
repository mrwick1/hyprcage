//go:build !linux

package mirror

import "github.com/hexadecimil/hyprcage/internal/registry"

// Run does nothing but log: the mirror needs Wayland, so it needs Linux.
func Run(rec *registry.Screen, opts Options) int {
	if opts.Log != nil {
		opts.Log("mirror: needs Linux")
	}
	return ExitError
}
