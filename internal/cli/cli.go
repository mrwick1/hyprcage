// Package cli implements the hyprcage command line (cahier §6.1). Every MCP
// tool has a CLI twin here so that everything the agent can do, a human can
// do by hand in a terminal.
package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/hexadecimil/hyprcage/internal/version"
)

// Exit codes (cahier §6.1).
const (
	ExitOK         = 0
	ExitUsage      = 1
	ExitScreen     = 2 // screen not found or dead
	ExitNotOwner   = 3
	ExitDependency = 4 // dependency missing or Hyprland unreachable
	ExitTimeout    = 5
)

// Env is what a command receives: its arguments and the three streams.
type Env struct {
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

type command struct {
	name    string
	summary string
	run     func(*Env) int
	hidden  bool
}

var commands []command

func init() {
	commands = []command{
		{"create", "create an agent screen", runCreate, false},
		{"launch", "run an application inside a screen", runLaunch, false},
		{"shot", "capture a screen to PNG", runShot, false},
		{"click", "click at screen coordinates", runClick, false},
		{"move", "move the pointer", runMove, false},
		{"scroll", "scroll at screen coordinates", runScroll, false},
		{"drag", "drag from one point to another", runDrag, false},
		{"type", "type Unicode text", runType, false},
		{"key", "press key combinations", runKey, false},
		{"wait", "wait for time, image stability or a window title", runWait, false},
		{"windows", "list the windows of a screen", runWindows, false},
		{"close", "ask a window to close", runClose, false},
		{"destroy", "close a screen", runDestroy, false},
		{"mirror", "open or close the human's mirror window for a screen", runMirrorOpen, false},
		{"list", "list screens", runList, false},
		{"gc", "close screens of dead sessions and orphans", runGC, false},
		{"doctor", "check dependencies and Hyprland", runDoctor, false},
		{"config", "write the configuration file with the defaults, unless it exists", runConfig, false},
		{"setup", "install cage (asks for your password)", runSetup, false},
		{"mcp", "run the MCP server on stdio", runMCP, false},
		{"version", "print the version", runVersion, false},
		{"record", "record a screen or the desktop to MP4 (start, stop, status)", runRecord, false},
		{"clip", "read or write the clipboard of a screen", runClip, false},
		{"show", "switch to the workspace of a screen's mirror", runShow, false},
		{"session-start", "Claude Code SessionStart hook", runSessionStart, true},
		{"session-end", "Claude Code SessionEnd hook", runSessionEnd, true},
		{"_holder", "internal: keeps cage alive and publishes its socket", runHolder, true},
		{"_mirror", "internal: shows a screen in a window on the human's compositor", runMirror, true},
		{"_record", "internal: captures a screen or the desktop into ffmpeg", runRecordChild, true},
	}
}

// Run dispatches args to a command and returns the process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	e := &Env{Stdin: stdin, Stdout: stdout, Stderr: stderr}
	if len(args) == 0 {
		usage(stderr)
		return ExitUsage
	}
	name := args[0]
	if name == "help" || name == "-h" || name == "--help" {
		usage(stdout)
		return ExitOK
	}
	for _, c := range commands {
		if c.name == name {
			e.Args = args[1:]
			return c.run(e)
		}
	}
	fmt.Fprintf(stderr, "hyprcage: unknown command %q\n\n", name)
	usage(stderr)
	return ExitUsage
}

func usage(w io.Writer) {
	fmt.Fprintf(w, "hyprcage %s: a caged desktop for AI agents on Hyprland\n\nUsage: hyprcage <command> [options]\n\nCommands:\n", version.Version)
	for _, c := range commands {
		if c.hidden {
			continue
		}
		fmt.Fprintf(w, "  %-10s %s\n", c.name, c.summary)
	}
}

func notYet(when string) func(*Env) int {
	return func(e *Env) int {
		return e.errorf("not implemented yet (%s)", when)
	}
}

func runVersion(e *Env) int {
	fmt.Fprintln(e.Stdout, version.Version)
	return ExitOK
}

// errorf prints a usage-level error and returns ExitUsage.
func (e *Env) errorf(format string, a ...any) int {
	fmt.Fprintf(e.Stderr, "hyprcage: "+format+"\n", a...)
	return ExitUsage
}

// flags returns a FlagSet whose errors go to the env's stderr.
func (e *Env) flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	return fs
}

// parse parses e.Args with options accepted anywhere, before or after the
// positional arguments (`hyprcage click hc-1 640 400 --button right`), which
// the flag package alone does not do. A bare "--" ends option parsing.
func (e *Env) parse(fs *flag.FlagSet) error {
	var opts, pos []string
	args := e.Args
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' || a == "-" {
			pos = append(pos, a)
			continue
		}
		opts = append(opts, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") {
			continue
		}
		f := fs.Lookup(name)
		if f == nil {
			continue // let Parse report it
		}
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
			continue
		}
		if i+1 < len(args) {
			opts = append(opts, args[i+1])
			i++
		}
	}
	if err := fs.Parse(opts); err != nil {
		return err
	}
	return fs.Parse(append([]string{"--"}, pos...))
}
