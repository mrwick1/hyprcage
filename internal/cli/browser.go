package cli

import (
	"github.com/hexadecimil/offstage/internal/bare"
	"os/exec"
)

// runBrowser serves the bundled browser protocol on stdin/stdout.
func runBrowser(e *Env) int {
	if len(e.Args) != 0 {
		return e.errorf("usage: offstage browser (Bare MCP on stdin/stdout); use browser_open through offstage mcp to open a page")
	}
	bin, err := bare.Find()
	if err != nil {
		return e.fail(err)
	}
	cmd := exec.Command(bin)
	cmd.Env = bare.Environment("")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = e.Stdin, e.Stdout, e.Stderr
	if err := cmd.Run(); err != nil {
		return e.fail(err)
	}
	return ExitOK
}
