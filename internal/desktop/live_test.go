package desktop

import (
	"os"
	"slices"
	"testing"
	"time"
)

// TestLiveCapture times a window capture and an output capture on the
// nested Hyprland that tests/e2e-desktop.py starts. It never runs without
// HYPRCAGE_DESKTOP_INSTANCE (the nested instance) and HYPRCAGE_LIVE_WINDOW (a
// window address of that instance), so it cannot reach the human's screens.
func TestLiveCapture(t *testing.T) {
	addr := os.Getenv("HYPRCAGE_LIVE_WINDOW")
	if addr == "" || os.Getenv("HYPRCAGE_DESKTOP_INSTANCE") == "" {
		t.Skip("set HYPRCAGE_DESKTOP_INSTANCE and HYPRCAGE_LIVE_WINDOW (see tests/e2e-desktop.py)")
	}
	h, err := Instance(nil)
	if err != nil || h == nil {
		t.Fatalf("instance: %v", err)
	}
	conn, err := Open(h)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CL.Close()
	for _, tc := range []struct{ name, addr string }{{"window", addr}, {"output", ""}} {
		var d []time.Duration
		for i := 0; i < 21; i++ {
			start := time.Now()
			img, err := conn.Frame(tc.addr, false)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if i == 0 {
				t.Logf("%s frame %v (RGBA, %d bytes)", tc.name, img.Bounds(), len(img.Pix))
				continue // the first capture sets up the buffers
			}
			d = append(d, time.Since(start))
		}
		slices.Sort(d)
		t.Logf("CAPTURE %s: median %v, min %v, max %v (20 captures)", tc.name, d[len(d)/2], d[0], d[len(d)-1])
	}
}
