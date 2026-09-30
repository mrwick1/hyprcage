package browser

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
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
	_, err = Open(&screen.Ctx{Cfg: cfg}, rec, "")
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
