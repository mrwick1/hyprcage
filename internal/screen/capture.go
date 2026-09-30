package screen

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"regexp"
	"time"

	"github.com/hexadecimil/hyprcage/internal/wl"
)

// Rect is a region in screen pixels.
type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// ShotOptions are the parameters of screenshot (cahier F3).
type ShotOptions struct {
	Scale    float64 // 0 or 1 = full size; 0.25..1
	Region   *Rect
	Format   string // "png" (default) or "jpeg"
	Cursor   bool
	MaxSide  int // hard cap per side, default 2000
	MaxBytes int // encoded size above which png falls back to jpeg, default 1 MiB
}

// ShotResult is an encoded capture with its geometry.
type ShotResult struct {
	Data    []byte  `json:"-"`
	MIME    string  `json:"mime"`
	Width   int     `json:"width"`
	Height  int     `json:"height"`
	Scale   float64 `json:"scale"`
	ScreenW int     `json:"screen_width"`
	ScreenH int     `json:"screen_height"`
	Region  *Rect   `json:"region,omitempty"`
	// Origin and LogicalPerPixel place a desktop capture in the global
	// logical layout: Origin is the logical position of capture pixel 0,0,
	// and LogicalPerPixel the logical size of one capture pixel.
	Origin          *[2]int `json:"origin,omitempty"`
	LogicalPerPixel float64 `json:"logical_per_pixel,omitempty"`
}

// Shot captures the screen, crops, scales within the budget and encodes.
func Shot(cl *wl.Client, o ShotOptions) (*ShotResult, error) {
	img, err := cl.Capture(o.Cursor)
	if err != nil {
		return nil, err
	}
	return ShotImage(img, o)
}

// ShotImage crops, scales within the budget and encodes a capture.
func ShotImage(img *image.RGBA, o ShotOptions) (*ShotResult, error) {
	res := &ShotResult{ScreenW: img.Bounds().Dx(), ScreenH: img.Bounds().Dy(), Scale: 1}
	if o.Region != nil {
		r := image.Rect(o.Region.X, o.Region.Y, o.Region.X+o.Region.W, o.Region.Y+o.Region.H).Intersect(img.Bounds())
		if r.Empty() {
			return nil, fmt.Errorf("region %+v is outside the %dx%d screen", *o.Region, res.ScreenW, res.ScreenH)
		}
		img = cropRGBA(img, r)
		res.Region = o.Region
	}
	scale := o.Scale
	if scale <= 0 || scale > 1 {
		scale = 1
	}
	maxSide := o.MaxSide
	if maxSide <= 0 {
		maxSide = 2000
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if long := max(w, h); float64(long)*scale > float64(maxSide) {
		scale = float64(maxSide) / float64(long)
	}
	if scale < 1 {
		dw, dh := max(1, int(float64(w)*scale+0.5)), max(1, int(float64(h)*scale+0.5))
		img = resizeBox(img, dw, dh)
	}
	res.Width, res.Height, res.Scale = img.Bounds().Dx(), img.Bounds().Dy(), scale

	maxBytes := o.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	format := o.Format
	if format == "" {
		format = "png"
	}
	var buf bytes.Buffer
	switch format {
	case "png":
		enc := png.Encoder{CompressionLevel: png.BestSpeed}
		if err := enc.Encode(&buf, img); err != nil {
			return nil, err
		}
		res.MIME = "image/png"
		if buf.Len() <= maxBytes {
			break
		}
		fallthrough
	case "jpeg", "jpg":
		for _, q := range []int{85, 70, 55} {
			buf.Reset()
			if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
				return nil, err
			}
			if buf.Len() <= maxBytes {
				break
			}
		}
		res.MIME = "image/jpeg"
	default:
		return nil, fmt.Errorf("unknown format %q (png, jpeg)", format)
	}
	if buf.Len() > maxBytes {
		return nil, errf("image_too_large", "lower scale or capture a region", "encoded capture is %d bytes, budget %d", buf.Len(), maxBytes)
	}
	res.Data = buf.Bytes()
	return res, nil
}

func cropRGBA(src *image.RGBA, r image.Rectangle) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := 0; y < r.Dy(); y++ {
		so := src.PixOffset(r.Min.X, r.Min.Y+y)
		copy(dst.Pix[y*dst.Stride:y*dst.Stride+r.Dx()*4], src.Pix[so:so+r.Dx()*4])
	}
	return dst
}

