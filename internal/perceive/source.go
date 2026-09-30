package perceive

import (
	"context"
	"fmt"
	"os"

	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// Source reads the nodes of one screen.
type Source interface {
	Name() string // "cdp", "atspi" or "ocr"
	// Nodes returns every node in document order, unfiltered.
	Nodes(ctx context.Context) ([]Node, error)
	// Reveal scrolls the node into view and returns it with fresh coordinates.
	Reveal(ctx context.Context, key string) (Node, error)
	// Press activates the node without the pointer. Only AT-SPI implements it.
	Press(ctx context.Context, key string) error
	Close() error
}

// hitTester is a Source that can tell whether another element covers a
// node. Act calls it before pointer input. Only CDP implements it.
type hitTester interface {
	// HitTest returns CodeRefOccluded when the node's centre hits another element.
	HitTest(ctx context.Context, key string) error
}

// The probes of Choose, replaced in tests so that no test dials a real bus.
var (
	ocrData  = OCRData
	hasApps  = HasApps
	newCDP   = NewCDP
	newATSPI = NewATSPI
	newOCR   = NewOCR
	ownsPort = screen.OwnsPort
)

// shotOf returns the capture function of the screen that cl is connected to.
func shotOf(cl *wl.Client) func(screen.ShotOptions) (*screen.ShotResult, error) {
	return func(o screen.ShotOptions) (*screen.ShotResult, error) { return screen.Shot(cl, o) }
}

// foreignPortHint answers a DevTools port that another program took over.
const foreignPortHint = "the DevTools port no longer belongs to this screen; relaunch the app with debug=true"

const noSourceHint = "launch the app with debug=true, or run hyprcage setup for OCR"

// Choose picks the source. want is "auto", "cdp", "atspi" or "ocr".
// auto tries CDP when rec.DebugPort > 0, the process recorded as its
// owner still listens on it and it answers, then AT-SPI when
// HasApps, then OCR when OCRData exists. It returns CodeNoSource otherwise.
func Choose(ctx context.Context, rec *registry.Screen, cl *wl.Client, want string) (Source, error) {
	ocrOK := func() bool { _, err := os.Stat(ocrData); return err == nil }
	switch want {
	case "cdp":
		if rec.DebugPort == 0 {
			return nil, screen.Errf(screen.CodeNoSource, "launch the app with debug=true", "screen %s has no DevTools port", rec.Name)
		}
		if !ownsPort(rec) {
			return nil, screen.Errf(screen.CodeCDP, foreignPortHint, "the process that opened 127.0.0.1:%d for screen %s no longer listens on it", rec.DebugPort, rec.Name)
		}
		return newCDP(ctx, rec.DebugPort)
	case "atspi":
		return newATSPI(ctx, rec.Name, rec.Width, rec.Height)
	case "ocr":
		if !ocrOK() {
			return nil, screen.Errf(screen.CodeNoSource, "run hyprcage setup", "no OCR data at %s", ocrData)
		}
		return newOCR(shotOf(cl)), nil
	case "auto":
		cdpNote := ""
		if rec.DebugPort > 0 && !ownsPort(rec) {
			// another screen's app or the human's Chrome may hold the port now
			cdpNote = " (CDP: " + foreignPortHint + ")"
		} else if rec.DebugPort > 0 {
			src, err := newCDP(ctx, rec.DebugPort)
			if err == nil {
				return src, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			cdpNote = fmt.Sprintf(" (CDP: %v)", err) // a closed DevTools port falls through to AT-SPI and OCR
		}
		if hasApps(ctx, rec.Name) {
			if src, err := newATSPI(ctx, rec.Name, rec.Width, rec.Height); err == nil {
				return src, nil
			}
		}
		if ocrOK() {
			return newOCR(shotOf(cl)), nil
		}
		return nil, screen.Errf(screen.CodeNoSource, noSourceHint, "screen %s has no DevTools answer, no AT-SPI application and no OCR data%s", rec.Name, cdpNote)
	}
	return nil, fmt.Errorf("unknown source %q (auto, cdp, atspi, ocr)", want)
}
