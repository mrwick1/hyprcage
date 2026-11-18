// Package bare connects offstage to the bundled browser over its native MCP protocol.
package bare

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Find looks beside offstage first, then on PATH. Bare needs its driver too.
func Find() (string, error) {
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	exe, _ := os.Executable()
	for _, dir := range append([]string{filepath.Dir(exe)}, filepath.SplitList(os.Getenv("PATH"))...) {
		server := filepath.Join(dir, "bare-mcp"+suffix)
		driver := filepath.Join(dir, "bare-drive"+suffix)
		if executable(server) && executable(driver) {
			return server, nil
		}
	}
	return "", fmt.Errorf("Bare is required: reinstall offstage to install bare-mcp and bare-drive")
}

func executable(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && (runtime.GOOS == "windows" || st.Mode()&0111 != 0)
}

// Environment keeps visible browser windows on the supplied cage socket.
// A headless browser cannot connect to the human's compositor by accident.
func Environment(display string) []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "WAYLAND_DISPLAY", "DISPLAY", "HYPRLAND_INSTANCE_SIGNATURE", "BARE_MCP_REPORT", "GDK_SCALE", "GDK_DPI_SCALE", "QT_SCALE_FACTOR":
			continue
		}
		env = append(env, kv)
	}
	return append(env, "WAYLAND_DISPLAY="+display)
}

// Start creates a browser process. Close the returned session to reap it.
func Start(ctx context.Context, display string) (*mcp.ClientSession, error) {
	bin, err := Find()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin)
	cmd.Env = Environment(display)
	cmd.Stderr = os.Stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "offstage", Version: "1"}, nil)
	return client.Connect(ctx, &mcp.CommandTransport{Command: cmd, TerminateDuration: time.Second}, nil)
}
