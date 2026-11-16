package lock

import (
	"os"
	"testing"
	"time"

	"github.com/hexadecimil/hyprcage/internal/registry"
)

func TestTryLockExcludesAnotherHandle(t *testing.T) {
	f1, err := os.OpenFile(t.TempDir()+"/l", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f1.Close()
	f2, err := os.OpenFile(f1.Name(), os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()
	if ok, err := tryLock(f1); !ok || err != nil {
		t.Fatalf("first lock: %v, %v", ok, err)
	}
	if ok, err := tryLock(f2); ok || err != nil {
		t.Fatalf("second handle got the lock: %v, %v", ok, err)
	}
	unlock(f1)
	if ok, err := tryLock(f2); !ok || err != nil {
		t.Fatalf("lock after unlock: %v, %v", ok, err)
	}
	unlock(f2)
}

func TestAcquireTimesOutWhileAnotherHandleHolds(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	rel, err := Acquire(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// A second process is a second handle on the lock file: hold it here.
	other, err := os.OpenFile(registry.LockPath(), os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if ok, _ := tryLock(other); ok {
		t.Fatal("another handle got the lock while Acquire holds it")
	}
	rel()
	if ok, err := tryLock(other); !ok || err != nil {
		t.Fatalf("lock after release: %v, %v", ok, err)
	}
	unlock(other)
	// With the file held by another handle, Acquire gives up with ErrBusy.
	if ok, _ := tryLock(other); !ok {
		t.Fatal("relock")
	}
	if _, err := Acquire(120 * time.Millisecond); err != ErrBusy {
		t.Fatalf("Acquire while held: %v, want ErrBusy", err)
	}
}
