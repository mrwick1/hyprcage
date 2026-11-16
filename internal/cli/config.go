package cli

import (
	"fmt"

	"github.com/hexadecimil/offstage/internal/config"
)

// runConfig puts the configuration file in place, every key at its default,
// and says where it is. A file already there is left as it is, so the
// command is safe to run on every installation.
func runConfig(e *Env) int {
	fs := e.flags("config")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	path, written, err := config.WriteDefault()
	if err != nil {
		return e.fail(err)
	}
	if *asJSON {
		return e.printJSON(map[string]any{"path": path, "written": written})
	}
	if written {
		fmt.Fprintf(e.Stdout, "wrote %s with the defaults, edit it to change them\n", path)
	} else {
		fmt.Fprintf(e.Stdout, "%s is already there\n", path)
	}
	return ExitOK
}
