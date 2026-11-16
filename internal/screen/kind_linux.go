package screen

import (
	"github.com/hexadecimil/offstage/internal/proc"
	"github.com/hexadecimil/offstage/internal/session"
)

func pidAlive(pid int, start uint64) bool { return session.PIDAlive(pid, start) }

// groupGone reports whether no process of group pgid is left. SIGKILL acts
// at once, but the group still lists a process until the system has reaped it.
func groupGone(pgid int) bool { return proc.Kill(-pgid, 0) != nil }
