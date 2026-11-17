package mcpserver

import (
	"sync"

	"github.com/hexadecimil/offstage/internal/audit"
	"github.com/hexadecimil/offstage/internal/report"
	"github.com/hexadecimil/offstage/internal/session"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// identity is this server's session, read once for the block log.
var identity = sync.OnceValue(session.Current)

// logBlock writes a failed call to the block log and returns its error
// text. A write error is dropped: the log must never fail a tool call.
func logBlock(e audit.Entry, err error, res *mcp.CallToolResult) string {
	msg := ""
	if err != nil {
		msg = err.Error()
	} else if res != nil {
		msg = resultText(res)
	}
	id := identity()
	_ = report.LogBlock(report.Block{
		Time: e.Time, Session: id.SessionID, Client: id.Client, ActionID: e.ActionID,
		Tool: e.Tool, Screen: e.Screen, Code: e.Error, Message: msg,
	})
	return msg
}
