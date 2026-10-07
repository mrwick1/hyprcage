package screen

import (
	"image"
	"testing"
)

func TestChangedPercent(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 100, 100))
	b := image.NewRGBA(image.Rect(0, 0, 100, 100))
	if p := ChangedPercent(a, b); p != 0 {
		t.Errorf("identical images: %v", p)
	}
	for i := 0; i < 10; i++ { // one 10-pixel line moves by 100
		b.Pix[i*4] = 100
	}
	if p := ChangedPercent(a, b); p < 0.09 || p > 0.11 {
		t.Errorf("10 of 10000 pixels: %v%%, want 0.1", p)
	}
	c := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for i := range c.Pix { // noise below the tolerance
		c.Pix[i] = 5
	}
	if p := ChangedPercent(a, c); p != 0 {
		t.Errorf("sub-tolerance noise counted: %v", p)
	}
}
