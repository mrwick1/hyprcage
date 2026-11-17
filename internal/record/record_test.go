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
	late  int // this call takes delay (0 = none)
	delay time.Duration
}

func (f *fakeCap) Capture(bool) (*image.RGBA, error) {
	f.calls++
	if f.calls == f.late {
		time.Sleep(f.delay)
	}
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
	a := strings.Join(FFmpegArgs(1280, 800, 10, image.Rectangle{}, "libopenh264", "/tmp/o.mp4"), " ")
	for _, want := range []string{"-f rawvideo", "-pix_fmt rgba", "-s 1280x800", "-framerate 10", "-i -", "-c:v libopenh264", "-pix_fmt yuv420p", "/tmp/o.mp4"} {
		if !strings.Contains(a, want) {
			t.Errorf("argv lacks %q: %s", want, a)
		}
	}
	x := strings.Join(FFmpegArgs(1920, 1080, 30, image.Rectangle{}, "libx264", "/tmp/o.mp4"), " ")
	if !strings.Contains(x, "-c:v libx264 -crf 18 -preset superfast -tune stillimage -threads 1 -x264-params rc-lookahead=0:bframes=0 -pix_fmt yuv420p /tmp/o.mp4") {
		t.Errorf("libx264 argv lacks the quality options: %s", x)
	}
}

func TestFFmpegArgsCrop(t *testing.T) {
	a := strings.Join(FFmpegArgs(1920, 1080, 30, image.Rect(0, 87, 1920, 1080), "libx264", "/tmp/o.mp4"), " ")
	if !strings.Contains(a, "-vf crop=1920:993:0:87,scale=") {
		t.Errorf("argv lacks the crop: %s", a)
	}
	// A crop past the frame is clipped to it.
	a = strings.Join(FFmpegArgs(1280, 800, 30, image.Rect(-5, 80, 1300, 900), "libx264", "/tmp/o.mp4"), " ")
	if !strings.Contains(a, "-vf crop=1280:720:0:80,scale=") {
		t.Errorf("argv lacks the clipped crop: %s", a)
	}
	r, err := ParseRect(FormatRect(image.Rect(3, 87, 1923, 1080)))
	if err != nil || r != image.Rect(3, 87, 1923, 1080) {
		t.Errorf("ParseRect(FormatRect) = %v, %v", r, err)
	}
	if _, err := ParseRect("1,2,3"); err == nil {
		t.Error("ParseRect accepted 1,2,3")
	}
}

func TestLoopCutsIdle(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	var buf bytes.Buffer
	n, err := Loop(context.Background(), &fakeCap{img: img}, &buf, 50, time.Second, 100*time.Millisecond, 0.02, nil, img)
	if err != nil {
		t.Fatal(err)
	}
	// An unchanged screen keeps 100 ms (5 frames) of the 1 s pause.
	if n < 4 || n > 9 {
		t.Errorf("%d frames for a still 1 s at 50 fps with idle 100 ms, want about 6", n)
	}
}

func TestLoopKeepsRealTime(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	var buf bytes.Buffer
	n, err := Loop(context.Background(), &fakeCap{img: img}, &buf, 20, 250*time.Millisecond, 0, 0, nil, img)
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
	if _, err := Loop(ctx, &fakeCap{img: img}, &bytes.Buffer{}, 10, time.Hour, 0, 0, nil, img); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Error("Loop did not stop on cancel")
	}
}

func TestLoopRejectsSizeChange(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	n, err := Loop(context.Background(), &fakeCap{img: img, grow: 2}, &buf, 50, time.Second, 0, 0, nil, img)
	if err == nil || !strings.Contains(err.Error(), "changed size") {
		t.Fatalf("want a size error, got %v", err)
	}
	if buf.Len() != n*4*4*4 {
		t.Errorf("partial frame written: %d bytes for %d frames", buf.Len(), n)
	}
}

// slowWriter takes d for each write, like an encoder that is slower than
// the frame rate.
type slowWriter struct{ d time.Duration }

func (s slowWriter) Write(p []byte) (int, error) { time.Sleep(s.d); return len(p), nil }

func TestLoopSlowEncoder(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	c := &fakeCap{img: img}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(time.Second); cancel() }()
	start := time.Now()
	// 50 fps needs a frame every 20 ms; the writer takes 30 ms.
	if _, err := Loop(ctx, c, slowWriter{30 * time.Millisecond}, 50, time.Hour, 0, 0, nil, img); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 1100*time.Millisecond {
		t.Errorf("Loop took %v to stop after a cancel at 1 s", d)
	}
	// One capture per write keeps the video moving.
	if c.calls < 20 {
		t.Errorf("%d captures in 1 s: the video froze on old images", c.calls)
	}
}

// startWriter blocks its first write for d, like ffmpeg while it starts.
type startWriter struct {
	d    time.Duration
	once bool
}

func (s *startWriter) Write(p []byte) (int, error) {
	if !s.once {
		s.once = true
		time.Sleep(s.d)
	}
	return len(p), nil
}

func TestLoopKeepsFFmpegStart(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	n, err := Loop(context.Background(), &fakeCap{img: img}, &startWriter{d: 300 * time.Millisecond}, 20, 500*time.Millisecond, 0, 0, nil, img)
	if err != nil {
		t.Fatal(err)
	}
	if n < 9 || n > 12 {
		t.Errorf("%d frames for 500 ms at 20 fps with a 300 ms ffmpeg start, want about 10", n)
	}
}

func TestLoopStopsDuringCatchUp(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	// The second capture comes 1 s late, so the loop writes about 50 frames
	// of catch-up at 20 ms each; the cancel falls inside them.
	c := &fakeCap{img: img, late: 2, delay: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(1200 * time.Millisecond); cancel() }()
	start := time.Now()
	if _, err := Loop(ctx, c, slowWriter{20 * time.Millisecond}, 50, time.Hour, 0, 0, nil, img); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 1400*time.Millisecond {
		t.Errorf("Loop took %v to stop after a cancel at 1.2 s", d)
	}
}
