package mcpserver

import "runtime"

// goos is the system the server runs on; a test sets it.
var goos = runtime.GOOS

// offLinux lists native tools available off Linux. Bare tools are registered
// separately on every platform.
var offLinux = map[string]bool{"report_issue": true}

// available reports whether the tool is registered on this system.
func available(name string) bool { return goos == "linux" || offLinux[name] }

// instructionsOffLinux replaces the instructions where there are only
// headless screens.
const instructionsOffLinux = `offstage ships Bare, its own headless browser. Chrome is not required.
Use browser_open, browser_snapshot, browser_act and browser_find. Use browser_ensure_session and browser_save_state for login state, browser_run_steps or browser_run_suite for journeys, and browser_console, browser_network and browser_diagnose to inspect failures. Close each session with browser_close.
The browser_* tools do not need screen_create. Native apps, mirrors and desktop tools are unavailable on this system.
` + bareLoop + reportNote
