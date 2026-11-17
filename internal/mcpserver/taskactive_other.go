//go:build !linux

package mcpserver

import "github.com/hexadecimil/offstage/internal/registry"

func tasksActive(registry.Owner) bool { return false }
