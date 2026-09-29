// Package registry stores one JSON record per screen under
// $XDG_RUNTIME_DIR/hyprcage (tmpfs: the state dies with the login session),
// plus the .inner file cage's holder writes and the global lock file.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrNotFound is returned by Load for an unknown screen.
var ErrNotFound = errors.New("screen not found")

// Owner identifies the session that created a screen (cahier §5.1).
type Owner struct {
	SessionID string `json:"session_id"`
	PID       int    `json:"pid"`
	PIDStart  uint64 `json:"pid_start"`
	Client    string `json:"client"`
}

// Screen is the record of one agent screen. Its file mtime is the heartbeat.
type Screen struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	State     string    `json:"state"` // starting, ready
	// Version is the record format: 3 for a screen with its own headless
	// output (cage on wlroots' headless backend). A record without one was
	// written by a 0.1 or 0.2 hyprcage, whose screens were Hyprland outputs,
	// and is torn down the old way.
	Version int `json:"version,omitempty"`
	Width   int `json:"width"`
	Height  int `json:"height"`
	// WorkspaceApp only exists on legacy records: the Hyprland workspace
	// pinned to the screen's output.
	WorkspaceApp    int `json:"ws_app,omitempty"`
	WorkspaceMirror int `json:"ws_mirror"`
	// MirrorNote says why there is no mirror window when ws_mirror is 0,
	// so that an absent mirror is never something to go and investigate.
	MirrorNote   string `json:"mirror_note,omitempty"`
	Slice        string `json:"slice"`
	InnerDisplay string `json:"inner_display"`
	InnerX11     string `json:"inner_x11,omitempty"`
	CagePID      int    `json:"cage_pid,omitempty"`
	// CagePIDStart is cage's process start time, so that a reused pid is
	// never mistaken for the cage that died.
	CagePIDStart uint64 `json:"cage_pid_start,omitempty"`
	// RenderDevice is the DRM render node cage renders on, and Renderer the
	// wlroots renderer it runs (gles2 or pixman).
	RenderDevice   string `json:"render_device,omitempty"`
	RenderDeviceBy string `json:"render_device_by,omitempty"` // how it was chosen
	Renderer       string `json:"renderer,omitempty"`
	Owner          Owner  `json:"owner"`
	// DebugPort is the DevTools port of the screen's application on
	// 127.0.0.1, 0 when it has none.
	DebugPort int `json:"debug_port,omitempty"`
}

// Dir is the registry directory.
func Dir() string {
	if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" {
		return filepath.Join(rt, "hyprcage")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("hyprcage-%d", os.Getuid()))
}

// EnsureDir creates the registry directory (0700) and returns it.
func EnsureDir() (string, error) {
	d := Dir()
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}

func Path(name string) string      { return filepath.Join(Dir(), name+".json") }
func InnerPath(name string) string { return filepath.Join(Dir(), name+".inner") }
func LockPath() string             { return filepath.Join(Dir(), ".lock") }

// Save writes the record atomically.
func Save(s *Screen) error {
	if _, err := EnsureDir(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomic(Path(s.Name), data, 0o600)
}

// Load reads one record.
func Load(name string) (*Screen, error) {
	data, err := os.ReadFile(Path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var s Screen
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("registry %s: %w", name, err)
	}
	return &s, nil
}

// List returns every readable record.
func List() ([]*Screen, error) {
	entries, err := os.ReadDir(Dir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Screen
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		s, err := Load(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// Delete removes the record and the .inner file.
func Delete(name string) error {
	for _, p := range []string{Path(name), InnerPath(name)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Touch refreshes the heartbeat (cahier §5.2).
func Touch(name string) error {
	now := time.Now()
	return os.Chtimes(Path(name), now, now)
}

// Heartbeat returns the record's last heartbeat.
func Heartbeat(name string) (time.Time, error) {
	fi, err := os.Stat(Path(name))
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}

// ReadInner parses the KEY=VALUE file written by `hyprcage _holder`.
func ReadInner(name string) (map[string]string, error) {
	data, err := os.ReadFile(InnerPath(name))
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			m[k] = v
		}
	}
	return m, nil
}

// WriteAtomic writes data through a temporary file and a rename.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
