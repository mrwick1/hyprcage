package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hexadecimil/hyprcage/internal/setup"
)

// runSetup installs cage with the human's authorisation
// (sudo in a terminal, a polkit dialog otherwise), then the user unit of
// the notification daemon.
func runSetup(e *Env) int {
	fs := e.flags("setup")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	rep, err := setup.Run()
	note, nerr := setup.Notifyd()
	rep.Notifyd, err = note, errors.Join(err, nerr)
	if *asJSON {
		if err != nil {
			rep.Manual = err.Error()
		}
		return e.printJSON(rep)
	}
	switch {
	case err != nil:
		return e.fail(err)
	case len(rep.Missing) == 0:
		fmt.Fprintln(e.Stdout, "nothing to install: cage is present")
	default:
		fmt.Fprintf(e.Stdout, "installed %s via %s\n", strings.Join(rep.Installed, ", "), rep.Method)
	}
	fmt.Fprintln(e.Stdout, rep.Notifyd)
	return ExitOK
}
