package mcpserver

import (
	"context"
	"time"

	"github.com/hexadecimil/offstage/internal/devtools"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/screen"
)

// startDevtools connects at launch, so the console holds the page's
// messages from the start. Snapshot and recording can retry a failed connection.
func (s *Server) startDevtools(rec *registry.Screen) *devtools.Session {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d, _ := s.devtoolsFor(ctx, rec)
	return d
}

// devtoolsFor returns the screen's DevTools session, connecting when there
// is none or the app behind it changed. The port must still be held by the
// process that opened it, so a stranger's browser is never reached.
func (s *Server) devtoolsFor(ctx context.Context, rec *registry.Screen) (*devtools.Session, error) {
	if rec.DebugPort == 0 || !screen.OwnsPort(rec) {
		s.dropDevtools(rec)
		return nil, screen.Errf(screen.CodeCDP, "launch the native app with debug=true; use crop none for a Bare recording",
			"screen %s has no app with a DevTools port", rec.Name)
	}
	k := tableKey(rec)
	if d := s.devtools[k]; d != nil && d.Alive() && d.Port() == rec.DebugPort {
		return d, nil
	}
	s.dropDevtools(rec)
	d, err := devtools.Open(ctx, rec.DebugPort)
	if err != nil {
		return nil, err
	}
	if s.devtools == nil {
		s.devtools = map[string]*devtools.Session{}
	}
	s.devtools[k] = d
	return d, nil
}

func (s *Server) dropDevtools(rec *registry.Screen) {
	k := tableKey(rec)
	if d := s.devtools[k]; d != nil {
		d.Close()
		delete(s.devtools, k)
	}
}

// devtoolsScreen resolves the screen and its session.
func (s *Server) devtoolsScreen(ctx context.Context, name string) (*registry.Screen, *devtools.Session, error) {
	rec, _, err := s.resolve(name, false)
	if err != nil {
		return nil, nil, err
	}
	d, err := s.devtoolsFor(ctx, rec)
	return rec, d, err
}
