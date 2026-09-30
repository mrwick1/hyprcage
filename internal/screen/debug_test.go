package screen

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/session"
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

// selfOwned returns a record whose DevTools owner is the test process,
// listening on port.
func selfOwned(t *testing.T, port int) *registry.Screen {
	t.Helper()
	start, err := session.ProcStart(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	return &registry.Screen{Name: "t", DebugPort: port, DebugPID: os.Getpid(), DebugPIDStart: start}
}

func TestPrepareDebugRefusesLivePort(t *testing.T) {
	rec := selfOwned(t, debugServer(t))
	_, _, err := PrepareDebug(rec, []string{"code"})
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeLimit {
		t.Fatalf("err = %v, want code %s", err, CodeLimit)
	}
	// Another process now answers on the port: it is replaced, not refused.
	rec.DebugPID++
	if _, port, err := PrepareDebug(rec, []string{"code"}); err != nil || port == rec.DebugPort {
		t.Fatalf("foreign owner: port %d, err %v", port, err)
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

func TestListenerPID(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	if pid, err := ListenerPID(port); err != nil || pid != os.Getpid() {
		t.Fatalf("ListenerPID = %d, %v, want %d", pid, err, os.Getpid())
	}
	free, _ := FreePort()
	if pid, err := ListenerPID(free); err != nil || pid != 0 {
		t.Fatalf("ListenerPID(free) = %d, %v, want 0", pid, err)
	}
}

func TestOwnsPort(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	rec := selfOwned(t, l.Addr().(*net.TCPAddr).Port)
	if !OwnsPort(rec) {
		t.Fatal("OwnsPort = false for the recorded PID and start")
	}
	other := *rec
	other.DebugPID = os.Getppid()
	if OwnsPort(&other) {
		t.Error("OwnsPort = true for another PID")
	}
	other = *rec
	other.DebugPIDStart++
	if OwnsPort(&other) {
		t.Error("OwnsPort = true for another start time")
	}
	other = *rec
	other.DebugPort, _ = FreePort()
	if OwnsPort(&other) {
		t.Error("OwnsPort = true for a port with no listener")
	}
}

// stubCmdline makes the DevTools flag check of ConfirmDebug answer ok.
func stubCmdline(t *testing.T, ok bool) {
	c := debugCmdlineOK
	debugCmdlineOK = func(int, int) bool { return ok }
	t.Cleanup(func() { debugCmdlineOK = c })
}

func TestConfirmDebugRecordsOwner(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	stubCmdline(t, true)
	rec := &registry.Screen{Name: "t"}
	port := debugServer(t)
	if err := ConfirmDebug(rec, port, time.Second); err != nil {
		t.Fatal(err)
	}
	want := selfOwned(t, port)
	if rec.DebugPort != port || rec.DebugPID != want.DebugPID || rec.DebugPIDStart != want.DebugPIDStart {
		t.Fatalf("recorded port %d pid %d start %d, want %+v", rec.DebugPort, rec.DebugPID, rec.DebugPIDStart, want)
	}
}

func TestConfirmDebugRefusesForeignListener(t *testing.T) {
	stubCmdline(t, false) // the listener lacks --remote-debugging-port=<port>
	rec := &registry.Screen{Name: "t"}
	err := ConfirmDebug(rec, debugServer(t), time.Second)
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeCDP {
		t.Fatalf("err = %v, want code %s", err, CodeCDP)
	}
	if rec.DebugPort != 0 || rec.DebugPID != 0 || rec.DebugPIDStart != 0 {
		t.Fatalf("recorded %+v, want nothing", rec)
	}
}

func TestKillDebug(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	start, err := session.ProcStart(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	killDebug(&registry.Screen{Name: "t", DebugPID: cmd.Process.Pid, DebugPIDStart: start + 1}) // another process: spared
	select {
	case <-done:
		t.Fatal("killDebug killed a process whose start time differs")
	case <-time.After(100 * time.Millisecond):
	}
	killDebug(&registry.Screen{Name: "t", DebugPID: cmd.Process.Pid, DebugPIDStart: start})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		cmd.Process.Kill()
		t.Fatal("the recorded DevTools process still runs")
	}
}

func TestHasDebugFlag(t *testing.T) {
	for _, tc := range []struct {
		cmdline string
		ok      bool
	}{
		{"code\x00--remote-debugging-port=1234\x00--force-renderer-accessibility\x00", true},
		{"/opt/google/chrome/chrome --enable-features=Acce --remote-debugging-port=1234 --no-first-run", true}, // Chrome's rewrite
		{"/opt/google/chrome/chrome --remote-debugging-port=4321", false},
		{"chrome\x00--remote-debugging-port=12345\x00", false},
	} {
		if got := hasDebugFlag(tc.cmdline, 1234); got != tc.ok {
			t.Errorf("hasDebugFlag(%q, 1234) = %v, want %v", tc.cmdline, got, tc.ok)
		}
	}
}
