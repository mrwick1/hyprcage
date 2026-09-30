// Command hyprcage gives AI agents their own virtual screen on Hyprland.
// See docs/development.md for the architecture.
package main

import (
	"os"

	"github.com/hexadecimil/hyprcage/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
