// Package hypr talks to Hyprland over its IPC sockets: commands and JSON
// queries on .socket.sock, events on .socket2.sock, and the two configuration
// drivers (classic keywords, Lua) behind one interface. No Hyprland library is
// linked, so a Hyprland update never requires a rebuild (cahier N6).
package hypr

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Instance is a running Hyprland instance, identified by its signature: the
// name of its directory under $XDG_RUNTIME_DIR/hypr.
type Instance struct {
	Signature string
	Dir       string
}

// RuntimeDir returns $XDG_RUNTIME_DIR or an error when it is unset.
func RuntimeDir() (string, error) {
	d := os.Getenv("XDG_RUNTIME_DIR")
	if d == "" {
		return "", errors.New("XDG_RUNTIME_DIR is not set")
	}
	return d, nil
}

// Discover finds the Hyprland instance to talk to: HYPRLAND_INSTANCE_SIGNATURE
// when set and alive, otherwise the most recent live instance under
// $XDG_RUNTIME_DIR/hypr (cahier N12: works from SSH or a transient unit).
func Discover() (*Instance, error) {
	rt, err := RuntimeDir()
	if err != nil {
		return nil, err
	}
	base := filepath.Join(rt, "hypr")
	if sig := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE"); sig != "" {
		inst := &Instance{Signature: sig, Dir: filepath.Join(base, sig)}
		if inst.Alive() {
			return inst, nil
		}
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, fmt.Errorf("no Hyprland instance directory at %s: %w", base, err)
	}
	var candidates []*Instance
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		inst := &Instance{Signature: e.Name(), Dir: filepath.Join(base, e.Name())}
		if inst.Alive() {
			candidates = append(candidates, inst)
		}
	}
	if len(candidates) == 0 {
		return nil, errors.New("no live Hyprland instance found (is Hyprland running?)")
	}
	sort.Slice(candidates, func(i, j int) bool {
		return modTime(candidates[i].Dir) > modTime(candidates[j].Dir)
	})
	return candidates[0], nil
}

func modTime(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.ModTime().UnixNano()
}

func (i *Instance) socket(name string) string {
	return filepath.Join(i.Dir, name)
}

// Alive reports whether the instance answers on its command socket.
func (i *Instance) Alive() bool {
	if _, err := os.Stat(i.socket(".socket.sock")); err != nil {
		return false
	}
	_, err := i.Request("version")
	return err == nil
}
