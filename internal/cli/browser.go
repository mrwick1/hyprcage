package cli

import (
	"github.com/hexadecimil/hyprcage/internal/browser"
	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/session"
)

func runBrowser(e *Env) int {
	fs := e.flags("browser")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 2 {
		return e.errorf("usage: hyprcage browser [screen] [url]")
	}
	cfg, err := config.Load()
	if err != nil {
		return e.fail(err)
	}
	c, err := screen.Connect(cfg)
	if err != nil {
		return e.fail(err)
	}
	rec, err := screen.Resolve(c, fs.Arg(0), session.Current())
	if err != nil {
		return e.fail(err)
	}
	info, err := browser.Open(c, rec, fs.Arg(1))
	if err != nil {
		return e.fail(err)
	}
	return e.printJSON(info)
}
