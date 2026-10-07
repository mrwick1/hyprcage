package cli

import (
	"context"
	"fmt"
	"image"
	"os"
	"os/signal"
	"syscall"

	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/desktop"
	"github.com/hexadecimil/hyprcage/internal/hypr"
	"github.com/hexadecimil/hyprcage/internal/record"
	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/session"
)

// recordTarget resolves "" or a screen name to a screen, and keeps "desktop".
func recordTarget(name string) (string, bool, error) {
	if name == record.Desktop {
		return record.Desktop, false, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return "", false, err
	}
	c, err := screen.Connect(cfg)
	if err != nil {
		return "", false, err
	}
	rec, err := screen.Resolve(c, name, session.Current())
	if err != nil {
		return "", false, err
	}
	return rec.Name, true, nil
}

func runRecord(e *Env) int {
	fs := e.flags("record")
	asJSON := fs.Bool("json", false, "JSON output")
	cropArg := fs.String("crop", "", "x,y,w,h part of the screen to record; empty records it whole")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	usage := "usage: hyprcage record start|stop [screen|desktop] [--crop x,y,w,h] [--json] | record status [--json]"
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return e.errorf(usage)
	}
	if fs.Arg(0) == "status" {
		list, err := record.List()
		if err != nil {
			return e.fail(err)
		}
		if *asJSON {
			return e.printJSON(list)
		}
		for _, s := range list {
			fmt.Fprintf(e.Stdout, "%-12s pid=%d  %s\n", s.Target, s.PID, s.Path)
		}
		return ExitOK
	}
	target, isScreen, err := recordTarget(fs.Arg(1))
	if err != nil {
		return e.fail(err)
	}
	var s record.State
	switch fs.Arg(0) {
	case "start":
		cfg, err := config.Load()
		if err != nil {
			return e.fail(err)
		}
		exe, err := os.Executable()
		if err != nil {
			return e.fail(screen.Errf(screen.CodeCapture, "", "%v", err))
		}
		var crop image.Rectangle
		if *cropArg != "" {
			if crop, err = record.ParseRect(*cropArg); err != nil {
				return e.errorf("%v", err)
			}
		}
		s, err = record.Start(exe, target, isScreen, session.Current().Owner(), cfg, crop)
		if err != nil {
			return e.fail(err)
		}
	case "stop":
		if s, err = record.Load(target); err != nil {
			return e.fail(err)
		}
		if err = record.CheckOwner(s, session.Current()); err != nil {
			return e.fail(err)
		}
		s, err = record.Stop(target)
		if err != nil {
			return e.fail(err)
		}
	default:
		return e.errorf(usage)
	}
	if *asJSON {
		return e.printJSON(s)
	}
	fmt.Fprintln(e.Stdout, s.Path)
	return ExitOK
}

// runRecordChild is `hyprcage _record <target> <out>`.
func runRecordChild(e *Env) int {
	if len(e.Args) != 2 && len(e.Args) != 3 {
		return e.errorf("usage: hyprcage _record <target> <out> [x,y,w,h]")
	}
	target, out := e.Args[0], e.Args[1]
	var crop image.Rectangle
	if len(e.Args) == 3 {
		var err error
		if crop, err = record.ParseRect(e.Args[2]); err != nil {
			return e.fail(err)
		}
	}
	cfg := config.Fallback()
	var display string
	if target == record.Desktop {
		found, err := hypr.Discover()
		if err != nil {
			return e.fail(err)
		}
		inst, err := desktop.Instance(found)
		if err != nil {
			return e.fail(err)
		}
		if display, err = inst.WaylandDisplay(); err != nil {
			return e.fail(err)
		}
	} else {
		rec, err := registry.Load(target)
		if err != nil {
			return e.fail(err)
		}
		display = rec.InnerDisplay
		if display == "" {
			inner, err := registry.ReadInner(target)
			if err != nil {
				return e.fail(err)
			}
			display = inner["WAYLAND_DISPLAY"]
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := record.RunChild(ctx, target, display, out, crop, cfg); err != nil {
		return e.fail(err)
	}
	return ExitOK
}
