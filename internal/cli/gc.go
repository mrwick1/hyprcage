package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/lock"
	"github.com/hexadecimil/hyprcage/internal/record"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/sysd"
)

func runGC(e *Env) int {
	fs := e.flags("gc")
	sess := fs.String("session", "", "only the screens of this session id")
	after := fs.Duration("after", 0, "schedule the gc in a transient systemd unit after this delay")
	all := fs.Bool("all", false, "destroy every screen, alive or not")
	asJSON := fs.Bool("json", false, "JSON report")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if *after > 0 {
		exe, err := os.Executable()
		if err != nil {
			return e.fail(err)
		}
		cmd := []string{exe, "gc"}
		if *sess != "" {
			cmd = append(cmd, "--session", *sess)
		}
		if *all {
			cmd = append(cmd, "--all")
		}
		if err := sysd.ScheduleOnce(*after, "", cmd); err != nil {
			return e.fail(err)
		}
		return ExitOK
	}
	// The desktop recorder has no screen, so the screen gc does not stop it.
	var stopped bool
	var recErr error
	if _, err := record.Load(record.Desktop); err == nil && *all {
		_, recErr = record.Stop(record.Desktop)
		stopped = recErr == nil
	} else if *sess != "" {
		stopped, recErr = record.StopSession(*sess)
	}
	if recErr != nil {
		fmt.Fprintf(e.Stderr, "error: desktop recording: %v\n", recErr)
	} else if stopped && !*asJSON {
		fmt.Fprintln(e.Stdout, "stopped the desktop recording")
	}
	c, err := screen.Connect(config.Fallback())
	if err != nil {
		return e.fail(err)
	}
	rep, err := screen.GC(c, screen.GCOptions{All: *all, Session: *sess})
	if errors.Is(err, lock.ErrBusy) {
		return ExitOK // another gc just ran (cahier §5.4)
	}
	if err != nil {
		return e.fail(err)
	}
	if *asJSON {
		return e.printJSON(rep)
	}
	for _, n := range rep.Destroyed {
		fmt.Fprintf(e.Stdout, "destroyed %s\n", n)
	}
	for _, n := range rep.Orphans {
		fmt.Fprintf(e.Stdout, "cleaned orphan %s\n", n)
	}
	for _, n := range rep.Errors {
		fmt.Fprintf(e.Stderr, "error: %s\n", n)
	}
	if len(rep.Errors) > 0 {
		return ExitUsage
	}
	return ExitOK
}
