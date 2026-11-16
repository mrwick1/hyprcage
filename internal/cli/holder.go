package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/screen"
	"github.com/hexadecimil/offstage/internal/wl"
)

// runHolder is cage's child process (cahier §4.1): it publishes cage's
// sockets in <registry>/<screen>.inner and then blocks. cage exits when its
// child exits, so this process is the screen's point of voluntary death.
func runHolder(e *Env) int {
	if len(e.Args) != 1 {
		return e.errorf("usage: offstage _holder <screen>")
	}
	name := e.Args[0]
	if err := screen.ValidateName(name); err != nil {
		return e.errorf("%v", err)
	}
	disp := os.Getenv("WAYLAND_DISPLAY")
	if disp == "" {
		return e.errorf("_holder: WAYLAND_DISPLAY is empty; it must run as cage's child")
	}
	if _, err := registry.EnsureDir(); err != nil {
		return e.errorf("%v", err)
	}
	// cage is this process's parent: its pid is what tells a live screen
	// from a dead one, now that cage has no window in Hyprland.
	data := fmt.Sprintf("WAYLAND_DISPLAY=%s\nDISPLAY=%s\nCAGE_PID=%d\nHOLDER_PID=%d\n", disp, os.Getenv("DISPLAY"), os.Getppid(), os.Getpid())
	if err := registry.WriteAtomic(registry.InnerPath(name), []byte(data), 0o600); err != nil {
		return e.errorf("%v", err)
	}
	// The seat keeps a pointer and a keyboard for as long as the screen
	// lives. Virtual devices belong to the client that made them, and every
	// offstage command is a client that comes and goes: without this, the
	// seat's capabilities would appear and vanish under the applications,
	// which re-bind their input on every change and drop the keys sent in
	// between. Holding them here makes the seat steady.
	if cl, err := wl.Connect(disp); err == nil {
		if err := cl.EnsureInput(); err != nil {
			cl.Close()
		} else {
			defer cl.Close()
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	parent := os.Getppid()
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-sig:
			return ExitOK
		case <-tick.C:
			if os.Getppid() != parent {
				return ExitOK // cage is gone: nothing left to hold
			}
		}
	}
}
