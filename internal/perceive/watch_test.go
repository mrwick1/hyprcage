package perceive

import (
	"image"
	"image/color"
	"testing"
)

func fill(c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
	}
	return img
}

// spin draws a 20x20 square at a position that depends on i.
func spin(i int) *image.RGBA {
	img := fill(color.RGBA{})
	x := (i % 4) * 20
	for dy := 0; dy < 20; dy++ {
		for dx := 0; dx < 20; dx++ {
			img.SetRGBA(x+dx, 40+dy, color.RGBA{255, 255, 255, 255})
		}
	}
	return img
}

func run(t *testing.T, n int, frame func(i int) *image.RGBA, expect bool) WatchResult {
	t.Helper()
	i := 0
	res, err := Watch(func() (*image.RGBA, error) { i++; return frame(i - 1), nil }, n, func() {}, expect)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestWatchVerdicts(t *testing.T) {
	static := func(int) *image.RGBA { return fill(color.RGBA{10, 20, 30, 255}) }
	cases := []struct {
		name   string
		frame  func(int) *image.RGBA
		expect bool
		want   string
	}{
		{"static page", static, false, "stable"},
		{"spinner", spin, false, "jitter"},
		{"static with expect_change", static, true, "frozen"},
		{"spinner with expect_change", spin, true, "jitter"},
	}
	for _, c := range cases {
		res := run(t, 6, c.frame, c.expect)
		if res.Verdict != c.want {
			t.Errorf("%s: verdict %q, want %q (ratios %v)", c.name, res.Verdict, c.want, res.Ratios)
		}
		if len(res.Frames) != 6 || len(res.Ratios) != 5 {
			t.Errorf("%s: %d frames, %d ratios", c.name, len(res.Frames), len(res.Ratios))
		}
	}
}

func TestWatchOneChangeIsStable(t *testing.T) {
	res := run(t, 6, func(i int) *image.RGBA {
		if i < 3 {
			return fill(color.RGBA{})
		}
		return fill(color.RGBA{200, 0, 0, 255})
	}, true)
	if res.Verdict != "stable" {
		t.Errorf("verdict %q, want stable (ratios %v)", res.Verdict, res.Ratios)
	}
}
