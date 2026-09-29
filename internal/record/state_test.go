package record

import (
	"os"
	"testing"
	"time"
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
