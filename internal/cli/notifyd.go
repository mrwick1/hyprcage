package cli

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/hexadecimil/hyprcage/internal/notifyd"
)

// runNotifyd keeps the desktop notifications in notifyd.Path() until
// SIGTERM or SIGINT. The user unit hyprcage-notifyd.service runs it.
func runNotifyd(e *Env) int {
	if err := e.parse(e.flags("notifyd")); err != nil {
		return ExitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := notifyd.Run(ctx); err != nil {
		return e.fail(err)
	}
	return ExitOK
}
