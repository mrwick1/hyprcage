package mcpserver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/browser"
	"github.com/hexadecimil/hyprcage/internal/headless"
	"github.com/hexadecimil/hyprcage/internal/notify"
	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/session"
)

// screenKind returns the kind that screen_create makes: what the caller
// asked for, else cage on Linux and a headless browser elsewhere.
func screenKind(asked string) (string, error) {
	switch asked {
	case "":
		if runtime.GOOS == "linux" {
			return screen.KindCage, nil
		}
		return screen.KindHeadless, nil
	case screen.KindCage:
		if runtime.GOOS != "linux" {
			return "", screen.Errf(screen.CodeUnsupportedKind, `use kind "headless"`, "kind cage needs Linux")
		}
	case screen.KindHeadless:
	default:
		return "", screen.Errf(screen.CodeUnsupportedKind, `kind is "cage" or "headless"`, "unknown kind %q", asked)
	}
	return asked, nil
}

// createHeadless makes a screen that is one headless Chrome. The record
// comes first, as for a cage, so that a crash leaves something for gc.
func (s *Server) createHeadless(c *screen.Ctx, opts screen.CreateOptions) (*registry.Screen, error) {
	chrome, err := browser.Find(s.cfg.BrowserCommand, exec.LookPath)
	if err != nil && s.cfg.BrowserCommand == "" {
		if p, e := headless.FindChrome(); e == nil { // the install folders of Windows
			chrome, err = p, nil
		}
	}
	if err != nil {
		return nil, err
	}
	release, err := screen.Reserve(c, &opts)
	if err != nil {
		return nil, err
	}
	defer release()
	rec := &registry.Screen{
		Name: opts.Name, CreatedAt: time.Now(), State: "starting", Version: screen.RecordVersion, Kind: screen.KindHeadless,
		Width: opts.Width, Height: opts.Height, MirrorNote: "a headless screen has no mirror window", Owner: opts.Owner,
	}
	if err := registry.Save(rec); err != nil {
		return nil, err
	}
	fail := func(err error) (*registry.Screen, error) {
		_ = screen.Destroy(c, rec)
		return nil, err
	}
	// screen.Destroy removes the profile by its prefix.
	profile, err := os.MkdirTemp("", screen.ProfilePrefix(opts.Name))
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b, err := headless.Start(ctx, headless.Options{Chrome: chrome, Width: opts.Width, Height: opts.Height, ProfileDir: profile})
	if err != nil {
		return fail(err)
	}
	rec.DebugPort, rec.DebugPID = b.Port(), b.PID()
	rec.DebugPIDStart, _ = session.ProcStart(rec.DebugPID)
	rec.State = "ready"
	if err := registry.Save(rec); err != nil {
		b.Close()
		return fail(err)
	}
	s.cacheBrowser(rec, b)
	if c.Cfg.Notify {
		notify.Send("Agent screen opened", rec.Name)
	}
	return rec, nil
}

func (s *Server) cacheBrowser(rec *registry.Screen, b *headless.Browser) {
	if s.browsers == nil {
		s.browsers = map[string]*headless.Browser{}
	}
	s.browsers[tableKey(rec)] = b
}

// browserFor returns the Browser of a headless screen. It dials the screen's
// Chrome again when this process has none, or when its connection died, so
// that a restarted server or another process can drive the screen.
func (s *Server) browserFor(rec *registry.Screen) (*headless.Browser, error) {
	if b := s.browsers[tableKey(rec)]; b != nil && b.Alive() {
		return b, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, err := headless.Attach(ctx, rec.DebugPort, rec.DebugPID, rec.Width, rec.Height)
	if err != nil {
		return nil, screen.Errf(screen.CodeCDP, "hyprcage destroy "+rec.Name, "cannot reach the browser of screen %s: %v", rec.Name, err)
	}
	s.cacheBrowser(rec, b)
	return b, nil
}

// dropBrowser closes the Browser of a screen and kills its Chrome.
func (s *Server) dropBrowser(rec *registry.Screen) {
	k := tableKey(rec)
	if b := s.browsers[k]; b != nil {
		b.Close()
		delete(s.browsers, k)
	}
}

// headlessOpen navigates the Chrome that screen_create started: it opens no
// browser. The reply has the shape of the cage's.
func (s *Server) headlessOpen(rec *registry.Screen, in browserIn) (*mcp.CallToolResult, error) {
	if in.Profile != "" {
		return nil, screen.Errf(screen.CodeUnsupportedKind, "leave profile out", "headless screen %s has a throwaway profile", rec.Name)
	}
	if err := browser.CheckURL(in.URL); err != nil {
		return nil, err
	}
	b, err := s.browserFor(rec)
	if err != nil {
		return nil, err
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", rec.DebugPort)
	ws, err := browser.Ready(base, 5*time.Second)
	if err != nil {
		return nil, err
	}
	s.startDevtools(rec) // before the load, so the console and the network see it
	if in.URL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), perceiveTimeout)
		defer cancel()
		if err := b.Navigate(ctx, in.URL); err != nil {
			return nil, err
		}
	}
	return textResult(browser.Info{Screen: rec.Name, PID: rec.DebugPID, Port: rec.DebugPort, BrowserURL: base, WS: ws}), nil
}
