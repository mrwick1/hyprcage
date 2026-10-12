package perceive

import (
	"image"

	"github.com/hexadecimil/hyprcage/internal/screen"
)

// FrameDiff is the change between two frames. Ratio is the fraction (0 to 1) of
// changed pixels. BBox is the smallest rectangle that holds every change.
type FrameDiff struct {
	Ratio float64
	BBox  image.Rectangle
}

// Compare diffs two frames. Pixels inside an ignore rectangle (the mouse
// pointer) do not count. A change that spans one pixel or less in width or
// height, such as a blinking text caret, does not count either. Frames of
// different size count as a full change.
func Compare(a, b *image.RGBA, ignore ...image.Rectangle) FrameDiff {
	n, bbox, ok := screen.DiffPixels(a, b, ignore...)
	if !ok {
		return FrameDiff{Ratio: 1, BBox: a.Bounds().Union(b.Bounds())}
	}
	if n == 0 || bbox.Dx() <= 1 || bbox.Dy() <= 1 {
		return FrameDiff{}
	}
	return FrameDiff{Ratio: float64(n) / float64(len(a.Pix)/4), BBox: bbox}
}
