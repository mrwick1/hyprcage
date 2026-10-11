package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

func TestFind(t *testing.T) {
	look := func(have ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, h := range have {
				if h == n {
					return "/usr/bin/" + n, nil
				}
			}
			return "", errors.New("no")
		}
	}
	if b, err := Find("", look("chromium", "google-chrome")); err != nil || b != "/usr/bin/google-chrome" {
		t.Errorf("prefer google-chrome: %s %v", b, err)
	}
	if b, err := Find("", look("chromium")); err != nil || b != "/usr/bin/chromium" {
		t.Errorf("fall back to chromium: %s %v", b, err)
	}
	if b, err := Find("brave", look("brave")); err != nil || b != "/usr/bin/brave" {
		t.Errorf("configured wins: %s %v", b, err)
	}
	if _, err := Find("", look()); err == nil {
		t.Error("no browser: want an error")
	}
}

func TestArgs(t *testing.T) {
	a := strings.Join(Args("/usr/bin/google-chrome", 9222, "/tmp/p", "https://x.test"), " ")
	for _, want := range []string{"--ozone-platform=wayland", "--user-data-dir=/tmp/p", "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=9222", "--no-first-run", "https://x.test"} {
		if !strings.Contains(a, want) {
			t.Errorf("argv lacks %s: %s", want, a)
		}
	}
}

func TestReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/browser/abc"}`)
	}))
	defer srv.Close()
	ws, err := Ready(srv.URL, time.Second)
	if err != nil || ws != "ws://127.0.0.1:9222/devtools/browser/abc" {
		t.Fatalf("Ready: %s %v", ws, err)
	}
	if _, err := Ready("http://127.0.0.1:1", 300*time.Millisecond); err == nil {
		t.Error("nothing listens: want a timeout")
	}
}

func codeOf(err error) screen.Code {
	var se *screen.Error
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}

func TestOpenIgnoresAPortItDoesNotOwn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	cfg := config.Fallback()
	cfg.BrowserCommand = "hc-no-such-browser" // reaching Find gives browser_not_running
	// A recorded port that a stranger holds is not the screen's app: Open picks a new port.
	rec := &registry.Screen{Name: "hc-test", DebugPort: ln.Addr().(*net.TCPAddr).Port}
	_, err = Open(&screen.Ctx{Cfg: cfg}, rec, "", "")
	if codeOf(err) != screen.CodeBrowserDown {
		t.Fatalf("want browser_not_running, got %v", err)
	}
}

func TestCheckURL(t *testing.T) {
	for _, u := range []string{"", "https://x.test", "http://127.0.0.1:8080/a", "file:///tmp/a.html", "data:text/html,<p>x</p>", "about:blank"} {
		if err := CheckURL(u); err != nil {
			t.Errorf("%q: want ok, got %v", u, err)
		}
	}
	for _, u := range []string{"--remote-allow-origins=*", "-x", "javascript:alert(1)", "chrome://settings", "ftp://x.test"} {
		if codeOf(CheckURL(u)) != screen.CodeUnsupported {
			t.Errorf("%q: want unsupported_input", u)
		}
	}
}

func TestOwned(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	// The listener belongs to this test binary: its argv holds "-test.".
	if err := owned("-test.", port, time.Second); err != nil {
		t.Errorf("own listener: %v", err)
	}
	if err := owned("--user-data-dir=/tmp/other", port, time.Second); codeOf(err) != screen.CodeBrowserBusy {
		t.Errorf("foreign listener: want browser_running, got %v", err)
	}
	ln.Close()
	if err := owned("-test.", port, 300*time.Millisecond); codeOf(err) != screen.CodeTimeout {
		t.Errorf("no listener: want timeout, got %v", err)
	}
}

func TestProfileName(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	for _, ok := range []string{"a", "tc-staging", "x1-2", strings.Repeat("a", 40)} {
		if _, err := ProfilePath(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "..", "../x", "a/b", "A", "a_b", "a b", strings.Repeat("a", 41), "a\n"} {
		if _, err := ProfilePath(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestProfilesDirHonoursXDG(t *testing.T) {
	x := t.TempDir()
	t.Setenv("XDG_DATA_HOME", x)
	if d, _ := ProfilesDir(); d != x+"/hyprcage/profiles" {
		t.Errorf("got %s", d)
	}
}

func TestOpenProfileModeAndInUse(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	p, err := openProfile("demo")
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o700 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	// A lock that names a dead pid is stale.
	os.Symlink("host-999999999", p+"/SingletonLock")
	if _, err := openProfile("demo"); err != nil {
		t.Errorf("stale lock refused: %v", err)
	}
	// A live process that runs with this user-data-dir holds the profile.
	cmd := exec.Command("sh", "-c", "sleep 30; true", "--user-data-dir="+p)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	for i := 0; i < 100; i++ { // wait until the child shows its argv
		if b, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", cmd.Process.Pid)); strings.Contains(string(b), "--user-data-dir=") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	os.Remove(p + "/SingletonLock")
	os.Symlink(fmt.Sprintf("host-%d", cmd.Process.Pid), p+"/SingletonLock")
	_, err = openProfile("demo")
	var se *screen.Error
	if !errors.As(err, &se) || se.Code != screen.CodeBrowserBusy {
		t.Fatalf("in-use not refused: %v", err)
	}
	if RemoveProfile("demo") == nil {
		t.Error("rm removed a profile in use")
	}
	cmd.Process.Kill()
	cmd.Wait()
	if err := RemoveProfile("demo"); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("profile still there")
	}
	if ps, _ := ListProfiles(); len(ps) != 0 {
		t.Errorf("list: %v", ps)
	}
}

func TestOpenProfileKeepsSessionCookies(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	p, err := openProfile("keep")
	if err != nil {
		t.Fatal(err)
	}
	file := p + "/Default/Preferences"
	got := func() map[string]any {
		var m map[string]any
		b, _ := os.ReadFile(file)
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	if got()["session"].(map[string]any)["restore_on_startup"] != float64(1) {
		t.Error("restore_on_startup not set on a new profile")
	}
	// Existing preferences stay.
	os.WriteFile(file, []byte(`{"session":{"restore_on_startup":5,"x":1},"a":2}`), 0o600)
	if _, err := openProfile("keep"); err != nil {
		t.Fatal(err)
	}
	m := got()
	s := m["session"].(map[string]any)
	if s["restore_on_startup"] != float64(1) || s["x"] != float64(1) || m["a"] != float64(2) {
		t.Errorf("merge lost data: %v", m)
	}
}
