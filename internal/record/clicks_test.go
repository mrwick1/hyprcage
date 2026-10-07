package record

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClicksDrawRing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.clicks")
	c := &clicks{path: path}
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	if c.draw(img) { // no file yet: nothing drawn, no error
		t.Error("input reported without a file")
	}
	now := time.Now().UnixMilli()
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d move 90 90\n%d click 50 50\npartial", now, now)), 0o600); err != nil {
		t.Fatal(err)
	}
	if !c.draw(img) {
		t.Error("new input not reported")
	}
	if len(c.recent) != 1 {
		t.Fatalf("%d clicks read, want 1 (a move draws nothing, the partial line waits)", len(c.recent))
	}
	if img.Pix[img.PixOffset(60, 50)+2] == 0 {
		t.Error("no ring 10 px right of the click")
	}
	if img.Pix[img.PixOffset(50, 50)+2] != 0 {
		t.Error("the centre of the click is painted")
	}
	c.recent[0].at = time.Now().Add(-time.Second)
	if c.draw(img) {
		t.Error("input reported with no new line")
	}
	if len(c.recent) != 0 {
		t.Error("a click older than ringTime is still drawn")
	}
}
