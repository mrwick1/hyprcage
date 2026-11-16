//go:build !linux

package mcpserver

import "github.com/modelcontextprotocol/go-sdk/mcp"

func (s *Server) registerConditions(srv *mcp.Server) {}
