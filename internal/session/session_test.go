package session

import (
	"os"
	"testing"
	"time"

	"github.com/hexadecimil/offstage/internal/registry"
)

func TestParseStartTime(t *testing.T) {
	// Field 22 is 1234567; the command name contains a space and parentheses.
	stat := "4242 (my (odd) cmd) S 1 4242 4242 0 -1 4194560 100 0 0 0 5 3 0 0 20 0 1 0 1234567 8192 100 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 3 0 0 0 0 0"
	got, err := parseStartTime(stat)
	if err != nil || got != 1234567 {
		t.Fatalf("got %d, %v", got, err)
	}
	if _, err := parseStartTime("garbage"); err == nil {
		t.Error("expected an error")
	}
}

func TestProcStartSelf(t *testing.T) {
	st, err := ProcStart(os.Getpid())
	if err != nil || st == 0 {
		t.Fatalf("ProcStart(self) = %d, %v", st, err)
	}
	if !PIDAlive(os.Getpid(), st) {
		t.Error("self should be alive")
	}
	if PIDAlive(os.Getpid(), st+1) {
		t.Error("a different start time must not match")
	}
	if PIDAlive(-1, 0) {
		t.Error("negative pid must be dead")
	}
}

func TestIsAlive(t *testing.T) {
	self, _ := ProcStart(os.Getpid())
	alive := registry.Owner{PID: os.Getpid(), PIDStart: self}
	if !IsAlive(alive, time.Time{}, time.Minute) {
		t.Error("live pid must be alive even without a heartbeat")
	}
	dead := registry.Owner{PID: 999999999, SessionID: "s"}
	if !IsAlive(dead, time.Now(), time.Minute) {
		t.Error("fresh heartbeat of a known session must keep it alive")
	}
	if IsAlive(dead, time.Now().Add(-2*time.Minute), time.Minute) {
		t.Error("stale heartbeat must be dead")
	}
	anon := registry.Owner{PID: 999999999}
	if IsAlive(anon, time.Now(), time.Minute) {
		t.Error("anonymous owner with a dead pid must be dead, heartbeat or not")
	}
}

func TestWriteRead(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	info := Info{SessionID: "abc", PID: 4242, Source: "startup", At: time.Now()}
	if err := Write(info); err != nil {
		t.Fatal(err)
	}
	got, err := Read(4242)
	if err != nil || got.SessionID != "abc" || got.Source != "startup" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := Read(1); err == nil {
		t.Error("unknown pid must fail")
	}
}
