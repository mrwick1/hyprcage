package record

import (
	"context"
	"syscall"

	"github.com/hexadecimil/offstage/internal/proc"
)

// requestStop makes the recorder end: SIGTERM ends its context.
func requestStop(s State) { _ = proc.Kill(s.PID, syscall.SIGTERM) }

// watchStop has nothing to add: the signal already ends ctx.
func watchStop(ctx context.Context, target string) context.Context { return ctx }
