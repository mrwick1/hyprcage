package perceive

import (
	"context"

	"github.com/hexadecimil/hyprcage/internal/screen"
)

const defaultReadSteps = 30

// ReadOpts selects what ReadPage reads.
type ReadOpts struct {
	Mode     Mode
	MaxNodes int // 0 means 1000
	MaxSteps int // snapshots to take at most, 0 means 30
}

// ReadPage takes a snapshot, scrolls one step with scroll and repeats until
// two snapshots in a row render the same or MaxSteps snapshots are taken.
// It returns the nodes of all snapshots, each Key once, in the order first
// seen, with refs assigned. OCR keys name the position in one snapshot, so
// OCR cannot be merged and ReadPage refuses it.
func ReadPage(ctx context.Context, src Source, t *Table, o ReadOpts, scroll func(context.Context) error) (string, []Node, error) {
	if src.Name() == "ocr" {
		return "", nil, screen.Errf(screen.CodeNoSource, "launch the app with debug=true", "read_page cannot merge OCR snapshots")
	}
	steps := o.MaxSteps
	if steps <= 0 {
		steps = defaultReadSteps
	}
	limit := o.MaxNodes
	if limit <= 0 {
		limit = 1000
	}
	var merged []Node
	seen := map[string]bool{}
	prev := ""
	for i := 0; i < steps; i++ {
		out, nodes, err := Snapshot(ctx, src, t, SnapOpts{Mode: o.Mode, MaxNodes: 1 << 30})
		if err != nil {
			return "", nil, err
		}
		if out == prev {
			break
		}
		prev = out
		for _, n := range nodes {
			if !seen[n.Key] {
				seen[n.Key] = true
				merged = append(merged, n)
			}
		}
		if i == steps-1 {
			break
		}
		if err := scroll(ctx); err != nil {
			return "", nil, err
		}
	}
	truncated := len(merged) > limit
	merged = merged[:min(limit, len(merged))]
	t.Assign(merged)
	t.SetSource(src.Name())
	return Render(Header{Source: src.Name(), Nodes: len(merged), Truncated: truncated}, merged), merged, nil
}
