// Package browser runs the agent's Chrome on a screen with the DevTools
// protocol on 127.0.0.1, for snapshot, act and the devtools_* tools.
package browser

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
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

// listenerCmdline returns the argv, joined by spaces, of the process that
// listens on 127.0.0.1:port, "" when none does, and "?" when its owner is
// not visible to this user.
func listenerCmdline(port int) string {
	pid, err := screen.ListenerPID(port)
	if err != nil {
		return "?"
	}
	if pid == 0 {
		return ""
	}
	argv, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	return strings.ReplaceAll(string(argv), "\x00", " ")
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

// Open starts the agent Chrome on rec, with DevTools on a free port of its
// own. A fixed port would reach whatever Chrome already holds it, the
// human's logged-in one for instance. A screen runs one DevTools app.
func Open(c *screen.Ctx, rec *registry.Screen, url string) (Info, error) {
	if err := CheckURL(url); err != nil {
		return Info{}, err
	}
	if rec.DebugPort != 0 && screen.OwnsPort(rec) {
		return Info{}, screen.Errf(screen.CodeBrowserBusy, "reuse it, or open the browser on another screen",
			"screen %s already runs an app with DevTools on 127.0.0.1:%d", rec.Name, rec.DebugPort)
	}
	port, err := screen.FreePort()
	if err != nil {
		return Info{}, err
	}
	bin, err := Find(c.Cfg.BrowserCommand, exec.LookPath)
	if err != nil {
		return Info{}, err
	}
	// screen.Destroy removes the profile by its prefix.
	profile, err := os.MkdirTemp("", screen.ProfilePrefix(rec.Name))
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
	if err == nil {
		err = screen.ClaimDebug(rec, port) // the port belongs to the screen by this Chrome's PID
	}
	if err == nil {
		err = registry.Save(rec)
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
