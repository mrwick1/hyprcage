package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/session"
)

func quiet(s *Server) { s.activity.lastDone.Store(time.Now().Add(-time.Minute).UnixNano()) }

func TestBusyGate(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	s := newServer(config.Default())
	ctx := context.Background()
	s.activity.begin()
	s.activity.end()
	if s.busy(ctx) == "" {
		t.Fatal("a request that just ended did not block")
	}
	quiet(s)
	if r := s.busy(ctx); r != "" {
		t.Fatalf("idle server is busy: %s", r)
	}
	s.activity.begin()
	if s.busy(ctx) == "" {
		t.Fatal("a request in flight did not block")
	}
	s.activity.end()
	quiet(s)
	if s.busy(ctx) != "" {
		t.Fatal("still busy after the request ended")
	}
	// A screen of this session blocks; one of another session does not.
	id := session.Current()
	if err := registry.Save(&registry.Screen{Name: "other", State: "ready", Owner: registry.Owner{SessionID: "someone-else", PID: 1, PIDStart: 1}}); err != nil {
		t.Fatal(err)
	}
	if r := s.busy(ctx); r != "" {
		t.Fatalf("a screen of another session blocked: %s", r)
	}
	if err := registry.Save(&registry.Screen{Name: "mine", State: "ready", Owner: id.Owner()}); err != nil {
		t.Fatal(err)
	}
	if r := s.busy(ctx); !strings.Contains(r, "mine") {
		t.Fatalf("own screen did not block: %q", r)
	}
}

func TestNoticeOnNextToolResultOnce(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	s := newServer(config.Default())
	srv := s.mcpServer()
	srv.AddReceivingMiddleware(s.activity.middleware)
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(context.Background(), st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	stage("offstage updated to v9.9.9", true)
	t.Cleanup(func() { updateNotice.Store(nil) })
	first := call(t, cs, "screen_list", nil)
	if !strings.Contains(first, "offstage updated to v9.9.9; the server restarts when idle.") {
		t.Fatalf("first result lacks the notice: %q", first)
	}
	if second := call(t, cs, "screen_list", nil); strings.Contains(second, "offstage updated") {
		t.Fatalf("notice repeated: %q", second)
	}
}

func TestSelfUpdateRestartsWhenIdleAfterDiskChange(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("OFFSTAGE_UPDATE_DELAY", "1h")
	s := newServer(config.Default())
	quiet(s)
	restartForTest = true
	defer func() { restartForTest = false }()
	restarted := make(chan struct{})
	orig := versionForTest
	versionForTest = "v0.0.1"
	defer func() { versionForTest = orig }()
	// The test binary stands in for the install: touch it to look like a new build on disk.
	dir := t.TempDir()
	fake := filepath.Join(dir, "offstage")
	os.WriteFile(fake, []byte("a"), 0o755)
	exeForTest = fake
	defer func() { exeForTest = "" }()
	go s.selfUpdate(context.Background(), func() { close(restarted) })
	time.Sleep(200 * time.Millisecond)
	os.WriteFile(fake, []byte("bb"), 0o755) // another server replaced the file
	select {
	case <-restarted:
	case <-time.After(15 * time.Second):
		t.Fatal("no restart after the binary changed on disk")
	}
}

func TestSelfUpdateOffDoesNothing(t *testing.T) {
	cases := map[string]func(*Server){
		"config":   func(s *Server) { s.cfg.Update.Auto = false },
		"env":      func(s *Server) { t.Setenv("OFFSTAGE_NO_AUTO_UPDATE", "1") },
		"checkout": func(*Server) { versionForTest = "v0.11.0-5-g1a2b3c4" },
	}
	for name, off := range cases {
		t.Run(name, func(t *testing.T) {
			versionForTest = "v0.0.1"
			defer func() { versionForTest = "" }()
			s := newServer(config.Default())
			off(s)
			done := make(chan struct{})
			go func() { s.selfUpdate(context.Background(), func() { t.Error("restart called") }); close(done) }()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("selfUpdate kept running")
			}
		})
	}
}

func TestSelfUpdateNoRestartForCodex(t *testing.T) {
	updateNotice.Store(nil)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("OFFSTAGE_UPDATE_DELAY", "1h")
	t.Setenv("CLAUDECODE", "")
	t.Setenv("CLAUDE_PID", "")
	versionForTest = "v0.0.1"
	defer func() { versionForTest = "" }()
	fake := filepath.Join(t.TempDir(), "offstage")
	os.WriteFile(fake, []byte("a"), 0o755)
	exeForTest = fake
	defer func() { exeForTest = "" }()
	s := newServer(config.Default())
	quiet(s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.selfUpdate(ctx, func() { t.Error("restart called for a client that does not respawn") })
	time.Sleep(200 * time.Millisecond)
	os.WriteFile(fake, []byte("bb"), 0o755)
	time.Sleep(7 * time.Second)
	t.Cleanup(func() { updateNotice.Store(nil) })
	if n := updateNotice.Load(); n == nil || !strings.Contains(*n, "restart the MCP server") {
		t.Fatalf("notice = %v", n)
	}
}
