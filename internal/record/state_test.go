package record

import (
	"errors"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/session"
)

func TestStateRoundTripAndDeadCleanup(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	live := State{Target: "hc-a", PID: os.Getpid(), Path: "/tmp/a.mp4", Started: time.Now()}
	dead := State{Target: "desktop", PID: 1 << 30, Path: "/tmp/d.mp4", Started: time.Now()}
	for _, s := range []State{live, dead} {
		if err := save(s); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Load("hc-a")
	if err != nil || got.Path != live.Path {
		t.Fatalf("Load: %+v %v", got, err)
	}
	list, err := List()
	if err != nil || len(list) != 1 || list[0].Target != "hc-a" {
		t.Fatalf("List keeps only live recorders: %+v %v", list, err)
	}
	if _, err := os.Stat(StatePath("desktop")); !os.IsNotExist(err) {
		t.Error("the dead recorder's state file is still there")
	}
}

func TestStopWithoutRecording(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if _, err := Stop("hc-none"); err == nil {
		t.Fatal("want not_recording")
	}
}

func TestStartFailureLeavesLog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	exe := filepath.Join(dir, "fake")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho boom-startup >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Fallback()
	cfg.RecordDir = dir
	_, err := Start(exe, "hc-t", true, registry.Owner{}, cfg, image.Rectangle{})
	var se *screen.Error
	if !errors.As(err, &se) || se.Code != screen.CodeCapture {
		t.Fatalf("want capture_failed, got %v", err)
	}
	logPath := filepath.Join(screen.LogDir(), "record-hc-t.log")
	if !strings.Contains(se.Hint, logPath) {
		t.Errorf("hint %q does not name %s", se.Hint, logPath)
	}
	if data, _ := os.ReadFile(logPath); !strings.Contains(string(data), "boom-startup") {
		t.Errorf("log lacks the child's error: %q", data)
	}
}

func TestRecordingStateIsNotAScreen(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if err := save(State{Target: "hc-a", PID: os.Getpid(), Path: "/tmp/a.mp4", Started: time.Now()}); err != nil {
		t.Fatal(err)
	}
	screens, err := registry.List()
	if err != nil || len(screens) != 0 {
		t.Fatalf("registry.List sees the recording state as a screen: %+v %v", screens, err)
	}
	list, err := List()
	if err != nil || len(list) != 1 || list[0].Target != "hc-a" {
		t.Fatalf("record.List: %+v %v", list, err)
	}
}

func TestStartPassesOwnerAndStripsScreen(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HYPRCAGE_SCREEN", "hc-inherited")
	old := lockedFn
	lockedFn = func() bool { return false }
	defer func() { lockedFn = old }()
	envOut := filepath.Join(dir, "env")
	exe := filepath.Join(dir, "fake")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nenv > "+envOut+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Fallback()
	cfg.RecordDir = filepath.Join(dir, "rec")
	_, _ = Start(exe, Desktop, false, registry.Owner{SessionID: "s1", PID: 42}, cfg, image.Rectangle{})
	data, _ := os.ReadFile(envOut)
	if strings.Contains(string(data), "HYPRCAGE_SCREEN=") {
		t.Error("the desktop recorder inherits HYPRCAGE_SCREEN")
	}
	if !strings.Contains(string(data), ownerEnv+`={"session_id":"s1"`) {
		t.Errorf("the recorder lacks the owner: %q", data)
	}
	t.Setenv(ownerEnv, `{"session_id":"s1","pid":42}`)
	if o := ownerFromEnv(); o.SessionID != "s1" || o.PID != 42 {
		t.Errorf("ownerFromEnv: %+v", o)
	}
	if fi, err := os.Stat(cfg.RecordDir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("record dir mode: %v %v", fi, err)
	}
}

func TestCheckOwner(t *testing.T) {
	old := ownerAlive
	defer func() { ownerAlive = old }()
	st := State{Target: Desktop, Owner: registry.Owner{SessionID: "a", PID: 7}}
	for _, c := range []struct {
		owner, caller string
		alive, ok     bool
	}{
		{"a", "a", true, true},
		{"a", "b", true, false},
		{"a", "b", false, true},
		{"", "b", true, true},
		{"a", "", true, true},
		{"", "", true, true},
	} {
		ownerAlive = func(int, uint64) bool { return c.alive }
		st.Owner.SessionID = c.owner
		err := CheckOwner(st, session.Identity{SessionID: c.caller})
		var se *screen.Error
		if c.ok != (err == nil) || (err != nil && (!errors.As(err, &se) || se.Code != screen.CodeNotOwner)) {
			t.Errorf("owner %q caller %q alive %v: %v", c.owner, c.caller, c.alive, err)
		}
	}
}

func TestStopSession(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = cmd.Wait() }()
	defer func() { _ = cmd.Process.Kill() }()
	if err := save(State{Target: Desktop, PID: cmd.Process.Pid, Owner: registry.Owner{SessionID: "s1"}}); err != nil {
		t.Fatal(err)
	}
	if stopped, err := StopSession("s2"); stopped || err != nil {
		t.Fatalf("another session stops the recording: %v %v", stopped, err)
	}
	if stopped, err := StopSession("s1"); !stopped || err != nil {
		t.Fatalf("StopSession(s1): %v %v", stopped, err)
	}
	if alive(cmd.Process.Pid) {
		t.Error("the recorder still runs")
	}
}
