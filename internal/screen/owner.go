package screen

import (
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/session"
)

// CheckOwner enforces cahier N4: a session only touches its own screens.
// Two known session ids must match; otherwise the pid (and its start time)
// must match.
func CheckOwner(rec *registry.Screen, id session.Identity) error {
	if id.SessionID != "" && rec.Owner.SessionID != "" {
		if rec.Owner.SessionID == id.SessionID {
			return nil
		}
	} else if rec.Owner.PID == id.PID && (rec.Owner.PIDStart == 0 || id.PIDStart == 0 || rec.Owner.PIDStart == id.PIDStart) {
		return nil
	}
	return errf(CodeNotOwner, "from a terminal: offstage destroy --force "+rec.Name, "screen %s belongs to another session", rec.Name)
}

// sameOwner is CheckOwner's rule between two records.
func sameOwner(a, b registry.Owner) bool {
	if a.SessionID != "" && b.SessionID != "" {
		return a.SessionID == b.SessionID
	}
	return a.PID == b.PID && (a.PIDStart == 0 || b.PIDStart == 0 || a.PIDStart == b.PIDStart)
}
