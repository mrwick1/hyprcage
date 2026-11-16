//go:build !linux

package mcpserver

import (
	"github.com/hexadecimil/offstage/internal/audit"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func timelineMetadata(in any) []audit.Option                            { return nil }
func (s *Server) recordTimeline(e audit.Entry, res *mcp.CallToolResult) {}
func (s *Server) registerTimeline(srv *mcp.Server)                      {}
