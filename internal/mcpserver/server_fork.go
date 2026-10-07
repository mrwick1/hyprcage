package mcpserver

import (
	"context"
	"fmt"
	"image"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/record"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/session"
)

type recordIn struct {
	Target string `json:"target,omitempty" jsonschema:"a screen name, or \"desktop\" for the human's own screen; optional when the session owns exactly one screen"`
}

type recordStartIn struct {
	Target string `json:"target,omitempty" jsonschema:"a screen name, or \"desktop\" for the human's own screen; optional when the session owns exactly one screen"`
	Crop string `json:"crop,omitempty" jsonschema:"viewport records only the page of the screen's browser, without tabs and address bar; none records the whole screen; default: record.crop in the config (viewport)"`
}

// registerFork adds the tools of the mrwick1 fork.
func (s *Server) registerFork(srv *mcp.Server) {
	tool(s, srv, "record_start", "Start recording a screen (or the human's desktop with target=\"desktop\") to an MP4 file. On a screen with a browser opened by browser_open or app_launch debug=true it records only the page viewport by default (crop). It stops by itself after the configured maximum (30 min by default), when the screen closes, or with record_stop. Desktop recording is refused while the human's session is locked.", s.recordStart)
	tool(s, srv, "record_stop", "Stop a recording and return the path of the MP4 file.", s.recordStop)
	s.registerClip(srv)
	s.registerDesktop(srv)
	s.registerBrowser(srv)
	s.registerNotify(srv)
}

func (s *Server) recordTarget(target string) (string, bool, error) {
	if target == record.Desktop {
		return record.Desktop, false, nil
	}
	rec, _, err := s.resolve(target, false)
	if err != nil {
		return "", false, err
	}
	return rec.Name, true, nil
}

func (s *Server) recordStart(in recordStartIn) (*mcp.CallToolResult, error) {
	target, isScreen, err := s.recordTarget(in.Target)
	if err != nil {
		return nil, err
	}
	mode := in.Crop
	if mode == "" {
		mode = s.cfg.RecordCrop
	}
	var crop image.Rectangle
	switch mode {
	case "none":
	case "viewport":
		if crop, err = s.viewport(target, isScreen); err != nil {
			// A screen without a DevTools browser, or the desktop, has no
			// viewport to crop to; the default then records it whole.
			if in.Crop != "" {
				return nil, err
			}
			mode = "none"
		}
	default:
		return nil, fmt.Errorf("crop: %q is not viewport or none", in.Crop)
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, screen.Errf(screen.CodeCapture, "", "%v", err)
	}
	st, err := record.Start(exe, target, isScreen, session.Current().Owner(), s.cfg, crop)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"target": st.Target, "path": st.Path, "max": s.cfg.RecordMax.String(), "crop": mode}
	if mode == "viewport" {
		out["region"] = record.FormatRect(crop)
	}
	return textResult(out), nil
}

// viewport is the page area of a screen's browser.
func (s *Server) viewport(target string, isScreen bool) (image.Rectangle, error) {
	if !isScreen {
		return image.Rectangle{}, screen.Errf(screen.CodeUnsupported, "pass crop none", "the desktop has no viewport to crop to")
	}
	ctx, cancel := context.WithTimeout(context.Background(), perceiveTimeout)
	defer cancel()
	_, d, err := s.devtoolsScreen(ctx, target)
	if err != nil {
		return image.Rectangle{}, err
	}
	return d.Viewport(ctx)
}

func (s *Server) recordStop(in recordIn) (*mcp.CallToolResult, error) {
	target, _, err := s.recordTarget(in.Target)
	if err != nil {
		return nil, err
	}
	st, err := record.Load(target)
	if err != nil {
		return nil, err
	}
	if err := record.CheckOwner(st, session.Current()); err != nil {
		return nil, err
	}
	st, err = record.Stop(target)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"target": st.Target, "path": st.Path}
	if fi, err := os.Stat(st.Path); err == nil {
		out["bytes"] = fi.Size()
	}
	return textResult(out), nil
}
