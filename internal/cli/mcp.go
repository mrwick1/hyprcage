package cli

import "github.com/hexadecimil/offstage/internal/mcpserver"

// runMCP serves MCP on stdio for one agent session (cahier §6.2).
func runMCP(e *Env) int {
	if len(e.Args) != 0 {
		return e.errorf("usage: offstage mcp   (no arguments; speaks MCP on stdin/stdout)")
	}
	if err := mcpserver.Run(); err != nil {
		return e.fail(err)
	}
	return ExitOK
}
