package screen

import (
	"image"

	"github.com/hexadecimil/offstage/internal/wl"
)

// Surface is what the screen tools act on. A cage screen gives a
// *wl.Client. A headless browser screen gives a CDP implementation
// (package headless). Coordinates are screen pixels.
type Surface interface {
	PointerInput
	// Type enters text as the user would, at the focus.
	Type(text string) error
	// Key presses one combination, for example "ctrl+a" or "Return".
	Key(combo string) error
	// Capture returns the whole screen. cursor draws the pointer on it.
	Capture(cursor bool) (*image.RGBA, error)
	// Toplevels lists the windows (a browser: its pages).
	Toplevels() ([]wl.Toplevel, error)
	// CloseToplevel closes one window by its id.
	CloseToplevel(id uint32) error
}

var _ Surface = (*wl.Client)(nil)
