//go:build !linux

package mcpserver

import (
	"context"
	"github.com/hexadecimil/offstage/internal/condition"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerConditions(srv *mcp.Server) {}

func observeProcessCondition(ctx context.Context, p predicate) condition.Observation {
	return condition.Observation{Status: "unsupported", Evidence: "process conditions require Linux"}
}
