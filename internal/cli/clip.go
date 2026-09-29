package cli

import (
	"fmt"

	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/session"
)

func runClip(e *Env) int {
	fs := e.flags("clip")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	usage := "usage: hyprcage clip get [screen] | clip set [screen] <text>"
	if fs.NArg() < 1 {
		return e.errorf(usage)
	}
	cfg, err := config.Load()
	if err != nil {
		return e.fail(err)
	}
	c, err := screen.Connect(cfg)
	if err != nil {
		return e.fail(err)
	}
	switch {
	case fs.Arg(0) == "get" && fs.NArg() <= 2:
		rec, err := screen.Resolve(c, fs.Arg(1), session.Current())
		if err != nil {
			return e.fail(err)
		}
		text, err := screen.ClipboardGet(rec)
		if err != nil {
			return e.fail(err)
		}
		fmt.Fprint(e.Stdout, text)
		return ExitOK
	case fs.Arg(0) == "set" && (fs.NArg() == 2 || fs.NArg() == 3):
		name, text := "", fs.Arg(1)
		if fs.NArg() == 3 {
			name, text = fs.Arg(1), fs.Arg(2)
		}
		rec, err := screen.Resolve(c, name, session.Current())
		if err != nil {
			return e.fail(err)
		}
		if err := screen.ClipboardSet(rec, text); err != nil {
			return e.fail(err)
		}
		return ExitOK
	}
	return e.errorf(usage)
}
