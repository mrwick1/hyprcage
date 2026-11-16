package mcpserver

// Legacy CDP fixtures exercise native-app diagnostics. They do not register
// Chrome browser tools or provide a runtime fallback for Bare.
import (
	"context"
	"crypto/rand"
	"github.com/hexadecimil/offstage/internal/headless"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/screen"
	"github.com/hexadecimil/offstage/internal/session"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
	"testing"
	"time"
)

func requireChrome(t *testing.T) {
	t.Helper()
	if _, err := headless.FindChrome(); err != nil {
		t.Skip("native CDP fixture: " + err.Error())
	}
}
func createHeadlessScreen(t *testing.T, cs *mcp.ClientSession) *registry.Screen {
	t.Helper()
	bin, err := headless.FindChrome()
	if err != nil {
		t.Skip(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	b, err := headless.Start(ctx, headless.Options{Chrome: bin, Width: 1280, Height: 800, ProfileDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	rec := &registry.Screen{Name: "hc-native-" + strings.ToLower(rand.Text()[:12]), CreatedAt: time.Now(), State: "ready", Kind: screen.KindHeadless, Version: screen.RecordVersion, Width: 1280, Height: 800, Owner: session.Current().Owner(), DebugPort: b.Port(), DebugPID: b.PID()}
	rec.DebugPIDStart, _ = session.ProcStart(b.PID())
	t.Cleanup(func() { hcall(t, cs, "screen_destroy", map[string]any{"screen": rec.Name}); b.Close() })
	if err := registry.Save(rec); err != nil {
		t.Fatal(err)
	}
	return rec
}
func openNativeTestPage(t *testing.T, s *Server, rec *registry.Screen, url string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, err := s.browserFor(rec)
	if err != nil {
		t.Fatal(err)
	}
	if s.startDevtools(rec) == nil {
		t.Fatal("native diagnostics unavailable")
	}
	if err := b.Navigate(ctx, url); err != nil {
		t.Fatal(err)
	}
}
func evalNativeTestPage(t *testing.T, s *Server, rec *registry.Screen, expression string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d, err := s.devtoolsFor(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Eval(ctx, "", expression, 5*time.Second); err != nil {
		t.Fatal(err)
	}
}
