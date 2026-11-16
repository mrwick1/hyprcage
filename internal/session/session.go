// Package session identifies the agent session that owns a screen and
// decides whether it is still alive (cahier §5.1, §5.2).
package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hexadecimil/offstage/internal/registry"
)

// Info is what the SessionStart hook records about a Claude Code session,
// keyed by the pid of the CLI process. A stdio MCP server cannot see
// CLAUDE_CODE_SESSION_ID in its environment; this file is its source.
type Info struct {
	SessionID     string    `json:"session_id"`
	HostSessionID string    `json:"host_session_id,omitempty"`
	Source        string    `json:"source,omitempty"`
	PID           int       `json:"pid"`
	At            time.Time `json:"at"`
}

func dir() string         { return filepath.Join(registry.Dir(), "sessions") }
func path(pid int) string { return filepath.Join(dir(), strconv.Itoa(pid)+".json") }

// Write records the session of one CLI process.
func Write(info Info) error {
	if err := os.MkdirAll(dir(), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return registry.WriteAtomic(path(info.PID), data, 0o600)
}

// Read returns the session recorded for a CLI process, if any.
func Read(pid int) (*Info, error) {
	data, err := os.ReadFile(path(pid))
	if err != nil {
		return nil, err
	}
	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// PIDAlive reports whether pid exists and, when start is known, is the same process.
func PIDAlive(pid int, start uint64) bool {
	if pid <= 0 {
		return false
	}
	st, err := ProcStart(pid)
	if err != nil {
		return false
	}
	return start == 0 || st == start
}

// Identity is what the MCP server or the CLI records as a screen's owner.
type Identity struct {
	SessionID string
	PID       int
	PIDStart  uint64
	Client    string
}

// Current derives the identity of the calling context: the Claude Code
// process is CLAUDE_PID (tool and hook environments) or the parent process;
// the session id comes from the hook-written file, else from the tool
// environment, else it stays empty (pid-based ownership, cahier §5.1).
func Current() Identity {
	pid := os.Getppid()
	if v := os.Getenv("CLAUDE_PID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pid = n
		}
	}
	id := Identity{PID: pid}
	id.PIDStart, _ = ProcStart(pid)
	if info, err := Read(pid); err == nil && info.SessionID != "" {
		id.SessionID = info.SessionID
	} else if v := os.Getenv("CLAUDE_CODE_SESSION_ID"); v != "" {
		id.SessionID = v
	}
	comm := procComm(pid)
	switch {
	case os.Getenv("CLAUDECODE") != "" || strings.HasPrefix(comm, "claude"):
		id.Client = "claude-code"
	case strings.HasPrefix(comm, "codex"):
		id.Client = "codex"
	default:
		id.Client = "unknown"
	}
	return id
}

// Owner converts the identity into a registry owner.
func (id Identity) Owner() registry.Owner {
	return registry.Owner{SessionID: id.SessionID, PID: id.PID, PIDStart: id.PIDStart, Client: id.Client}
}

// IsAlive implements cahier §5.2: the owner is alive while its pid exists
// (same start time), or, for a known session, while one of its records
// carries a heartbeat younger than the grace period, which covers a restart
// under a new pid. /clear (same pid, new id) is handled by the hooks, not here.
func IsAlive(owner registry.Owner, heartbeat time.Time, grace time.Duration) bool {
	if PIDAlive(owner.PID, owner.PIDStart) {
		return true
	}
	return owner.SessionID != "" && !heartbeat.IsZero() && time.Since(heartbeat) < grace
}
