// Package audit appends one JSON line per tool call to the action log,
// $XDG_STATE_HOME/hyprcage/actions.jsonl (default ~/.local/state/hyprcage).
//
// Every line has time and tool. The fields screen, args, error, ack and
// verdict appear only when set, so a later field never changes the meaning
// of an older line. Typed text is never logged.
package audit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/hexadecimil/hyprcage/internal/screen"
)

// Entry is one line of the log.
type Entry struct {
	Time    string         `json:"time"`
	Screen  string         `json:"screen,omitempty"`
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args,omitempty"`
	Error   string         `json:"error,omitempty"`   // error code of a failed action
	Ack     string         `json:"ack,omitempty"`     // the reason the caller gave for a guarded action
	Verdict any            `json:"verdict,omitempty"` // the outcome of the action, when the tool computes one
}

// Option sets an optional field of an Entry.
type Option func(*Entry)

// WithVerdict sets the verdict field.
func WithVerdict(v any) Option { return func(e *Entry) { e.Verdict = v } }

// secret names the arguments that can hold typed or pasted text. Record
// removes them at any depth, for example inside the actions of a batch.
var secret = map[string]bool{"text": true, "expression": true, "value": true, "data": true}

// Path returns the log file.
func Path() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "hyprcage", "actions.jsonl")
}

// Record appends one line for a tool call with input in and result err.
// It never fails the call: a write error is dropped.
func Record(tool string, in any, err error, opts ...Option) {
	e := Entry{Time: time.Now().UTC().Format(time.RFC3339Nano), Tool: tool}
	if b, jerr := json.Marshal(in); jerr == nil {
		var args map[string]any
		if json.Unmarshal(b, &args) == nil {
			redact(args)
			if s, ok := args["screen"].(string); ok {
				e.Screen = s
				delete(args, "screen")
			}
			if a, ok := args["ack"].(string); ok {
				e.Ack = a
				delete(args, "ack")
			}
			if len(args) > 0 {
				e.Args = args
			}
		}
	}
	if err != nil {
		e.Error = "error"
		var se *screen.Error
		if errors.As(err, &se) {
			e.Error = string(se.Code)
		}
	}
	for _, o := range opts {
		o(&e)
	}
	line, jerr := json.Marshal(e)
	if jerr != nil {
		return
	}
	p := Path()
	if os.MkdirAll(filepath.Dir(p), 0o700) != nil {
		return
	}
	f, oerr := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if oerr != nil {
		return
	}
	defer f.Close()
	f.Write(append(line, '\n')) // one write call keeps lines whole across processes
}

func redact(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if secret[k] {
				delete(t, k)
				continue
			}
			redact(x)
		}
	case []any:
		for _, x := range t {
			redact(x)
		}
	}
}
