package perceive

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func testFrame(fill func(*image.RGBA)) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 800))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{250, 250, 250, 255}), image.Point{}, draw.Src)
	if fill != nil {
		fill(img)
	}
	return img
}

func box(r image.Rectangle) func(*image.RGBA) {
	return func(img *image.RGBA) {
		draw.Draw(img, r, image.NewUniform(color.RGBA{20, 20, 200, 255}), image.Point{}, draw.Src)
	}
}

func TestJudge(t *testing.T) {
	button := image.Rect(100, 100, 200, 130)
	cases := []struct {
		name   string
		after  func(*image.RGBA)
		points []image.Point
		want   Verdict
	}{
		{"identical frames", nil, []image.Point{{150, 115}}, NoOp},
		{"caret blink only", box(image.Rect(300, 300, 301, 320)), []image.Point{{150, 115}}, NoOp},
		{"button changes under the click", box(button), []image.Point{{150, 115}}, Confirmed},
		{"change far from the click", box(image.Rect(900, 600, 1100, 700)), []image.Point{{150, 115}}, Diverged},
		{"change next to the click", box(image.Rect(100, 150, 300, 300)), []image.Point{{150, 115}}, Partial},
		{"key with no target and a change", box(image.Rect(900, 600, 1100, 700)), nil, Confirmed},
		{"full change", box(image.Rect(0, 0, 1280, 800)), []image.Point{{150, 115}}, Confirmed},
		{"drag where only the start changed", box(button), []image.Point{{150, 115}, {800, 500}}, Partial},
		{"drag where both ends changed", box(image.Rect(100, 100, 900, 600)), []image.Point{{150, 115}, {800, 500}}, Confirmed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NewVerification(Compare(testFrame(nil), testFrame(c.after)), c.points...)
			if got.Verdict != c.want {
				t.Fatalf("verdict %s (diff %+v), want %s", got.Verdict, got.Diff, c.want)
			}
			if got.Sense.NoOp != (c.want == NoOp) || got.Sense.Changed != (c.want != NoOp) {
				t.Fatalf("sense %+v does not follow verdict %s", got.Sense, c.want)
			}
		})
	}
}

func TestVerificationLine(t *testing.T) {
	v := NewVerification(Compare(testFrame(nil), testFrame(box(image.Rect(120, 80, 760, 380)))), image.Pt(200, 200))
	want := "verify: CONFIRMED (changed 18.7%, bbox 120,80 640x300)"
	if got := v.String(); got != want {
		t.Fatalf("line %q, want %q", got, want)
	}
	if got := NewVerification(FrameDiff{}).String(); got != "verify: NO_OP (changed 0.0%)" {
		t.Fatalf("line %q", got)
	}
}
