// Package notifyd keeps the desktop notifications of the last 48 hours in a
// JSON-lines file. A monitor on the session bus appends one line per event;
// the notification tools read the file and never talk to the daemon.
package notifyd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Keep is how long an entry stays in the file.
const Keep = 48 * time.Hour

// Entry is one line of the file.
type Entry struct {
	ID      uint32            `json:"id"`
	App     string            `json:"app"`
	Summary string            `json:"summary"`
	Body    string            `json:"body"`
	Actions map[string]string `json:"actions,omitempty"` // key -> label
	Time    time.Time         `json:"time"`
	Event   string            `json:"event"`            // "notify", "closed" or "action"
	Action  string            `json:"action,omitempty"` // the invoked key, for "action"
}

// Path is $HYPRCAGE_NOTIFY_FILE, else ~/.local/state/hyprcage/notifications.jsonl.
func Path() string {
	if p := os.Getenv("HYPRCAGE_NOTIFY_FILE"); p != "" {
		return p
	}
	return filepath.Join(os.Getenv("HOME"), ".local", "state", "hyprcage", "notifications.jsonl")
}

// Append adds e as one line, creating the file (0600) and its directory (0700).
func Append(path string, e Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Read returns the entries of the file. A line that does not parse, such
// as the torn last line of a crashed write, is skipped.
func Read(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Entry
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

// Prune drops the entries older than Keep. It writes the rest to a
// temporary file in the same directory and renames it over the file, so
// that an error or a crash leaves the old file whole.
func Prune(path string, now time.Time) error {
	entries, err := Read(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	for _, e := range entries {
		if e.Time.Before(now.Add(-Keep)) {
			continue
		}
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		buf.Write(append(line, '\n'))
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".notifications-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
