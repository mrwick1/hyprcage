package record

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/screen"
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
	_, err := Start(exe, "hc-t", true, cfg)
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