// resizeBox downscales with a box filter (area average), good enough for
// screenshots and free of dependencies.
func resizeBox(src *image.RGBA, dw, dh int) *image.RGBA {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for dy := 0; dy < dh; dy++ {
		y0, y1 := dy*sh/dh, max(dy*sh/dh+1, (dy+1)*sh/dh)
		for dx := 0; dx < dw; dx++ {
			x0, x1 := dx*sw/dw, max(dx*sw/dw+1, (dx+1)*sw/dw)
			var r, g, b, n int
			for y := y0; y < y1; y++ {
				o := src.PixOffset(x0, y)
				for x := x0; x < x1; x++ {
					r += int(src.Pix[o])
					g += int(src.Pix[o+1])
					b += int(src.Pix[o+2])
					o += 4
					n++
				}
			}
			d := dst.PixOffset(dx, dy)
			dst.Pix[d], dst.Pix[d+1], dst.Pix[d+2], dst.Pix[d+3] = uint8(r/n), uint8(g/n), uint8(b/n), 255
		}
	}
	return dst
}

// changedPercent is the share (in percent) of pixels whose colour moved by
// more than a small tolerance between two equally sized images. A global
// mean would miss one new line in a terminal; counting pixels does not.
func changedPercent(a, b *image.RGBA) float64 {
	if len(a.Pix) != len(b.Pix) || len(a.Pix) == 0 {
		return 100
	}
	const tolerance = 12
	changed := 0
	for i := 0; i < len(a.Pix); i += 4 {
		for k := 0; k < 3; k++ {
			d := int(a.Pix[i+k]) - int(b.Pix[i+k])
			if d > tolerance || d < -tolerance {
				changed++
				break
			}
		}
	}
	return 100 * float64(changed) / float64(len(a.Pix)/4)
}

// WaitStable returns once the screen, sampled at 10 Hz without the cursor
// and reduced 8x, has had less than threshold percent of its pixels change
// for stable (cahier F6). The default tolerates a blinking text cursor but
// not a new line of text.
func WaitStable(cl *wl.Client, stable, timeout time.Duration, threshold float64) error {
	if threshold <= 0 {
		threshold = 0.02
	}
	deadline := time.Now().Add(timeout)
	var prev *image.RGBA
	var quietSince time.Time
	for {
		img, err := cl.Capture(false)
		if err != nil {
			return err
		}
		small := resizeBox(img, max(1, img.Bounds().Dx()/8), max(1, img.Bounds().Dy()/8))
		now := time.Now()
		if prev != nil && changedPercent(prev, small) <= threshold {
			if quietSince.IsZero() {
				quietSince = now
			}
			if now.Sub(quietSince) >= stable {
				return nil
			}
		} else {
			quietSince = time.Time{}
		}
		prev = small
		if now.After(deadline) {
			return errf(CodeTimeout, "the screen keeps changing; raise timeout_ms or stable_threshold", "not stable within %s", timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// CloseWindow asks a window to close and returns the id it actually acted
// on. A window id is a Wayland object id, and a new one is handed out when a
// window remaps, which cage does when it puts a freshly opened window
// fullscreen: the id a caller noted at launch can already be stale. When the
// id is unknown and the screen holds exactly one window, that one is closed,
// which covers the whole of the case under a single-application kiosk.
func CloseWindow(cl *wl.Client, id uint32) (uint32, error) {
	err := cl.CloseToplevel(id)
	if err == nil {
		return id, nil
	}
	tls, lerr := cl.Toplevels()
	if lerr != nil || len(tls) != 1 {
		return 0, err
	}
	if cerr := cl.CloseToplevel(tls[0].ID); cerr != nil {
		return 0, err
	}
	return tls[0].ID, nil
}

// WaitTitle returns once a window title matches re.
func WaitTitle(cl *wl.Client, re *regexp.Regexp, timeout time.Duration) (*wl.Toplevel, error) {
	deadline := time.Now().Add(timeout)
	for {
		tls, err := cl.Toplevels()
		if err != nil {
			return nil, err
		}
		for i := range tls {
			if re.MatchString(tls[i].Title) {
				return &tls[i], nil
			}
		}
		if time.Now().After(deadline) {
			return nil, errf(CodeTimeout, "", "no window title matching %s within %s", re, timeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
