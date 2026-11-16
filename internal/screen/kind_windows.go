package screen

import "github.com/hexadecimil/offstage/internal/session"

func pidAlive(pid int, start uint64) bool { return session.PIDAlive(pid, start) }

// groupGone is true: taskkill /T has ended the tree when it returns.
func groupGone(int) bool { return true }
