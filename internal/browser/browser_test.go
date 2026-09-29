package browser

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
