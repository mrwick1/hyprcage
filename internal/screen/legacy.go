package screen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hexadecimil/offstage/internal/hypr"
	"github.com/hexadecimil/offstage/internal/lock"
	"github.com/hexadecimil/offstage/internal/notify"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/sysd"
)

// Before 0.3 a screen was a Hyprland output with cage in a window on it.
// This file tears such screens down: a record without a version, written
// by an older offstage still in memory when the binary was updated, and an
// output carrying the prefix without any record. Removing an output makes
// Hyprland migrate its workspaces and can warp the cursor, so the human's
// state is captured before and put back after, for as long as Hyprland and
// the desktop's own scripts keep reacting.

// humanState is what `output remove` may disturb.
type humanState struct {
	cursor  hypr.CursorPos
	active  map[string]int // real monitor → active workspace id
	focused string
}

func captureHuman(h *hypr.Instance, prefix string, known map[string]bool) *humanState {
	st := &humanState{active: map[string]int{}}
	st.cursor, _ = h.CursorPos()
	mons, err := h.Monitors()
	if err != nil {
		return st
	}
	for _, m := range mons {
		if isAgentOutput(m.Name, prefix, known) {
			continue
		}
		st.active[m.Name] = m.ActiveWorkspace.ID
		if m.Focused {
			st.focused = m.Name
		}
	}
	return st
}

func restoreHuman(h *hypr.Instance, d hypr.ConfigDriver, before *humanState, prefix string, known map[string]bool, cursorAlone bool) bool {
	after := captureHuman(h, prefix, known)
	var cmds []string
	var moved []string
	for name, ws := range before.active {
		if after.active[name] != ws {
			cmds = append(cmds, d.FocusMonitorCmd(name), d.WorkspaceCmd(ws))
			moved = append(moved, fmt.Sprintf("%s: workspace %d -> %d", name, ws, after.active[name]))
		}
	}
	if after.cursor != before.cursor {
		moved = append(moved, fmt.Sprintf("cursor: %d,%d -> %d,%d", before.cursor.X, before.cursor.Y, after.cursor.X, after.cursor.Y))
	}
	if after.focused != before.focused {
		moved = append(moved, fmt.Sprintf("focus: %s -> %s", before.focused, after.focused))
	}
	if len(moved) > 0 {
		traceRestore(strings.Join(moved, "; "), len(cmds) > 0 || cursorAlone)
	}
	if len(cmds) > 0 && before.focused != "" {
		cmds = append(cmds, d.FocusMonitorCmd(before.focused))
	}
	if len(cmds) > 0 || (cursorAlone && after.cursor != before.cursor) {
		cmds = append(cmds, d.MoveCursorCmd(before.cursor.X, before.cursor.Y))
	}
	if len(cmds) > 0 {
		_, _ = h.Batch(cmds...)
		return true
	}
	return false
}

// removeOutputRestoring removes a legacy output and keeps the human's state
// in place while Hyprland and the desktop react.
func removeOutputRestoring(c *Ctx, name string) error {
	if c.Hypr == nil {
		return fmt.Errorf("no Hyprland to remove output %s from", name)
	}
	known := knownScreens()
	before := captureHuman(c.Hypr, c.Cfg.OutputPrefix, known)
	err := c.Hypr.RemoveOutput(name)
	waitOutputGone(c.Hypr, name, time.Second)
	restoreHuman(c.Hypr, c.Driver, before, c.Cfg.OutputPrefix, known, true)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(150 * time.Millisecond)
		restoreHuman(c.Hypr, c.Driver, before, c.Cfg.OutputPrefix, known, false)
	}
	return err
}

// traceRestore appends what an output removal moved, and whether it was put
// back, to ~/.local/state/offstage/log/restore.log.
func traceRestore(what string, restored bool) {
	f, err := os.OpenFile(filepath.Join(LogDir(), "restore.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	verb := "seen, left alone"
	if restored {
		verb = "restored"
	}
	fmt.Fprintf(f, "%s %s (%s)\n", time.Now().Format(time.RFC3339Nano), what, verb)
}

func waitOutputGone(h *hypr.Instance, name string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for hasMonitor(h, name) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
}

func hasMonitor(h *hypr.Instance, name string) bool {
	if h == nil {
		return false
	}
	mons, err := h.Monitors()
	if err != nil {
		return false
	}
	for _, m := range mons {
		if m.Name == name {
			return true
		}
	}
	return false
}

// destroyLegacy tears down a screen recorded by a offstage before 0.3.
func destroyLegacy(c *Ctx, rec *registry.Screen) error {
	release, err := lock.Acquire(30 * time.Second)
	if err != nil {
		return err
	}
	defer release()

	CloseConn(rec.Name)
	useSystemd := sysd.Available()
	if useSystemd {
		_ = sysd.StopUnit(rec.Slice)
	} else {
		killLegacyPIDs(c.Hypr, rec, syscall.SIGTERM)
	}
	if !waitNoLegacyClients(c.Hypr, rec, 3*time.Second) {
		killLegacyPIDs(c.Hypr, rec, syscall.SIGKILL)
		waitNoLegacyClients(c.Hypr, rec, time.Second)
	}
	var removeErr error
	if hasMonitor(c.Hypr, rec.Name) {
		removeErr = removeOutputRestoring(c, rec.Name)
	}
	if useSystemd {
		sysd.DisarmTimer(sysd.GCTimerName(rec.Name))
		sysd.ResetFailed(rec.Slice)
	}
	if err := registry.Delete(rec.Name); err != nil {
		return err
	}
	if c.Cfg.Notify {
		notify.Send("Agent screen closed", rec.Name)
	}
	return removeErr
}

// legacyPIDs lists the windows of a legacy screen: everything on its app
// workspace, and only the mirror window on the (shared) mirror workspace.
func legacyPIDs(h *hypr.Instance, rec *registry.Screen) []int {
	if h == nil {
		return nil
	}
	cls, err := h.Clients()
	if err != nil {
		return nil
	}
	var pids []int
	for _, cl := range cls {
		onApp := cl.Workspace.ID == rec.WorkspaceApp
		isMirror := rec.WorkspaceMirror > 0 && cl.Workspace.ID == rec.WorkspaceMirror &&
			cl.Title == "Wayland Output Mirror for "+rec.Name
		if onApp || isMirror {
			pids = append(pids, cl.PID)
		}
	}
	return pids
}

func killLegacyPIDs(h *hypr.Instance, rec *registry.Screen, sig syscall.Signal) {
	for _, pid := range legacyPIDs(h, rec) {
		if pid > 0 {
			_ = syscall.Kill(pid, sig)
		}
	}
}

func waitNoLegacyClients(h *hypr.Instance, rec *registry.Screen, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if len(legacyPIDs(h, rec)) == 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
