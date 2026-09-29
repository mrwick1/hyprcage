// Package setup installs what hyprcage needs on the machine, with the
// human's authorisation: cage, the agent's compositor, through the
// distribution's package manager. Root is obtained the way a desktop
// application store does it: sudo when it needs no password or a terminal
// is there, otherwise a polkit dialog (pkexec) on the human's screen.
package setup

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Packages lists what hyprcage needs, with the binary that proves each
// package is present.
var Packages = []struct{ Package, Binary string }{
	{"cage", "cage"},
	{"ffmpeg", "ffmpeg"},        // recording
	{"wl-clipboard", "wl-copy"}, // clipboard of a screen
}

// Report says what Run found and did.
type Report struct {
	Missing   []string `json:"missing"`   // before running
	Installed []string `json:"installed"` // by this run
	Method    string   `json:"method"`    // sudo, pkexec, or none
	Manual    string   `json:"manual,omitempty"`
}

// Missing returns the packages whose binary is not in PATH.
func Missing() []string {
	var out []string
	for _, p := range Packages {
		if _, err := exec.LookPath(p.Binary); err != nil {
			out = append(out, p.Package)
		}
	}
	return out
}

// InstallArgv returns the command that installs pkgs with the first known
// package manager on PATH: pacman (Arch) or zypper (openSUSE).
func InstallArgv(lookPath func(string) (string, error), pkgs []string) ([]string, error) {
	if _, err := lookPath("pacman"); err == nil {
		return append([]string{"pacman", "-S", "--needed", "--noconfirm"}, pkgs...), nil
	}
	if _, err := lookPath("zypper"); err == nil {
		return append([]string{"zypper", "--non-interactive", "install", "--no-recommends"}, pkgs...), nil
	}
	return nil, errors.New("no known package manager (pacman or zypper): install " + strings.Join(pkgs, " and ") + " by hand")
}

// ManualCommand is what the human can run themselves.
func ManualCommand(pkgs []string) string {
	argv, err := InstallArgv(exec.LookPath, pkgs)
	if err != nil {
		return err.Error()
	}
	return "sudo " + strings.Join(argv, " ")
}

// Run installs the missing packages. It tries sudo without a password
// (NOPASSWD setups), sudo with the terminal when stdin is one, then pkexec,
// which opens the desktop's authentication dialog. When none applies the
// report carries the command for the human and err is non-nil.
func Run() (Report, error) {
	rep := Report{Missing: Missing(), Method: "none"}
	if len(rep.Missing) == 0 {
		return rep, nil
	}
	pm, err := InstallArgv(exec.LookPath, rep.Missing)
	if err != nil {
		rep.Manual = err.Error()
		return rep, err
	}
	var attempts []struct {
		method string
		argv   []string
	}
	if exec.Command("sudo", "-n", "true").Run() == nil {
		attempts = append(attempts, struct {
			method string
			argv   []string
		}{"sudo", append([]string{"sudo", "-n"}, pm...)})
	} else if stdinIsTerminal() {
		attempts = append(attempts, struct {
			method string
			argv   []string
		}{"sudo", append([]string{"sudo"}, pm...)})
	}
	if _, err := exec.LookPath("pkexec"); err == nil {
		attempts = append(attempts, struct {
			method string
			argv   []string
		}{"pkexec", append([]string{"pkexec"}, pm...)})
	}
	var last error
	for _, a := range attempts {
		cmd := exec.Command(a.argv[0], a.argv[1:]...)
		cmd.Stdin = os.Stdin
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			last = fmt.Errorf("%s: %v: %s", a.method, err, strings.TrimSpace(lastLine(out.String())))
			continue
		}
		rep.Method = a.method
		rep.Installed = rep.Missing
		if still := Missing(); len(still) > 0 {
			rep.Installed = diff(rep.Missing, still)
			return rep, fmt.Errorf("%s ran but %s still missing", a.method, strings.Join(still, ", "))
		}
		return rep, nil
	}
	rep.Manual = ManualCommand(rep.Missing)
	if last == nil {
		last = errors.New("no way to ask for root here (no sudo without password, no terminal, no pkexec)")
	}
	return rep, fmt.Errorf("%v. From a terminal: %s", last, rep.Manual)
}

func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func diff(all, still []string) []string {
	var out []string
	for _, p := range all {
		found := false
		for _, s := range still {
			if s == p {
				found = true
			}
		}
		if !found {
			out = append(out, p)
		}
	}
	return out
}
