package cli

import (
	"errors"
	"fmt"

	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/screen"
	"github.com/hexadecimil/offstage/internal/session"
)

func runDestroy(e *Env) int {
	fs := e.flags("destroy")
	force := fs.Bool("force", false, "destroy even if another session owns the screen")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 1 {
		return e.errorf("usage: offstage destroy [--force] <screen>")
	}
	cfg, err := config.Load()
	if err != nil {
		return e.fail(err)
	}
	c, err := screen.Connect(cfg)
	if err != nil {
		return e.fail(err)
	}
	rec, err := registry.Load(fs.Arg(0))
	if errors.Is(err, registry.ErrNotFound) {
		return e.fail(&screen.Error{Code: screen.CodeNotFound, Msg: "no screen named " + fs.Arg(0), Hint: "offstage list --all"})
	}
	if err != nil {
		return e.fail(err)
	}
	if !*force {
		if err := screen.CheckOwner(rec, session.Current()); err != nil {
			return e.fail(err)
		}
	}
	if err := screen.Destroy(c, rec); err != nil {
		return e.fail(err)
	}
	fmt.Fprintf(e.Stdout, "destroyed %s\n", rec.Name)
	return ExitOK
}
