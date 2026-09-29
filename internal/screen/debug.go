package screen

import (
	"fmt"
	"net"
	"net/http"
	"time"
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
