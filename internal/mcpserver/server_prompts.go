package mcpserver

import (
	"image"

	"github.com/hexadecimil/offstage/internal/desktop"
	"github.com/hexadecimil/offstage/internal/hypr"
	"github.com/hexadecimil/offstage/internal/osfeat"
	"github.com/hexadecimil/offstage/internal/screen"
)

// promptGuard refuses an input action on the human's desktop when a system
// prompt (polkit, keyring, pinentry) has the focus, is the target window or
// lies under the click. Neither ack nor guard.enabled = "off" lifts it.
func (s *Server) promptGuard(c guardCall) error {
	if s.osf.Prompts == nil || !desktop.IsDesktop(c.Screen) {
		return nil
	}
	d, err := s.desktop()
	if err != nil {
		return nil // the action itself reports that the desktop is unreachable
	}
	wins, err := d.Windows()
	if err != nil {
		return nil
	}
	if w := promptTarget(s.osf.Prompts, wins, c); w != nil {
		return screen.Errf(screen.CodeSystemPrompt, "ask the human to answer the prompt, then retry",
			"window %s (%s) is a system prompt; offstage does not act on it", w.Address, w.Class)
	}
	return nil
}

// promptTarget returns the prompt window that c would reach, nil when none.
func promptTarget(p osfeat.Prompts, wins []hypr.Client, c guardCall) *hypr.Client {
	for i, w := range wins {
		if !p.IsPrompt(osfeat.Window{Class: w.Class, Title: w.Title}) {
			continue
		}
		box := image.Rect(w.At[0], w.At[1], w.At[0]+w.Size[0], w.At[1]+w.Size[1])
		if w.FocusHistoryID == 0 || w.Address == c.Window || (c.At != nil && image.Pt(c.At[0], c.At[1]).In(box)) {
			return &wins[i]
		}
	}
	return nil
}
