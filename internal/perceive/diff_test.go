package perceive

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func solid(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{255, 255, 255, 255}), image.Point{}, draw.Src)
	return img
}

func paint(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	draw.Draw(img, r, image.NewUniform(c), image.Point{}, draw.Src)
}

var black = color.RGBA{0, 0, 0, 255}

func TestCompareIdentical(t *testing.T) {
	d := Compare(solid(100, 100), solid(100, 100))
	if d.Ratio != 0 || !d.BBox.Empty() {
		t.Fatalf("got %+v, want zero diff", d)
	}
}

func TestCompareCaretBlinkIgnored(t *testing.T) {
	a, b := solid(100, 100), solid(100, 100)
	paint(b, image.Rect(40, 30, 41, 46), black) // 1px wide, 16px tall caret
	if d := Compare(a, b); d.Ratio != 0 || !d.BBox.Empty() {
		t.Fatalf("caret counted as a change: %+v", d)
	}
}

func TestCompareMovedWindow(t *testing.T) {
	a, b := solid(100, 100), solid(100, 100)
	paint(a, image.Rect(10, 10, 30, 30), black)
	paint(b, image.Rect(50, 10, 70, 30), black)
	d := Compare(a, b)
	if d.Ratio != 0.08 { // two 20x20 areas of 10000 pixels
		t.Fatalf("ratio %v, want 0.08", d.Ratio)
	}
	if want := image.Rect(10, 10, 70, 30); d.BBox != want {
		t.Fatalf("bbox %v, want %v", d.BBox, want)
	}
}

func TestCompareFullChange(t *testing.T) {
	a, b := solid(50, 40), solid(50, 40)
	paint(b, b.Bounds(), black)
	d := Compare(a, b)
	if d.Ratio != 1 || d.BBox != a.Bounds() {
		t.Fatalf("got %+v, want full change", d)
	}
}

func TestCompareIgnoresPointer(t *testing.T) {
	a, b := solid(100, 100), solid(100, 100)
	pointer := image.Rect(60, 60, 76, 76)
	paint(b, pointer, black)
	if d := Compare(a, b, pointer); d.Ratio != 0 || !d.BBox.Empty() {
		t.Fatalf("pointer counted as a change: %+v", d)
	}
	paint(b, image.Rect(5, 5, 15, 15), black)
	d := Compare(a, b, pointer)
	if d.Ratio != 0.01 || d.BBox != image.Rect(5, 5, 15, 15) {
		t.Fatalf("got %+v", d)
	}
}

func TestCompareSizeMismatch(t *testing.T) {
	d := Compare(solid(10, 10), solid(20, 10))
	if d.Ratio != 1 {
		t.Fatalf("got %+v, want ratio 1", d)
	}
}
