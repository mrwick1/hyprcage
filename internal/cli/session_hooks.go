package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/proc"
	"github.com/hexadecimil/offstage/internal/session"
	"github.com/hexadecimil/offstage/internal/sysd"
)

// hookInput is the JSON Claude Code passes to hooks on stdin.
type hookInput struct {
	SessionID string `json:"session_id"`
	Source    string `json:"source"` // SessionStart: startup, resume, clear, compact, fork
	Reason    string `json:"reason"` // SessionEnd: clear, resume, logout, prompt_input_exit, other
}

func readHook(e *Env) (hookInput, error) {
	var in hookInput
	data, err := io.ReadAll(io.LimitReader(e.Stdin, 1<<20))
	if err != nil {
		return in, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return in, nil
	}
	return in, json.Unmarshal(data, &in)
}

// cliPID is the Claude Code process: CLAUDE_PID in hook and tool
// environments, the parent otherwise.
func cliPID() int {
	if v := os.Getenv("CLAUDE_PID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return os.Getppid()
}

// runSessionStart records the session id for the MCP server (cahier §5.1)
// and, after /clear, reaps the screens of the previous session id.
func runSessionStart(e *Env) int {
	in, err := readHook(e)
	if err != nil {
		return e.errorf("session-start: %v", err)
	}
	pid := cliPID()
	prev, _ := session.Read(pid)
	_ = session.Write(session.Info{
		SessionID: in.SessionID, HostSessionID: os.Getenv("CLAUDE_CODE_HOST_SESSION_ID"),
		Source: in.Source, PID: pid, At: time.Now(),
	})
	if in.Source == "clear" && prev != nil && prev.SessionID != "" && prev.SessionID != in.SessionID {
		return gcSession(e, prev.SessionID, 0)
	}
	return ExitOK
}

// runSessionEnd is mechanism M2 (cahier §5.3): destroy now on a definitive
// end, after the grace period when the session may come back under a new pid.
func runSessionEnd(e *Env) int {
	in, err := readHook(e)
	if err != nil {
		return e.errorf("session-end: %v", err)
	}
	if in.SessionID == "" {
		return ExitOK
	}
	switch in.Reason {
	case "clear", "logout", "prompt_input_exit":
		return gcSession(e, in.SessionID, 0)
	default:
		return gcSession(e, in.SessionID, config.Fallback().SessionGrace)
	}
}

// gcSession schedules `offstage gc --session ID`, detached: the hook budget
// is not guaranteed, so it never waits for the work itself.
func gcSession(e *Env, sessionID string, after time.Duration) int {
	exe, err := os.Executable()
	if err != nil {
		return e.errorf("%v", err)
	}
	cmd := []string{exe, "gc", "--session", sessionID}
	if after <= 0 {
		after = time.Second
	}
	if err := sysd.ScheduleOnce(after, "", cmd); err != nil {
		// Without systemd (Windows) nothing waits. A delayed gc is dropped:
		// the next screen_create and the server's disconnect reap the screens.
		if runtime.GOOS != "linux" && after > time.Second {
			return ExitOK
		}
		c := exec.Command(cmd[0], cmd[1:]...)
		proc.Detach(c)
		if err := c.Start(); err != nil {
			return e.errorf("%v", err)
		}
	}
	return ExitOK
}
