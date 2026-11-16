package screen

import (
	"time"

	"github.com/hexadecimil/hyprcage/internal/proc"
	"github.com/hexadecimil/hyprcage/internal/registry"
)

// The kinds of screen. A cage screen has the empty Kind in its record.
const (
	KindCage     = "cage"
	KindHeadless = "headless"
)

// RefuseHeadless answers CodeUnsupportedKind when rec is a headless screen,
// for a tool that needs a cage: what names the tool.
func RefuseHeadless(rec *registry.Screen, what string) error {
	if rec.Kind != KindHeadless {
		return nil
	}
	return errf(CodeUnsupportedKind, "a headless screen is one browser: use browser_open, snapshot, act, click and type",
		"%s does not work on headless screen %s", what, rec.Name)
}

// headlessAlive reports whether the Chrome of a headless screen still runs.
func headlessAlive(rec *registry.Screen) bool {
	return pidAlive(rec.DebugPID, rec.DebugPIDStart) && !zombie(rec.DebugPID)
}

// destroyHeadless ends the Chrome of a headless screen and its children,
// then removes its profile. The caller holds the lock.
func destroyHeadless(rec *registry.Screen) error {
	if headlessAlive(rec) {
		proc.KillTree(rec.DebugPID)
		for deadline := time.Now().Add(3 * time.Second); headlessAlive(rec) && time.Now().Before(deadline); {
			time.Sleep(50 * time.Millisecond)
		}
	} else if rec.DebugPID > 0 && proc.Kill(rec.DebugPID, 0) != nil {
		// Chrome died, but its children may live on and write to the
		// profile. No process has its pid, so the pid still names only
		// Chrome's group, which the system does not reuse while it has members.
		proc.KillTree(rec.DebugPID)
	}
	// Chrome may hold files for a moment after it is killed.
	for i := 0; i < 20; i++ {
		removeProfiles(rec.Name)
		if len(profileDirs(rec.Name)) == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return registry.Delete(rec.Name)
}
