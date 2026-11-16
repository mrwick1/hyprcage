package cli

import (
	"fmt"
	"os"

	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/mirror"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/screen"
	"github.com/hexadecimil/offstage/internal/sysd"
)

// runMirror is the mirror process (offstage _mirror <screen>): it shows the
// screen in a window on the human's compositor and exits when the window is
// closed or a side goes away. It is normally launched by StartMirror, on a
// workspace chosen by the exec rules, but it can be run by hand too.
func runMirror(e *Env) int {
	if len(e.Args) != 1 {
		return e.errorf("usage: offstage _mirror <screen>")
	}
	name := e.Args[0]
	if err := screen.ValidateName(name); err != nil {
		return e.errorf("%v", err)
	}
	rec, err := registry.Load(name)
	if err != nil {
		return e.errorf("%v", err)
	}
	logf := func(format string, args ...any) {
		if f := screen.MirrorLog(name); f != nil {
			fmt.Fprintf(f, format+"\n", args...)
			f.Close()
		}
	}
	cfg := config.Fallback()
	return mirror.Run(rec, mirror.Options{
		FPS: cfg.MirrorFPS,
		// Alone on its workspace the window takes it whole; sharing it with
		// the session's other mirrors, it tiles with them.
		Fullscreen: cfg.MirrorGroup == "screen",
		Workspace:  rec.WorkspaceMirror,
		Log:        logf,
	})
}

// runMirrorOpen is the user-facing `offstage mirror <screen>`: open (default)
// or close (-close) the mirror of a screen without touching the screen.
func runMirrorOpen(e *Env) int {
	fs := e.flags("mirror")
	closeIt := fs.Bool("close", false, "close the mirror instead of opening it")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 1 {
		return e.errorf("usage: offstage mirror [-close] <screen>")
	}
	cfg, err := config.Load()
	if err != nil {
		return e.fail(err)
	}
	name := screen.Prefixed(fs.Arg(0), cfg.OutputPrefix)
	c, err := screen.Connect(cfg)
	if err != nil {
		return e.fail(err)
	}
	rec, err := screen.Get(c, name)
	if err != nil {
		return e.fail(err)
	}
	if *closeIt {
		screen.StopMirror(name)
		return ExitOK
	}
	exe, err := os.Executable()
	if err != nil {
		return e.fail(err)
	}
	if err := screen.StartMirror(c, rec, exe, sysd.Available()); err != nil {
		return e.fail(err)
	}
	return ExitOK
}
