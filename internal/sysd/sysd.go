// Package sysd wraps the systemd user manager: one transient slice per
// screen, a scope per process, transient timers for the safety gc
// (cahier §4.1, §5.3).
package sysd

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Available reports whether the user manager answers.
func Available() bool {
	out, err := exec.Command("systemctl", "--user", "is-system-running").Output()
	state := strings.TrimSpace(string(out))
	if err != nil && state == "" {
		return false
	}
	return state == "running" || state == "degraded" || state == "starting"
}

// unitSafe turns a screen name into a unit-name fragment: dashes would make
// systemd create one intermediate slice per segment (offstage-hc.slice…),
// so they become underscores, which never occur in screen names.
func unitSafe(screen string) string { return strings.ReplaceAll(screen, "-", "_") }

// ScreenFromUnit reverses unitSafe on a fragment extracted from a unit name.
func ScreenFromUnit(fragment string) string { return strings.ReplaceAll(fragment, "_", "-") }

func SliceName(screen string) string      { return "offstage-" + unitSafe(screen) + ".slice" }
func UnitName(screen, role string) string { return "offstage-" + unitSafe(screen) + "-" + role }
func GCTimerName(screen string) string    { return "offstage-gc-" + unitSafe(screen) }

// ScopeArgs builds the argv that runs cmd in a transient scope of the
// screen's slice with a 3 s stop timeout (the inherited default is 90 s).
// The scope is collected once inactive, failed included: a process killed
// at the timeout must not leave a failed unit that keeps its name busy.
// Note: systemd-run --scope stays the parent of cmd, it does not exec it.
func ScopeArgs(screen, role string, cmd []string) []string {
	args := []string{"systemd-run", "--user", "--scope", "--quiet", "--collect",
		"-p", "TimeoutStopSec=3s",
		"--slice", SliceName(screen),
		"--unit", UnitName(screen, role),
		"--"}
	return append(args, cmd...)
}

func run(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// StopUnit stops a unit; stopping a slice stops every scope under it.
func StopUnit(unit string) error { return run("systemctl", "--user", "stop", unit) }

// StopScreen stops a screen's units in an order its applications survive:
// the applications first, so that they see their SIGTERM before their
// compositor is gone, then the mirrors, then cage, then the slice itself.
func StopScreen(screen string) {
	units, _ := ListUnits("offstage-" + unitSafe(screen) + "-*")
	for _, want := range []string{"-app-", "-mirror", "-cage"} {
		for _, u := range units {
			if strings.Contains(u, want) {
				_ = StopUnit(u)
			}
		}
	}
	_ = StopUnit(SliceName(screen))
	ResetFailed("offstage-" + unitSafe(screen) + "-*")
	ResetFailed(SliceName(screen))
}

// ResetFailed clears failed units (a name or a glob) so their names can be
// reused.
func ResetFailed(unit string) { _ = run("systemctl", "--user", "reset-failed", unit) }

// ScheduleOnce runs cmd once after the delay in a transient unit (M1, M2).
func ScheduleOnce(after time.Duration, unit string, cmd []string) error {
	args := []string{"systemd-run", "--user", "--quiet", "--collect", "--on-active=" + seconds(after)}
	if unit != "" {
		args = append(args, "--unit", unit)
	}
	args = append(args, "--")
	return run(append(args, cmd...)...)
}

// ArmTimer installs a periodic transient timer (M3); an already active timer
// of that name is kept, so arming twice is idempotent.
func ArmTimer(unit string, every time.Duration, cmd []string) error {
	if active(unit + ".timer") {
		return nil
	}
	args := []string{"systemd-run", "--user", "--quiet", "--collect",
		"--unit", unit,
		"--on-active=" + seconds(every),
		"--on-unit-inactive=" + seconds(every),
		"--"}
	return run(append(args, cmd...)...)
}

// DisarmTimer stops the timer and its service.
func DisarmTimer(unit string) {
	_ = run("systemctl", "--user", "stop", unit+".timer")
	_ = run("systemctl", "--user", "stop", unit+".service")
	ResetFailed(unit + ".service")
}

func active(unit string) bool {
	out, _ := exec.Command("systemctl", "--user", "is-active", unit).Output()
	return strings.TrimSpace(string(out)) == "active"
}

// ListUnits returns the unit names matching a glob such as "offstage-*".
func ListUnits(pattern string) ([]string, error) {
	out, err := exec.Command("systemctl", "--user", "list-units", "--all", "--plain", "--no-legend", pattern).Output()
	if err != nil {
		return nil, err
	}
	var units []string
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			units = append(units, f[0])
		}
	}
	return units, nil
}

func seconds(d time.Duration) string { return fmt.Sprintf("%ds", int(d.Seconds())) }
