package screen

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/session"
)

// FreePort returns a free TCP port on 127.0.0.1.
func FreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// DebugArgs appends the DevTools flags to command. The port binds to
// 127.0.0.1 only, and the renderer keeps its accessibility tree built.
func DebugArgs(command []string, port int) []string {
	out := append([]string{}, command...)
	return append(out,
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--remote-debugging-address=127.0.0.1",
		"--force-renderer-accessibility")
}

// WaitDebug polls http://127.0.0.1:<port>/json/version until it answers.
// It returns *Error with CodeCDP after timeout.
func WaitDebug(port int, timeout time.Duration) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	deadline := time.Now().Add(timeout)
	for {
		// A zero client timeout means none: keep each probe bounded.
		hc := http.Client{Timeout: max(min(time.Until(deadline), time.Second), 50*time.Millisecond)}
		if resp, err := hc.Get(url); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return errf(CodeCDP, "the app ignores --remote-debugging-port; snapshot falls back to AT-SPI or OCR",
				"no DevTools answer on 127.0.0.1:%d within %s", port, timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// PrepareDebug picks a DevTools port for the application to launch on rec
// and returns the command with the DevTools flags. A screen runs one
// application: a saved port whose recorded owner still listens is refused
// with CodeLimit. A dead port, or one that another process took, is
// replaced.
func PrepareDebug(rec *registry.Screen, command []string) ([]string, int, error) {
	if rec.DebugPort != 0 && OwnsPort(rec) {
		return nil, 0, errf(CodeLimit, "a screen runs one application; use another screen",
			"screen %s already has a DevTools port %d", rec.Name, rec.DebugPort)
	}
	port, err := FreePort()
	if err != nil {
		return nil, 0, err
	}
	return DebugArgs(command, port), port, nil
}

// debugCmdlineOK reports whether the command line of pid carries
// --remote-debugging-port=<port>. Tests replace it: their listener is the
// test process.
var debugCmdlineOK = func(pid, port int) bool {
	argv, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	want := fmt.Sprintf("--remote-debugging-port=%d", port)
	return err == nil && slices.Contains(strings.Split(string(argv), "\x00"), want)
}

// ClaimDebug records on rec the process that listens on the DevTools port
// just opened: DebugPort, DebugPID and DebugPIDStart. The listener must
// carry --remote-debugging-port=<port>. On error rec is unchanged. It
// does not save rec.
func ClaimDebug(rec *registry.Screen, port int) error {
	pid, err := ListenerPID(port)
	if err != nil || pid == 0 || !debugCmdlineOK(pid, port) {
		return errf(CodeCDP, "another program holds the port; relaunch the app with debug=true",
			"127.0.0.1:%d is not held by the application just launched", port)
	}
	start, err := session.ProcStart(pid)
	if err != nil {
		return errf(CodeCDP, "relaunch the app with debug=true", "the process on 127.0.0.1:%d exited: %v", port, err)
	}
	rec.DebugPort, rec.DebugPID, rec.DebugPIDStart = port, pid, start
	return nil
}

// ConfirmDebug waits for the DevTools port, records its owner process,
// then saves rec. On error the application keeps running and rec is
// unchanged.
func ConfirmDebug(rec *registry.Screen, port int, timeout time.Duration) error {
	if err := WaitDebug(port, timeout); err != nil {
		return err
	}
	if err := ClaimDebug(rec, port); err != nil {
		return err
	}
	return registry.Save(rec)
}
