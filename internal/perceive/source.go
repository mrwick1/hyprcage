package perceive

import "context"

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
