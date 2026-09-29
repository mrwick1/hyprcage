// Package browser runs the agent's Chrome on a screen with the DevTools
// protocol on 127.0.0.1, for chrome-devtools-mcp --browserUrl to drive.
package browser

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
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

// Open starts the agent Chrome on rec. A port that already answers means a
// browser is there, the agent's or the human's: it is refused, never shared.
func Open(c *screen.Ctx, rec *registry.Screen, url string) (Info, error) {
	base := fmt.Sprintf("http://127.0.0.1:%d", c.Cfg.BrowserPort)
	if _, err := Ready(base, 300*time.Millisecond); err == nil {
		return Info{}, screen.Errf(screen.CodeBrowserBusy, "reuse it through the agent-chrome tools, or destroy its screen",
			"something already serves DevTools on %s", base)
	}
	bin, err := Find(c.Cfg.BrowserCommand, exec.LookPath)
	if err != nil {
		return Info{}, err
	}
	profile, err := os.MkdirTemp("", "hc-chrome-")
	if err != nil {
		return Info{}, err
	}
	pid, _, err := screen.Launch(c, rec, Args(bin, c.Cfg.BrowserPort, profile, url), "", nil)
	if err != nil {
		return Info{}, err
	}
	ws, err := Ready(base, 20*time.Second)
	if err != nil {
		return Info{}, err
	}
	return Info{Screen: rec.Name, PID: pid, Port: c.Cfg.BrowserPort, BrowserURL: base, WS: ws, Profile: profile}, nil
}
