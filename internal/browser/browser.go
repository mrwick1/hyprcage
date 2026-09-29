// Package browser runs the agent's Chrome on a screen with the DevTools
// protocol on 127.0.0.1, for chrome-devtools-mcp --browserUrl to drive.
package browser

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

// Info describes the running agent Chrome.
type Info struct {
	Screen     string `json:"screen"`
	PID        int    `json:"pid"`
	Port       int    `json:"port"`
	BrowserURL string `json:"browser_url"`
	WS         string `json:"ws_endpoint"`
	Profile    string `json:"profile"`
}

var candidates = []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"}

// Find returns the configured browser, else the first Chrome on PATH.
func Find(configured string, lookPath func(string) (string, error)) (string, error) {
	names := candidates
	if configured != "" {
		names = []string{configured}
	}
	for _, n := range names {
		if p, err := lookPath(n); err == nil {
			return p, nil
		}
	}
	return "", screen.Errf(screen.CodeBrowserDown, "set browser.command in config.toml", "no Chrome found (%v)", names)
}

// Args is the Chrome command line: Wayland, a throwaway profile, DevTools
// on 127.0.0.1 only.
func Args(bin string, port int, profile, url string) []string {
	a := []string{bin, "--ozone-platform=wayland", "--user-data-dir=" + profile, "--no-first-run",
		"--no-default-browser-check", "--remote-debugging-address=127.0.0.1", fmt.Sprintf("--remote-debugging-port=%d", port)}
	if url != "" {
		a = append(a, url)
	}
	return a
}

// Ready polls baseURL/json/version until it answers, and returns the
// browser's WebSocket endpoint.
func Ready(baseURL string, timeout time.Duration) (string, error) {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(timeout)
	for {
		resp, err := client.Get(baseURL + "/json/version")
		if err == nil {
			var v struct {
				WS string `json:"webSocketDebuggerUrl"`
			}
			err = json.NewDecoder(resp.Body).Decode(&v)
			resp.Body.Close()
			if err == nil && v.WS != "" {
				return v.WS, nil
			}
		}
		if time.Now().After(deadline) {
			return "", screen.Errf(screen.CodeTimeout, "", "no DevTools endpoint at %s within %s", baseURL, timeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// CheckURL accepts an empty url or one of http, https, file, data and
// about. A leading "-" would reach Chrome as a flag.
func CheckURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if strings.HasPrefix(raw, "-") || err != nil || !allowedSchemes[u.Scheme] {
		return screen.Errf(screen.CodeUnsupported, "", "browser_open accepts http, https, file, data or about URLs, got %q", raw)
	}
	return nil
}

var allowedSchemes = map[string]bool{"http": true, "https": true, "file": true, "data": true, "about": true}

// portFree reports whether nothing holds port on 127.0.0.1.
func portFree(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	if conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
		conn.Close()
		return false
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// listenerCmdline returns the argv, joined by spaces, of the process that
// listens on 127.0.0.1:port, or "" when none does. Linux only: it reads
// /proc/net/tcp and the fd links of the user's processes.
func listenerCmdline(port int) string {
	data, err := os.ReadFile("/proc/net/tcp")
	if err != nil {
		return ""
	}
	local := fmt.Sprintf("0100007F:%04X", port)
	inode := ""
	for _, line := range strings.Split(string(data), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) > 9 && f[1] == local && f[3] == "0A" { // 0A is LISTEN
			inode = f[9]
			break
		}
	}
	if inode == "" {
		return ""
	}
	target := "socket:[" + inode + "]"
	fds, _ := filepath.Glob("/proc/[0-9]*/fd/*")
	for _, fd := range fds {
		if link, err := os.Readlink(fd); err == nil && link == target {
			pid := strings.Split(fd, "/")[2]
			argv, _ := os.ReadFile("/proc/" + pid + "/cmdline")
			return strings.ReplaceAll(string(argv), "\x00", " ")
		}
	}
	return "?" // a listener whose owner this user cannot see
}

// owned waits until something listens on 127.0.0.1:port and checks that
// its command line holds mark, the --user-data-dir of the Chrome just
// launched. That proves the DevTools endpoint is the agent's own.
func owned(mark string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if argv := listenerCmdline(port); argv != "" {
			if !strings.Contains(argv, mark) {
				return screen.Errf(screen.CodeBrowserBusy, "", "another program took 127.0.0.1:%d", port)
			}
			return nil
		}
		if time.Now().After(deadline) {
			return screen.Errf(screen.CodeTimeout, "", "Chrome did not listen on 127.0.0.1:%d within %s", port, timeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// gone waits until nothing listens on 127.0.0.1:port.
func gone(port int, timeout time.Duration) {
	for deadline := time.Now().Add(timeout); listenerCmdline(port) != "" && time.Now().Before(deadline); {
		time.Sleep(200 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
}

// Open starts the agent Chrome on rec. A port in use means some other
// program is there, the human's Chrome for instance: it is refused, never
// shared.
func Open(c *screen.Ctx, rec *registry.Screen, url string) (Info, error) {
	if err := CheckURL(url); err != nil {
		return Info{}, err
	}
	port := c.Cfg.BrowserPort
	if !portFree(port) {
		return Info{}, screen.Errf(screen.CodeBrowserBusy,
			fmt.Sprintf("port %d is in use; close whatever holds it or set browser.port", port),
			"something already listens on 127.0.0.1:%d", port)
	}
	bin, err := Find(c.Cfg.BrowserCommand, exec.LookPath)
	if err != nil {
		return Info{}, err
	}
	// ponytail: the profile outlives a successful run; destroy cleans no
	// per-screen directory yet.
	profile, err := os.MkdirTemp("", "hc-chrome-")
	if err != nil {
		return Info{}, err
	}
	pid, _, err := screen.Launch(c, rec, Args(bin, port, profile, url), "", nil)
	if err != nil {
		os.RemoveAll(profile)
		return Info{}, err
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	var ws string
	err = owned("--user-data-dir="+profile, port, 20*time.Second)
	if err == nil {
		ws, err = Ready(base, 5*time.Second)
	}
	if err != nil {
		// Launch starts a new session, so -pid is its process group.
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		_ = syscall.Kill(pid, syscall.SIGTERM)
		gone(port, 5*time.Second) // Chrome writes to its profile until it exits
		os.RemoveAll(profile)
		return Info{}, err
	}
	return Info{Screen: rec.Name, PID: pid, Port: port, BrowserURL: base, WS: ws, Profile: profile}, nil
}
