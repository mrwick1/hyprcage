// Package screen implements the life cycle of an agent screen: create,
// launch, destroy, gc (cahier §4 and §5).
package screen

import "fmt"

// Code is a structured error code (cahier N8), returned to MCP clients as an
// isError tool result and mapped to exit codes by the CLI.
type Code string

const (
	CodeNotFound     Code = "screen_not_found"
	CodeDead         Code = "screen_dead"
	CodeNotOwner     Code = "not_owner"
	CodeInvalidName  Code = "invalid_name"
	CodeHyprland     Code = "hyprland_unreachable"
	CodeCage         Code = "cage_missing"
	CodeCapture      Code = "capture_failed"
	CodeTimeout      Code = "timeout"
	CodeLimit        Code = "limit" // size or number of screens beyond the configuration
	CodeRecording    Code = "recording_active"
	CodeNotRecording Code = "not_recording"
	CodeLocked       Code = "session_locked"
	CodeBrowserBusy  Code = "browser_running"
	CodeBrowserDown  Code = "browser_not_running"
	CodeUnsupported  Code = "unsupported_input"
	CodeAddress      Code = "invalid_address"
	CodeStaleRef     Code = "stale_ref" // the ref no longer matches exactly one node
	CodeNoSource     Code = "no_source"
	CodeRefOffscreen Code = "ref_offscreen"
	CodeRefOccluded  Code = "ref_occluded" // another element covers the node
	CodeCDP          Code = "cdp_unreachable"
)

// Error carries a code, a message and a hint for the caller.
type Error struct {
	Code Code
	Msg  string
	Hint string
}

func (e *Error) Error() string {
	if e.Hint != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Msg, e.Hint)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Msg)
}

func errf(code Code, hint, format string, a ...any) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, a...), Hint: hint}
}

// Errf builds an Error for the packages outside screen (record, desktop,
// browser), so that every tool reports the same way.
func Errf(code Code, hint, format string, a ...any) *Error { return errf(code, hint, format, a...) }
