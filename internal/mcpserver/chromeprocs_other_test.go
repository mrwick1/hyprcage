//go:build !linux && !windows

package mcpserver

// chromeProcs has no process list to read on this system.
func chromeProcs(string) []string { return nil }
