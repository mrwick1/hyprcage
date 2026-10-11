package cli

import (
	"fmt"

	"github.com/hexadecimil/hyprcage/internal/browser"
)

func runProfile(e *Env) int {
	fs := e.flags("profile")
	asJSON := fs.Bool("json", false, "JSON output (list)")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	switch fs.Arg(0) {
	case "list":
		if fs.NArg() != 1 {
			return e.errorf("usage: hyprcage profile list [--json]")
		}
		ps, err := browser.ListProfiles()
		if err != nil {
			return e.fail(err)
		}
		if *asJSON {
			if ps == nil {
				ps = []browser.ProfileInfo{}
			}
			return e.printJSON(ps)
		}
		for _, p := range ps {
			state := ""
			if p.InUse {
				state = "  (in use)"
			}
			fmt.Fprintf(e.Stdout, "%s  %s%s\n", p.Name, p.Path, state)
		}
	case "rm":
		if fs.NArg() != 2 {
			return e.errorf("usage: hyprcage profile rm <name>")
		}
		if err := browser.RemoveProfile(fs.Arg(1)); err != nil {
			return e.fail(err)
		}
	default:
		return e.errorf("usage: hyprcage profile list|rm <name>")
	}
	return ExitOK
}
