//go:build linux

package mcpserver

import (
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/task"
)

func tasksActive(o registry.Owner) bool { return task.Active(o) }
