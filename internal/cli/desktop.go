package cli

import (
	"strconv"
	"strings"

	"github.com/hexadecimil/hyprcage/internal/desktop"
	"github.com/hexadecimil/hyprcage/internal/hypr"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

func openDesktop() (desktop.Desktop, error) {
	inst, err := hypr.Discover()
	if err != nil {
		return desktop.Desktop{}, screen.Errf(screen.CodeHyprland, "", "%v", err)
	}
	return desktop.Desktop{H: inst, D: inst.Driver()}, nil
}

func runDesktop(e *Env) int {
	fs := e.flags("desktop")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	usage := "usage: hyprcage desktop windows | focus <addr> | move <addr> <ws> | type <addr> <text> | key <addr> <combo>..."
	if fs.NArg() < 1 {
		return e.errorf(usage)
	}
	d, err := openDesktop()
	if err != nil {
		return e.fail(err)
	}
	a := fs.Args()
	switch {
	case a[0] == "windows" && len(a) == 1:
		wins, err := d.Windows()
		if err != nil {
			return e.fail(err)
		}
		return e.printJSON(wins)
	case a[0] == "focus" && len(a) == 2:
		err = d.Focus(a[1])
	case a[0] == "move" && len(a) == 3:
		ws, convErr := strconv.Atoi(a[2])
		if convErr != nil {
			return e.errorf("workspace: expected an integer, got %q", a[2])
		}
		err = d.Move(a[1], ws)
	case a[0] == "type" && len(a) >= 3:
		err = d.Type(a[1], strings.Join(a[2:], " "))
	case a[0] == "key" && len(a) >= 3:
		err = d.Key(a[1], a[2:])
	default:
		return e.errorf(usage)
	}
	if err != nil {
		return e.fail(err)
	}
	return ExitOK
}
