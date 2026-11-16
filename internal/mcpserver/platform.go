package mcpserver

import "runtime"

// goos is the system the server runs on; a test sets it.
var goos = runtime.GOOS

// offLinux lists the tools that exist off Linux: a screen there is one
// headless Chrome. Every other tool is not registered, so an agent never
// sees a tool that cannot work.
var offLinux = map[string]bool{
	"screen_create": true, "screen_destroy": true, "screen_list": true, "browser_open": true,
	"screenshot": true, "click": true, "double_click": true, "type": true, "key": true,
	"scroll": true, "drag": true, "move": true, "wait": true,
	"snapshot": true, "act": true, "find": true, "read_page": true, "watch": true,
	"devtools_eval": true, "devtools_console": true, "devtools_network": true,
	"devtools_trace": true, "devtools_heap": true, "devtools_audit": true, "devtools_emulate": true,
	"record_start": true, "record_stop": true, "report_issue": true,
}

// available reports whether the tool is registered on this system.
func available(name string) bool { return goos == "linux" || offLinux[name] }

// instructionsOffLinux replaces the instructions where there are only
// headless screens.
const instructionsOffLinux = `offstage gives you a browser screen of your own: one headless Chrome, with no window on the human's desktop. Chrome must be installed.
Workflow: screen_create -> browser_open -> snapshot / act / find / screenshot / click / type / key / scroll / drag / wait -> screen_destroy as soon as you are done.
Read a screen as text first: snapshot, then act by ref, and find (with timeout_ms) instead of wait plus screenshot. Take a screenshot only when the snapshot does not explain the page. Coordinates are page pixels.
Recording: record_start / record_stop need ffmpeg. devtools_eval, devtools_console, devtools_network, devtools_trace, devtools_heap, devtools_audit (Lighthouse) and devtools_emulate read and shape the page.
There is no desktop, no native app, no mirror window, no clipboard and no notification tool on this system.
` + reportNote
