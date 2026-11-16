//go:build !linux

package mcpserver

import (
	"fmt"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerTasks(srv *mcp.Server) {}
func validateTask(taskID string) error {
	if taskID != "" {
		return fmt.Errorf("tasks require Linux")
	}
	return nil
}
func (s *Server) attachTask(taskID string, rec *registry.Screen) error { return validateTask(taskID) }
