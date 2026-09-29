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

// The probes of Choose, replaced in tests so that no test dials a real bus.
var (
	ocrData  = OCRData
	hasApps  = HasApps
	newCDP   = NewCDP
	newATSPI = NewATSPI
	newOCR   = NewOCR
)

const noSourceHint = "launch the app with debug=true, or run hyprcage setup for OCR"

// Choose picks the source. want is "auto", "cdp", "atspi" or "ocr".
// auto tries CDP when rec.DebugPort > 0 and it answers, then AT-SPI when
// HasApps, then OCR when OCRData exists. It returns CodeNoSource otherwise.
func Choose(ctx context.Context, rec *registry.Screen, cl *wl.Client, want string) (Source, error) {
	ocrOK := func() bool { _, err := os.Stat(ocrData); return err == nil }
	switch want {
	case "cdp":
		if rec.DebugPort == 0 {
			return nil, screen.Errf(screen.CodeNoSource, "launch the app with debug=true", "screen %s has no DevTools port", rec.Name)
		}
		return newCDP(ctx, rec.DebugPort)
	case "atspi":
		return newATSPI(ctx, rec.Name)
	case "ocr":
		if !ocrOK() {
			return nil, screen.Errf(screen.CodeNoSource, "run hyprcage setup", "no OCR data at %s", ocrData)
		}
		return newOCR(cl), nil
	case "auto":
		if rec.DebugPort > 0 {
			if src, err := newCDP(ctx, rec.DebugPort); err == nil {
				return src, nil
			} // a closed DevTools port falls through to AT-SPI and OCR
		}
		if hasApps(ctx, rec.Name) {
			if src, err := newATSPI(ctx, rec.Name); err == nil {
				return src, nil
			}
		}
		if ocrOK() {
			return newOCR(cl), nil
		}
		return nil, screen.Errf(screen.CodeNoSource, noSourceHint, "screen %s has no DevTools port, no AT-SPI application and no OCR data", rec.Name)
	}
	return nil, fmt.Errorf("unknown source %q (auto, cdp, atspi, ocr)", want)
}
