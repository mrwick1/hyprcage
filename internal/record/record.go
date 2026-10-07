// Package record turns a screen or the human's desktop into an MP4 file:
// hyprcage captures frames through wlr-screencopy and pipes raw RGBA to
// ffmpeg. wf-recorder is not used: on openSUSE it fails with a libavformat
// symbol error.
package record

import (
	"context"
	"fmt"
	"image"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/hexadecimil/hyprcage/internal/screen"
)

// Capturer grabs one frame. *wl.Client satisfies it.
type Capturer interface {
	Capture(cursor bool) (*image.RGBA, error)
}

// Encoder picks the first H.264 encoder in the output of `ffmpeg -encoders`,
// else mpeg4, which every ffmpeg build has. Arch ships libx264, openSUSE
// only libopenh264.
func Encoder(encodersOutput string) string {
	for _, e := range []string{"libx264", "libopenh264"} {
		if strings.Contains(encodersOutput, " "+e+" ") {
			return e
		}
	}
	return "mpeg4"
}

// FFmpegArgs is the ffmpeg command that reads raw w x h RGBA frames on
// stdin and writes out. A non-empty crop keeps only that part of the frame.
// The scale filter rounds the size down to even numbers, which yuv420p
// needs.
func FFmpegArgs(w, h, fps int, crop image.Rectangle, encoder, out string) []string {
	vf := "scale=trunc(iw/2)*2:trunc(ih/2)*2"
	if crop = crop.Intersect(image.Rect(0, 0, w, h)); !crop.Empty() {
		vf = fmt.Sprintf("crop=%d:%d:%d:%d,%s", crop.Dx(), crop.Dy(), crop.Min.X, crop.Min.Y, vf)
	}
	args := []string{"ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "rawvideo", "-pix_fmt", "rgba", "-s", fmt.Sprintf("%dx%d", w, h), "-framerate", strconv.Itoa(fps), "-i", "-",
		"-vf", vf, "-c:v", encoder}
	args = append(args, quality(encoder)...)
	return append(args, "-pix_fmt", "yuv420p", out)
}

// quality keeps small UI text sharp. The ffmpeg defaults (CRF 23 for x264)
// blur it. libopenh264 and mpeg4 have no CRF, so they get a high bitrate
// and a low quantizer.
func quality(encoder string) []string {
	switch encoder {
	case "libx264":
		return []string{"-crf", "18", "-preset", "medium", "-tune", "stillimage"}
	case "libopenh264":
		return []string{"-b:v", "6M"}
	}
	return []string{"-q:v", "2"}
}

// Loop writes first, then one capture per frame period, to w until ctx
// ends or max elapses. When a capture comes late, the previous image is
// written again, so the video keeps real time. When idle is above zero, a
// pause in which less than threshold percent of the pixels change stays in
// the video for idle at most, and the rest of it is cut. It returns the
// number of frames written. A frame is always written whole.
func Loop(ctx context.Context, c Capturer, w io.Writer, fps int, max, idle time.Duration, threshold float64, first *image.RGBA) (int, error) {
	begin := time.Now()
	start := begin // the clock of the video; a cut moves it forward
	period := time.Second / time.Duration(fps)
	img, written := first, 0
	changed, last := begin, begin // the last change, the last capture
	for {
		due := int(time.Since(start)/period) + 1
		for ; written < due; written++ {
			if err := writeFrame(w, img); err != nil {
				return written, err
			}
		}
		if time.Since(begin) >= max {
			return written, nil
		}
		select {
		case <-ctx.Done():
			return written, nil
		case <-time.After(time.Until(start.Add(time.Duration(written) * period))):
		}
		next, err := c.Capture(true)
		if err != nil {
			return written, err
		}
		if next.Bounds() != first.Bounds() {
			return written, fmt.Errorf("record: the screen changed size to %v", next.Bounds().Size())
		}
		now := time.Now()
		if idle <= 0 || screen.ChangedPercent(img, next) > threshold {
			changed = now
		} else if now.Sub(changed) > idle {
			start = start.Add(now.Sub(last))
		}
		img, last = next, now
	}
}

// FormatRect and ParseRect carry a crop to the recorder process as x,y,w,h.
func FormatRect(r image.Rectangle) string {
	return fmt.Sprintf("%d,%d,%d,%d", r.Min.X, r.Min.Y, r.Dx(), r.Dy())
}

func ParseRect(s string) (image.Rectangle, error) {
	var x, y, w, h int
	if _, err := fmt.Sscanf(s, "%d,%d,%d,%d", &x, &y, &w, &h); err != nil {
		return image.Rectangle{}, fmt.Errorf("crop %q is not x,y,w,h: %w", s, err)
	}
	return image.Rect(x, y, x+w, y+h), nil
}

func writeFrame(w io.Writer, img *image.RGBA) error {
	b := img.Bounds()
	row := b.Dx() * 4
	for y := 0; y < b.Dy(); y++ {
		off := y * img.Stride
		if _, err := w.Write(img.Pix[off : off+row]); err != nil {
			return err
		}
	}
	return nil
}
