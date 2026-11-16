// Package audit appends one JSON line per tool call to the action log,
// $XDG_STATE_HOME/offstage/actions.jsonl (default ~/.local/state/offstage,
// %LOCALAPPDATA%\offstage on Windows).
//
// Every line has time and tool. The fields screen, args, error, ack, verdict,
// target and ms appear only when set, so a later field never changes the
// meaning of an older line. Typed text is never logged: only its length is.
package audit

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	textredact "github.com/hexadecimil/offstage/internal/redact"
	"github.com/hexadecimil/offstage/internal/screen"
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
	Target  []Target       `json:"target,omitempty"`  // the elements the action aimed at, for a replay
	Ms      int64          `json:"ms,omitempty"`      // how long the call took, in milliseconds
}

// Target is the element of the last snapshot under a point or a ref of an
// action. A replay finds it again by role and name. An empty Target means
// that no element was known there.
type Target struct {
	Role string `json:"role,omitempty"`
	Name string `json:"name,omitempty"`
	X    int    `json:"x,omitempty"` // centre of the element, screen pixels
	Y    int    `json:"y,omitempty"`
}

// Option sets an optional field of an Entry.
type Option func(*Entry)

// WithVerdict sets the verdict field.
func WithVerdict(v any) Option { return func(e *Entry) { e.Verdict = v } }

// WithDuration sets the ms field.
func WithDuration(d time.Duration) Option { return func(e *Entry) { e.Ms = d.Milliseconds() } }

// WithTarget sets the target field.
func WithTarget(t []Target) Option { return func(e *Entry) { e.Target = t } }

// secret names the arguments that can hold typed or pasted text. Record
// replaces them at any depth, for example inside the actions of a batch,
// with <name>_len, the length of the text in characters. Other arguments
// that can hold a secret are reduced by scrub: env to its names, the
// arguments of command through redact.Text, url to scheme, host and path,
// and a single-character key to a placeholder.
var secret = map[string]bool{"text": true, "expression": true, "value": true, "data": true}

// Path returns the log file.
func Path() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" && runtime.GOOS == "windows" {
		dir = os.Getenv("LocalAppData")
	}
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "offstage", "actions.jsonl")
}

// Record appends one line for a tool call with input in and result err, and
// returns the line. It never fails the call: a write error is dropped.
func Record(tool string, in any, err error, opts ...Option) Entry {
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
		return e
	}
	p := Path()
	if os.MkdirAll(filepath.Dir(p), 0o700) != nil {
		return e
	}
	f, oerr := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if oerr != nil {
		return e
	}
	defer f.Close()
	f.Write(append(line, '\n')) // one write call keeps lines whole across processes
	return e
}

func redact(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if secret[k] {
				delete(t, k)
				if str, ok := x.(string); ok {
					t[k+"_len"] = utf8.RuneCountInString(str)
				}
				continue
			}
			t[k] = scrub(k, x)
			redact(t[k])
		}
	case []any:
		for _, x := range t {
			redact(x)
		}
	}
}

// scrub reduces the value of an argument that can hold a secret.
func scrub(k string, v any) any {
	switch x := v.(type) {
	case map[string]any:
		if k == "env" {
			names := make([]string, 0, len(x))
			for n := range x {
				names = append(names, n)
			}
			sort.Strings(names)
			return names
		}
	case []any:
		switch k {
		case "command":
			out := make([]any, len(x))
			for i, a := range x {
				out[i] = a
				if s, ok := a.(string); ok && i > 0 {
					out[i] = textredact.Text(s)
				}
			}
			return out
		case "keys":
			out := make([]any, len(x))
			for i, e := range x {
				out[i] = e
				if s, ok := e.(string); ok && !loggableKey(s) {
					out[i] = "<char>"
				}
			}
			return out
		}
	case string:
		if k == "url" || k == "url_contains" {
			u, err := url.Parse(x)
			if err != nil || u.Host == "" {
				return "<redacted>"
			}
			return u.Scheme + "://" + u.Host + u.Path
		}
	}
	return v
}

// controlKeys are the keysyms that print nothing. A key press is logged only
// when its last key is one of these, or when it holds ctrl, alt or super,
// which do not enter text. Any other key can be part of a typed secret.
var controlKeys = map[string]bool{
	"return": true, "kp_enter": true, "tab": true, "iso_left_tab": true, "escape": true, "backspace": true,
	"delete": true, "insert": true, "home": true, "end": true, "page_up": true, "page_down": true, "prior": true, "next": true,
	"up": true, "down": true, "left": true, "right": true, "menu": true, "print": true, "pause": true,
}

func loggableKey(k string) bool {
	parts := strings.Split(strings.ToLower(k), "+")
	for _, m := range parts[:len(parts)-1] {
		if m == "ctrl" || m == "control" || m == "alt" || m == "super" || m == "meta" {
			return true
		}
	}
	last := parts[len(parts)-1]
	if controlKeys[last] {
		return true
	}
	if strings.HasPrefix(last, "f") && len(last) > 1 && len(last) <= 3 && strings.Trim(last[1:], "0123456789") == "" {
		return true
	}
	return false
}
