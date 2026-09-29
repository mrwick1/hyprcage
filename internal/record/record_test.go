package record

import (
	"bytes"
	"context"
	"image"
	"strings"
	"testing"
	"time"
)

type fakeCap struct {
	img   *image.RGBA
	calls int
	grow  int // after this many calls, return a bigger image (0 = never)
}

func (f *fakeCap) Capture(bool) (*image.RGBA, error) {
	f.calls++
	if f.grow > 0 && f.calls >= f.grow {
		return image.NewRGBA(image.Rect(0, 0, 8, 8)), nil
	}
	return f.img, nil
}

func TestEncoder(t *testing.T) {
	cases := map[string]string{
		" V....D libx264   H.264\n V....D libopenh264 H.264\n": "libx264",
		" V....D libopenh264 OpenH264\n V....D mpeg4 MPEG-4\n": "libopenh264",
		" V....D mpeg4 MPEG-4 part 2\n":                        "mpeg4",
	}
	for in, want := range cases {
		if got := Encoder(in); got != want {
			t.Errorf("Encoder(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestFFmpegArgs(t *testing.T) {
	a := strings.Join(FFmpegArgs(1280, 800, 10, "libopenh264", "/tmp/o.mp4"), " ")
	for _, want := range []string{"-f rawvideo", "-pix_fmt rgba", "-s 1280x800", "-framerate 10", "-i -", "-c:v libopenh264", "-pix_fmt yuv420p", "/tmp/o.mp4"} {
		if !strings.Contains(a, want) {
			t.Errorf("argv lacks %q: %s", want, a)
		}
	}
}

func TestLoopKeepsRealTime(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	var buf bytes.Buffer
	n, err := Loop(context.Background(), &fakeCap{img: img}, &buf, 20, 250*time.Millisecond, img)
	if err != nil {
		t.Fatal(err)
	}
	if n < 4 || n > 7 {
		t.Errorf("%d frames for 250 ms at 20 fps, want about 5", n)
	}
	if buf.Len() != n*4*2*4 {
		t.Errorf("%d bytes for %d frames of 4x2 RGBA", buf.Len(), n)
	}
}

func TestLoopStopsOnCancel(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	start := time.Now()
	if _, err := Loop(ctx, &fakeCap{img: img}, &bytes.Buffer{}, 10, time.Hour, img); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Error("Loop did not stop on cancel")
	}
}

func TestLoopRejectsSizeChange(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	n, err := Loop(context.Background(), &fakeCap{img: img, grow: 2}, &buf, 50, time.Second, img)
	if err == nil || !strings.Contains(err.Error(), "changed size") {
		t.Fatalf("want a size error, got %v", err)
	}
	if buf.Len() != n*4*4*4 {
		t.Errorf("partial frame written: %d bytes for %d frames", buf.Len(), n)
	}
}
