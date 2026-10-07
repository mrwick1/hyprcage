package record

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"strings"
	"time"
)

// ringTime is how long the ring of a click stays in the video.
const ringTime = 500 * time.Millisecond

// clicks reads the pointer input noted for a recorded screen
// (screen.InputPath) and draws a growing, fading ring around each click.
type clicks struct {
	path   string
	off    int64
	recent []click
}

type click struct {
	at time.Time
	p  image.Point
}

// draw paints the rings on img and reports whether input came since the
// last call.
func (c *clicks) draw(img *image.RGBA) bool {
	input := c.read()
	now := time.Now()
	keep := c.recent[:0]
	for _, k := range c.recent {
		if t := now.Sub(k.at); t >= 0 && t < ringTime {
			keep = append(keep, k)
			ring(img, k.p, float64(t)/float64(ringTime))
		}
	}
	c.recent = keep
	return input
}

// read takes the whole lines added since the last read and reports
// whether there were any.
func (c *clicks) read() bool {
	f, err := os.Open(c.path)
	if err != nil {
		return false
	}
	defer f.Close()
	if _, err := f.Seek(c.off, io.SeekStart); err != nil {
		return false
	}
	data, _ := io.ReadAll(f)
	end := bytes.LastIndexByte(data, '\n') + 1
	c.off += int64(end)
	for _, line := range strings.Split(string(data[:end]), "\n") {
		var ms int64
		var kind string
		var x, y int
		if _, err := fmt.Sscanf(line, "%d %s %d %d", &ms, &kind, &x, &y); err == nil && kind == "click" {
			c.recent = append(c.recent, click{time.UnixMilli(ms), image.Pt(x, y)})
		}
	}
	return end > 0
}

// ring draws a blue circle around p that grows and fades as t goes from 0
// to 1.
func ring(img *image.RGBA, p image.Point, t float64) {
	const width = 3.0
	r := 10 + 22*t
	a := 0.7 * (1 - t)
	n := int(r + width)
	b := image.Rect(p.X-n, p.Y-n, p.X+n+1, p.Y+n+1).Intersect(img.Bounds())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if d := math.Hypot(float64(x-p.X), float64(y-p.Y)) - r; d < -width/2 || d > width/2 {
				continue
			}
			o := img.PixOffset(x, y)
			for k, v := range [3]float64{66, 133, 244} {
				img.Pix[o+k] = uint8(float64(img.Pix[o+k])*(1-a) + v*a)
			}
		}
	}
}
