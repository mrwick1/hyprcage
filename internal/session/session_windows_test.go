package session

import (
	"os/exec"
	"testing"
	"time"
)

// filetime converts a ProcStart value to a time.
func filetime(t uint64) time.Time {
	return time.Unix(0, (int64(t)-116444736000000000)*100)
}

func TestProcStartIsCreationTime(t *testing.T) {
	c := exec.Command("ping", "-n", "30", "127.0.0.1")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer c.Process.Kill()
	st, err := ProcStart(c.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(filetime(st)); d < 0 || d > time.Minute {
		t.Errorf("creation time %v is %v ago, want under a minute", filetime(st), d)
	}
	if !PIDAlive(c.Process.Pid, st) || PIDAlive(c.Process.Pid, st+1) {
		t.Error("PIDAlive must accept the start time and refuse another")
	}
	if got := procComm(c.Process.Pid); got != "ping" {
		t.Errorf("procComm = %q, want ping", got)
	}
}

// A process that exited stays openable while a handle holds it, which
// os/exec does until Wait: it must still count as dead.
func TestExitedProcessIsDead(t *testing.T) {
	c := exec.Command("cmd", "/c", "exit", "0")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	st, err := ProcStart(c.Process.Pid)
	if err != nil {
		t.Skip("exited before the first query")
	}
	deadline := time.Now().Add(5 * time.Second)
	for PIDAlive(c.Process.Pid, st) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if PIDAlive(c.Process.Pid, st) || PIDAlive(c.Process.Pid, 0) {
		t.Error("exited process reported alive")
	}
	if _, err := ProcStart(c.Process.Pid); err == nil {
		t.Error("ProcStart of an exited process must fail")
	}
	c.Wait()
}
