package screen

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/hexadecimil/hyprcage/internal/registry"
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
// application: a saved port that still answers is refused with CodeLimit,
// a dead one is replaced.
func PrepareDebug(rec *registry.Screen, command []string) ([]string, int, error) {
	if rec.DebugPort != 0 && WaitDebug(rec.DebugPort, 200*time.Millisecond) == nil {
		return nil, 0, errf(CodeLimit, "a screen runs one application; use another screen",
			"screen %s already has a DevTools port %d", rec.Name, rec.DebugPort)
	}
	port, err := FreePort()
	if err != nil {
		return nil, 0, err
	}
	return DebugArgs(command, port), port, nil
}

// ConfirmDebug waits for the DevTools port, checks that a process of the
// screen listens on it, then saves it on rec. On error the application
// keeps running and rec is unchanged.
func ConfirmDebug(rec *registry.Screen, port int, timeout time.Duration) error {
	if err := WaitDebug(port, timeout); err != nil {
		return err
	}
	if !OwnsPort(rec.Name, port) {
		return errf(CodeCDP, "another program holds the port; relaunch the app with debug=true",
			"127.0.0.1:%d does not belong to screen %s", port, rec.Name)
	}
	rec.DebugPort = port
	return registry.Save(rec)
}
