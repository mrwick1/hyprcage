package hypr

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const requestTimeout = 5 * time.Second

// Request sends one raw command to Hyprland's command socket, the very
// strings hyprctl sends ("j/monitors", "keyword …", "dispatch …", "eval …",
// "output create headless NAME"), and returns the trimmed reply.
func (i *Instance) Request(cmd string) (string, error) {
	conn, err := net.DialTimeout("unix", i.socket(".socket.sock"), requestTimeout)
	if err != nil {
		return "", fmt.Errorf("hyprland unreachable: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(requestTimeout))
	if _, err := io.WriteString(conn, cmd); err != nil {
		return "", fmt.Errorf("hyprland request: %w", err)
	}
	out, err := io.ReadAll(conn)
	if err != nil {
		return "", fmt.Errorf("hyprland reply: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Command sends a command whose only acceptable reply is "ok" (keyword,
// dispatch, output).
func (i *Instance) Command(cmd string) error {
	out, err := i.Request(cmd)
	if err != nil {
		return err
	}
	if out != "ok" {
		return fmt.Errorf("hyprland refused %q: %s", cmd, out)
	}
	return nil
}

// RequestJSON sends a query with the "j/" prefix and decodes the reply.
func (i *Instance) RequestJSON(cmd string, v any) error {
	out, err := i.Request("j/" + cmd)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(out), v); err != nil {
		return fmt.Errorf("hyprland %q: cannot decode reply: %w (%.120s)", cmd, err, out)
	}
	return nil
}

// Batch runs several commands in one round trip ([[BATCH]] syntax).
func (i *Instance) Batch(cmds ...string) (string, error) {
	return i.Request("[[BATCH]]" + strings.Join(cmds, ";"))
}

// WorkspaceRef is the {id, name} pair Hyprland embeds in monitors and clients.
type WorkspaceRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Monitor is an entry of `hyprctl -j monitors`.
type Monitor struct {
	ID              int          `json:"id"`
	Name            string       `json:"name"`
	Description     string       `json:"description"`
	Width           int          `json:"width"`
	Height          int          `json:"height"`
	X               int          `json:"x"`
	Y               int          `json:"y"`
	Scale           float64      `json:"scale"`
	Focused         bool         `json:"focused"`
	Disabled        bool         `json:"disabled"`
	ActiveWorkspace WorkspaceRef `json:"activeWorkspace"`
	// Reserved is the area taken by layer surfaces with an exclusive zone (a
	// bar), as left, top, right, bottom.
	Reserved [4]int `json:"reserved"`
}

// Workspace is an entry of `hyprctl -j workspaces`.
type Workspace struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Monitor string `json:"monitor"`
	Windows int    `json:"windows"`
}

// FullscreenState decodes the "fullscreen" field, a bool in old Hyprland
// versions and a mode number (0 none, 1 maximized, 2 fullscreen) in new ones.
type FullscreenState int

func (f *FullscreenState) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	switch s {
	case "true":
		*f = 2
		return nil
	case "false", "null":
		*f = 0
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("fullscreen: unexpected %s", s)
	}
	*f = FullscreenState(n)
	return nil
}

// Client is an entry of `hyprctl -j clients` (a window).
type Client struct {
	Address    string          `json:"address"`
	PID        int             `json:"pid"`
	Class      string          `json:"class"`
	Title      string          `json:"title"`
	Workspace  WorkspaceRef    `json:"workspace"`
	Monitor    int             `json:"monitor"`
	Fullscreen FullscreenState `json:"fullscreen"`
	Size       [2]int          `json:"size"`
	At         [2]int          `json:"at"`
}

// CursorPos is the reply of `hyprctl -j cursorpos`.
type CursorPos struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func (i *Instance) Monitors() ([]Monitor, error) {
	var v []Monitor
	return v, i.RequestJSON("monitors", &v)
}

func (i *Instance) Workspaces() ([]Workspace, error) {
	var v []Workspace
	return v, i.RequestJSON("workspaces", &v)
}

func (i *Instance) Clients() ([]Client, error) {
	var v []Client
	return v, i.RequestJSON("clients", &v)
}

func (i *Instance) CursorPos() (CursorPos, error) {
	var v CursorPos
	return v, i.RequestJSON("cursorpos", &v)
}

// WaylandDisplay returns the name of the instance's Wayland socket (e.g.
// "wayland-1"), from `hyprctl instances`, else from WAYLAND_DISPLAY.
func (i *Instance) WaylandDisplay() (string, error) {
	// hyprland.lock holds the compositor pid, then the Wayland socket name.
	// The socket does not answer "instances": hyprctl computes it itself.
	if data, err := os.ReadFile(filepath.Join(i.Dir, "hyprland.lock")); err == nil {
		lines := strings.Split(string(data), "\n")
		if len(lines) > 1 {
			name := strings.TrimSpace(lines[1])
			if rt, err := RuntimeDir(); err == nil && name != "" && !strings.Contains(name, "/") {
				if fi, err := os.Stat(filepath.Join(rt, name)); err == nil && fi.Mode()&os.ModeSocket != 0 {
					return name, nil
				}
			}
		}
	}
	var list []struct {
		Instance string `json:"instance"`
		WLSocket string `json:"wl_socket"`
	}
	if err := i.RequestJSON("instances", &list); err == nil {
		for _, e := range list {
			if e.Instance == i.Signature && e.WLSocket != "" {
				return e.WLSocket, nil
			}
		}
	}
	if d := os.Getenv("WAYLAND_DISPLAY"); d != "" {
		return d, nil
	}
	return "", errors.New("cannot find the Wayland socket of this Hyprland instance")
}

// Version returns the tag (e.g. "v0.56.2") or, failing that, the version string.
func (i *Instance) Version() (string, error) {
	var v struct {
		Tag     string `json:"tag"`
		Version string `json:"version"`
		Commit  string `json:"commit"`
	}
	if err := i.RequestJSON("version", &v); err != nil {
		return "", err
	}
	if v.Tag != "" {
		return v.Tag, nil
	}
	if v.Version != "" {
		return v.Version, nil
	}
	return v.Commit, nil
}
