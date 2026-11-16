// Package mirror shows the human what an agent screen looks like: an
// ordinary window on the human's compositor whose content is the screen's
// output, copied by cage into a memory file that the human's compositor
// reads directly. Nothing is copied by this process. A frame is only asked
// for when the human's compositor is ready for one, which it only is while
// the window is visible, and cage only copies it once the screen changed.
// A still screen, or a hidden mirror, costs nothing.
//
// The mirror is passive: it binds no seat on cage and relays no input.
// Clicking or typing in it does nothing.
package mirror

// Exit codes of the mirror process.
const (
	ExitClosed    = 0 // the human closed the window, or a signal
	ExitCageGone  = 2 // the screen's cage went away
	ExitHumanGone = 3 // the human's compositor went away
	ExitError     = 4
)

// Options tunes a mirror.
type Options struct {
	Display    string // the human's compositor, "" for $WAYLAND_DISPLAY
	FPS        int    // ceiling on the frames asked per second
	Fullscreen bool   // fill the workspace once placed
	Workspace  int    // the workspace the window was sent to, for the sidecar
	Log        func(format string, args ...any)
}
