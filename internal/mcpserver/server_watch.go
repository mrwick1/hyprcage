package mcpserver

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/desktop"
	"github.com/hexadecimil/hyprcage/internal/perceive"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

type watchIn struct {
	Screen       string `json:"screen,omitempty"`
	Seconds      int    `json:"seconds,omitempty" jsonschema:"how long to watch, default 5, max 30"`
	FPS          int    `json:"fps,omitempty" jsonschema:"frames per second, default 1, max 4"`
	ExpectChange bool   `json:"expect_change,omitempty" jsonschema:"set true when the screen should be changing: a screen that never changes then returns frozen"`
	Path         string `json:"path,omitempty" jsonschema:"directory to write every frame to as PNG; without it the result holds three thumbnails (first, middle, last)"`
}

func (s *Server) registerWatch(srv *mcp.Server) {
	tool(s, srv, "watch", "Sample a screen for a few seconds and return a verdict: stable (nothing changes), jitter (most frames change, for example a spinner) or frozen (nothing changes although expect_change is true), with the per-frame change ratios. Agent screens only.", s.watch)
}

// watchParams returns the frame count and the interval for in.
func watchParams(in watchIn) (int, time.Duration) {
	sec, fps := in.Seconds, in.FPS
	if sec <= 0 {
		sec = 5
	}
	if fps <= 0 {
		fps = 1
	}
	sec, fps = min(sec, 30), min(fps, 4)
	return sec * fps, time.Second / time.Duration(fps)
}

func (s *Server) watch(in watchIn) (*mcp.CallToolResult, error) {
	if desktop.IsDesktop(in.Screen) {
		return nil, fmt.Errorf("watch applies to agent screens only")
	}
	rec, cl, err := s.resolve(in.Screen, true)
	if err != nil {
		return nil, err
	}
	n, iv := watchParams(in)
	res, err := perceive.Watch(func() (*image.RGBA, error) { return cl.Capture(false) }, n, func() { time.Sleep(iv) }, in.ExpectChange)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"screen": rec.Name, "verdict": res.Verdict, "frames": len(res.Frames), "ratios": res.Ratios}
	var images []mcp.Content
	if in.Path != "" {
		if err := os.MkdirAll(in.Path, 0o755); err != nil {
			return nil, err
		}
		files := []string{}
		for i, f := range res.Frames {
			name := filepath.Join(in.Path, fmt.Sprintf("frame-%03d.png", i))
			w, err := os.Create(name)
			if err != nil {
				return nil, err
			}
			err = png.Encode(w, f)
			if cerr := w.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return nil, err
			}
			files = append(files, name)
		}
		out["files"] = files
	} else {
		for _, i := range []int{0, len(res.Frames) / 2, len(res.Frames) - 1} {
			th, err := screen.ShotImage(res.Frames[i], screen.ShotOptions{Scale: 0.25, MaxSide: s.cfg.ShotMaxSide, MaxBytes: s.cfg.ShotMaxBytes})
			if err != nil {
				return nil, err
			}
			images = append(images, &mcp.ImageContent{Data: th.Data, MIMEType: th.MIME})
		}
	}
	r := textResult(out)
	r.Content = append(r.Content, images...)
	return r, nil
}
