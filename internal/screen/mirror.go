package screen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hexadecimil/hyprcage/internal/hypr"
	"github.com/hexadecimil/hyprcage/internal/proc"
	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/session"
	"github.com/hexadecimil/hyprcage/internal/shellq"
	"github.com/hexadecimil/hyprcage/internal/sysd"
)

var mirrorSeq int

// MirrorState is what a running mirror publishes in <name>.mirror: its pid
// and, once known, the workspace its window landed on.
type MirrorState struct {
	PID       int
	PIDStart  uint64
	Workspace int
}

// MirrorPath is the mirror's sidecar file.
func MirrorPath(name string) string { return filepath.Join(registry.Dir(), name+".mirror") }

// ReadMirror reads the sidecar and says whether the mirror it names is
// alive. A file whose pid is dead is a leftover and reads as no mirror.
func ReadMirror(name string) (MirrorState, bool) {
	data, err := os.ReadFile(MirrorPath(name))
	if err != nil {
		return MirrorState{}, false
	}
	var st MirrorState
	for _, line := range strings.Split(string(data), "\n") {
		k, v, _ := strings.Cut(line, "=")
		n, _ := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		switch k {
		case "PID":
			st.PID = int(n)
		case "PID_START":
			st.PIDStart = n
		case "WORKSPACE":
			st.Workspace = int(n)
		}
	}
	if st.PID == 0 || !session.PIDAlive(st.PID, st.PIDStart) {
		return st, false
	}
	return st, true
}

// WriteMirror publishes the sidecar, from the mirror process.
func WriteMirror(name string, st MirrorState) error {
	data := fmt.Sprintf("PID=%d\nPID_START=%d\nWORKSPACE=%d\n", st.PID, st.PIDStart, st.Workspace)
	return registry.WriteAtomic(MirrorPath(name), []byte(data), 0o600)
}

// StartMirror opens the human's mirror window for a screen. Under Hyprland
// the mirror is launched by Hyprland itself with the rules that put it on
// the screen's workspace, silently and without focus, and in the screen's
// slice all the same. A screen created without a mirror has no workspace
// yet: one is chosen now and recorded, and when the range is all taken the
// mirror is refused rather than opened on whatever the human is looking
// at. Without Hyprland it is launched directly, wherever the compositor
// puts new windows. One mirror per screen: a second request while one runs
// is a no-op.
func StartMirror(c *Ctx, rec *registry.Screen, exe string, useSystemd bool) error {
	if err := RefuseHeadless(rec, "mirror"); err != nil {
		return err
	}
	if _, alive := ReadMirror(rec.Name); alive {
		return nil
	}
	if c.Hypr != nil && rec.WorkspaceMirror == 0 {
		ws := mirrorWorkspace(c, rec.Owner)
		if ws == 0 {
			return errf(CodeLimit, "close a mirror with `hyprcage mirror -close <screen>`",
				"no workspace left for the mirror: %d to %d are all taken", c.Cfg.MirrorMin, c.Cfg.MirrorMax)
		}
		rec.WorkspaceMirror, rec.MirrorNote = ws, ""
		if err := registry.Save(rec); err != nil {
			return err
		}
	}
	mirrorSeq++
	cmd := []string{"env", "HYPRCAGE_SCREEN=" + rec.Name, exe, "_mirror", rec.Name}
	argv := cmd
	if useSystemd {
		argv = sysd.ScopeArgs(rec.Name, fmt.Sprintf("mirror-%d-%d", os.Getpid(), mirrorSeq), cmd)
	}
	if c.Hypr != nil && rec.WorkspaceMirror > 0 {
		rules := hypr.ExecRules{Workspace: fmt.Sprintf("%d silent", rec.WorkspaceMirror), NoInitialFocus: true, NoAnim: true}
		if err := c.Driver.Exec(shellq.Join(argv), rules); err != nil {
			return err
		}
	} else {
		p := exec.Command(argv[0], argv[1:]...)
		proc.Detach(p)
		if err := p.Start(); err != nil {
			return err
		}
		go func() { _ = p.Wait() }()
	}
	// Best effort: give the window a moment to appear so that the caller's
	// answer describes it.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, alive := ReadMirror(rec.Name); alive {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// StopMirror closes the mirror of a screen, if one runs, and gives it a
// moment to go down on its own: it takes its window with it and removes its
// own sidecar.
func StopMirror(name string) {
	st, alive := ReadMirror(name)
	if alive {
		_ = proc.Kill(st.PID, syscall.SIGTERM)
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if !session.PIDAlive(st.PID, st.PIDStart) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	_ = os.Remove(MirrorPath(name))
}
