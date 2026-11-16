package registry

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestSaveLoadListDelete(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if l, err := List(); err != nil || len(l) != 0 {
		t.Fatalf("empty registry: %v, %v", l, err)
	}
	s := &Screen{Name: "hc-1a2b3c", CreatedAt: time.Now(), State: "ready", Width: 1280, Height: 800, WorkspaceApp: 11, Owner: Owner{PID: 1, Client: "test"}}
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	got, err := Load("hc-1a2b3c")
	if err != nil || got.Width != 1280 || got.Owner.Client != "test" {
		t.Fatalf("Load: %+v, %v", got, err)
	}
	if _, err := Load("nope"); err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
	l, err := List()
	if err != nil || len(l) != 1 || l[0].Name != "hc-1a2b3c" {
		t.Fatalf("List: %v, %v", l, err)
	}
	fi, _ := os.Stat(Path("hc-1a2b3c"))
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 { // Windows has no mode bits
		t.Errorf("record perm %v, want 0600", fi.Mode().Perm())
	}
	if err := Delete("hc-1a2b3c"); err != nil {
		t.Fatal(err)
	}
	if err := Delete("hc-1a2b3c"); err != nil {
		t.Errorf("deleting twice must be fine: %v", err)
	}
}

func TestHeartbeat(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if err := Save(&Screen{Name: "hc-a"}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(Path("hc-a"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := Touch("hc-a"); err != nil {
		t.Fatal(err)
	}
	hb, err := Heartbeat("hc-a")
	if err != nil || time.Since(hb) > time.Minute {
		t.Errorf("heartbeat not refreshed: %v, %v", hb, err)
	}
}

func TestReadInner(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if _, err := EnsureDir(); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(InnerPath("hc-a"), []byte("WAYLAND_DISPLAY=wayland-2\nDISPLAY=:3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := ReadInner("hc-a")
	if err != nil || m["WAYLAND_DISPLAY"] != "wayland-2" || m["DISPLAY"] != ":3" {
		t.Fatalf("ReadInner: %v, %v", m, err)
	}
}

func TestDirDefault(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	if runtime.GOOS == "windows" {
		base := t.TempDir()
		t.Setenv("LocalAppData", base)
		if got, want := Dir(), filepath.Join(base, "hyprcage", "run"); got != want {
			t.Errorf("Dir() = %q, want %q", got, want)
		}
	}
	rt := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", rt)
	if got, want := Dir(), filepath.Join(rt, "hyprcage"); got != want {
		t.Errorf("Dir() with XDG_RUNTIME_DIR = %q, want %q", got, want)
	}
}

// A record is replaced while readers open it: Save must not fail.
func TestSaveWhileReading(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if err := Save(&Screen{Name: "hc-a"}); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if _, err := Load("hc-a"); err != nil {
						t.Errorf("Load: %v", err)
						return
					}
					_ = Touch("hc-a")
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}
	for i := 0; i < 300; i++ {
		if err := Save(&Screen{Name: "hc-a", Width: i}); err != nil {
			t.Errorf("Save %d: %v", i, err)
			break
		}
	}
	close(stop)
	wg.Wait()
}
