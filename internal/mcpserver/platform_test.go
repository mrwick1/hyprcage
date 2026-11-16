package mcpserver

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func toolNames(t *testing.T) []string {
	t.Helper()
	res, err := connect(t).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
	}
	return names
}

// Off Linux the server registers the headless tools and no others.
func TestToolsOffLinux(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	goos = "windows"
	t.Cleanup(func() { goos = "linux" })
	got := toolNames(t)
	if len(got) != len(offLinux) {
		t.Errorf("%d tools, want %d: %v", len(got), len(offLinux), got)
	}
	for name := range offLinux {
		if !slices.Contains(got, name) {
			t.Errorf("tool %s missing", name)
		}
	}
	for _, name := range []string{"app_launch", "mirror", "setup", "batch", "clipboard_get", "desktop_key", "notify_list", "windows"} {
		if slices.Contains(got, name) {
			t.Errorf("tool %s registered off Linux", name)
		}
	}
}

func TestInstructionsOffLinux(t *testing.T) {
	goos = "windows"
	t.Cleanup(func() { goos = "linux" })
	cs := connect(t)
	if got := cs.InitializeResult().Instructions; !strings.Contains(got, "headless Chrome") || strings.Contains(got, "app_launch") {
		t.Errorf("instructions: %q", got)
	}
}
