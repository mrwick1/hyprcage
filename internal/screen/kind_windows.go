package screen

import "github.com/hexadecimil/hyprcage/internal/session"

func pidAlive(pid int, start uint64) bool { return session.PIDAlive(pid, start) }
