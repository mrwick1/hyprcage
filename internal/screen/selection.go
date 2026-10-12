package screen

import (
	"time"

	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

const (
	selectionSentinel = "\x00hyprcage-no-selection\x00"
	selectionPolls    = 20
	selectionPollGap  = 50 * time.Millisecond
)

// ReadSelection copies the selection of the focused window of a screen
// and returns it. It restores the earlier clipboard of the screen.
// No selection returns "".
func ReadSelection(rec *registry.Screen, cl *wl.Client) (string, error) {
	return readSelection(
		func() (string, error) { return ClipboardGet(rec) },
		func(s string) error { return ClipboardSet(rec, s) },
		func(combo string) error { return cl.Key(combo) },
	)
}

// readSelection puts a sentinel on the clipboard, presses ctrl+c and
// polls until the clipboard changes. A clipboard that keeps the sentinel
// means the window had no selection.
func readSelection(get func() (string, error), set func(string) error, key func(string) error) (string, error) {
	prev, err := get()
	if err != nil {
		return "", err
	}
	if err := set(selectionSentinel); err != nil {
		return "", err
	}
	text := ""
	err = key("ctrl+c")
	for i := 0; err == nil && i < selectionPolls; i++ {
		var cur string
		if cur, err = get(); err != nil {
			break
		}
		if cur != selectionSentinel {
			text = cur
			break
		}
		time.Sleep(selectionPollGap)
	}
	if rerr := set(prev); err == nil {
		err = rerr
	}
	return text, err
}
