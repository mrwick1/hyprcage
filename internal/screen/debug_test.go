package screen

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/hexadecimil/hyprcage/internal/registry"
)

func TestDebugArgs(t *testing.T) {
	got := DebugArgs([]string{"code"}, 9333)
	want := []string{"code", "--remote-debugging-port=9333", "--remote-debugging-address=127.0.0.1", "--force-renderer-accessibility"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DebugArgs = %q, want %q", got, want)
	}
}

func TestFreePort(t *testing.T) {
	port, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	if port < 1024 || port > 65535 {
		t.Fatalf("port %d out of range", port)
	}
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("listen on free port %d: %v", port, err)
	}
	l.Close()
}

func TestLaunchDebugUnreachable(t *testing.T) {
	port, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = WaitDebug(port, 300*time.Millisecond)
	if d := time.Since(start); d >= time.Second {
		t.Fatalf("WaitDebug took %s", d)
	}
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeCDP {
		t.Fatalf("err = %v, want code %s", err, CodeCDP)
	}
}

func TestWaitDebugReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"Browser":"test"}`)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	if err := WaitDebug(port, 2*time.Second); err != nil {
		t.Fatal(err)
	}
}

// debugServer answers /json/version and returns its port.
func debugServer(t *testing.T) int {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"Browser":"test"}`)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	return port
}

func TestPrepareDebugRefusesLivePort(t *testing.T) {
	rec := &registry.Screen{Name: "t", DebugPort: debugServer(t)}
	_, _, err := PrepareDebug(rec, []string{"code"})
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeLimit {
		t.Fatalf("err = %v, want code %s", err, CodeLimit)
	}
}

func TestPrepareDebugReplacesDeadPort(t *testing.T) {
	dead, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	rec := &registry.Screen{Name: "t", DebugPort: dead}
	args, port, err := PrepareDebug(rec, []string{"code"})
	if err != nil {
		t.Fatal(err)
	}
	if port == dead || port == 0 {
		t.Fatalf("port = %d, dead port was %d", port, dead)
	}
	if !slices.Contains(args, fmt.Sprintf("--remote-debugging-port=%d", port)) {
		t.Fatalf("args %q lack the new port %d", args, port)
	}
}
