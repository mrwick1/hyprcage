package mcpserver

import (
	"context"
	"time"

	"github.com/hexadecimil/offstage/internal/headless"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/screen"
)

// Keep connections to legacy headless records until their screens are destroyed.
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
		return nil, screen.Errf(screen.CodeCDP, "offstage destroy "+rec.Name, "cannot reach the browser of screen %s: %v", rec.Name, err)
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
