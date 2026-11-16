package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/headless"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/screen"
)

const headlessPage = `<!doctype html><title>t</title>
<body style="margin:0;background:#fff">
<input id=q aria-label="Name" style="position:absolute;left:100px;top:100px;width:300px;height:40px">
<div style="position:absolute;left:600px;top:300px;width:200px;height:200px;background:#c00"></div>
</body>`

func hcall(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

func mustCall(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res := hcall(t, cs, name, args)
	if res.IsError {
		t.Fatalf("%s: %s", name, text(res))
	}
	return res
}

func requireChrome(t *testing.T) {
	t.Helper()
	if _, err := headless.FindChrome(); err != nil {
		t.Skip("no Chrome: " + err.Error())
	}
}

// createHeadlessScreen makes a headless screen through screen_create and
// destroys it when the test ends, unless the test destroyed it already.
func createHeadlessScreen(t *testing.T, cs *mcp.ClientSession) *registry.Screen {
	t.Helper()
	var out struct{ Screen, Kind string }
	if err := json.Unmarshal([]byte(text(mustCall(t, cs, "screen_create", map[string]any{"kind": "headless"}))), &out); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hcall(t, cs, "screen_destroy", map[string]any{"screen": out.Screen}) })
	rec, err := registry.Load(out.Screen)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != "headless" || rec.Kind != "headless" || rec.State != "ready" || rec.DebugPort == 0 || rec.DebugPID == 0 {
		t.Fatalf("reply kind %q, record %+v", out.Kind, rec)
	}
	return rec
}

func TestHeadlessScreenEndToEnd(t *testing.T) {
	requireChrome(t)
	s, cs := guardServer(t, nil)
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, headlessPage) }))
	defer web.Close()
	rec := createHeadlessScreen(t, cs)
	profile := screen.ProfilePrefix(rec.Name)
	profiles, _ := filepath.Glob(filepath.Join(os.TempDir(), profile+"*"))
	if len(profiles) != 1 || len(chromeProcs(profiles[0])) == 0 {
		t.Fatalf("profile dirs %v, processes %v", profiles, chromeProcs(profile))
	}

	open := text(mustCall(t, cs, "browser_open", map[string]any{"url": web.URL}))
	if !strings.Contains(open, fmt.Sprintf(`"port": %d`, rec.DebugPort)) || !strings.Contains(open, `"ws_endpoint"`) {
		t.Errorf("browser_open reply: %s", open)
	}
	if snap := text(mustCall(t, cs, "snapshot", nil)); !strings.Contains(snap, "source=cdp") || !strings.Contains(snap, `textbox "Name"`) {
		t.Errorf("snapshot: %s", snap)
	}
	found := text(mustCall(t, cs, "find", map[string]any{"text": "Name", "role": "textbox", "timeout_ms": 2000}))
	m := regexp.MustCompile(`\((\d+),(\d+)\)`).FindStringSubmatch(found)
	if m == nil {
		t.Fatalf("find: %s", found)
	}
	x, _ := strconv.Atoi(m[1])
	y, _ := strconv.Atoi(m[2])
	mustCall(t, cs, "click", map[string]any{"x": x, "y": y})
	mustCall(t, cs, "type", map[string]any{"text": "héllo"})
	eval := func() string {
		var v string
		if err := json.Unmarshal([]byte(text(mustCall(t, cs, "devtools_eval", map[string]any{"expression": "document.getElementById('q').value"}))), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if got := eval(); got != "héllo" {
		t.Errorf("typed value = %q", got)
	}

	shot := mustCall(t, cs, "screenshot", map[string]any{"scale": 1})
	var img *mcp.ImageContent
	for _, c := range shot.Content {
		if ic, ok := c.(*mcp.ImageContent); ok {
			img = ic
		}
	}
	if img == nil {
		t.Fatalf("screenshot has no image: %s", text(shot))
	}
	pic, err := png.Decode(bytes.NewReader(img.Data))
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, _ := pic.At(700, 400).RGBA(); r>>8 < 180 || g>>8 > 40 || b>>8 > 40 {
		t.Errorf("pixel in the red box = %d,%d,%d", r>>8, g>>8, b>>8)
	}
	if r, g, b, _ := pic.At(20, 20).RGBA(); r>>8 != 255 || g>>8 != 255 || b>>8 != 255 {
		t.Errorf("pixel of the white page = %d,%d,%d", r>>8, g>>8, b>>8)
	}

	// A second process has no cached Browser: resolve dials Chrome again.
	k := tableKey(rec)
	old := s.browsers[k]
	if old == nil {
		t.Fatal("create did not cache the browser")
	}
	delete(s.browsers, k)
	mustCall(t, cs, "type", map[string]any{"text": " you"})
	if s.browsers[k] == nil || s.browsers[k] == old {
		t.Fatal("resolve did not attach again")
	}
	if got := eval(); got != "héllo you" {
		t.Errorf("value after re-attach = %q", got)
	}

	mustCall(t, cs, "screen_destroy", map[string]any{"screen": rec.Name})
	if _, err := registry.Load(rec.Name); err != registry.ErrNotFound {
		t.Errorf("record after destroy: %v", err)
	}
	if _, err := os.Stat(profiles[0]); !os.IsNotExist(err) {
		t.Errorf("profile dir %s after destroy: %v", profiles[0], err)
	}
	if left := chromeProcs(profiles[0]); len(left) > 0 {
		t.Errorf("Chrome processes left: %v", left)
	}
	if s.browsers[k] != nil {
		t.Error("destroy kept the cached browser")
	}
}

func TestHeadlessRefusesWhatNeedsACage(t *testing.T) {
	requireChrome(t)
	_, cs := guardServer(t, nil)
	rec := createHeadlessScreen(t, cs)
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"app_launch", map[string]any{"command": []string{"xterm"}}},
		{"mirror", nil},
		{"clipboard_get", nil},
		{"clipboard_set", map[string]any{"text": "x"}},
		{"read_selection", nil},
	} {
		args := map[string]any{"screen": rec.Name}
		for k, v := range c.args {
			args[k] = v
		}
		if res := hcall(t, cs, c.tool, args); !res.IsError || !strings.Contains(text(res), "unsupported_kind") {
			t.Errorf("%s: isError=%v text=%q", c.tool, res.IsError, text(res))
		}
	}
}

func TestGCReapsHeadlessWhenChromeDies(t *testing.T) {
	requireChrome(t)
	_, cs := guardServer(t, nil)
	rec := createHeadlessScreen(t, cs)
	proc, _ := os.FindProcess(rec.DebugPID)
	proc.Kill()
	deadline := time.Now().Add(5 * time.Second)
	for screen.Alive(rec) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if res := hcall(t, cs, "screenshot", map[string]any{"screen": rec.Name}); !res.IsError || !strings.Contains(text(res), "screen_dead") {
		t.Errorf("screenshot of a dead screen: isError=%v text=%q", res.IsError, text(res))
	}
	c, err := screen.Connect(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	rep, err := screen.GC(c, screen.GCOptions{})
	if err != nil || len(rep.Destroyed) != 1 || rep.Destroyed[0] != rec.Name || len(rep.Errors) > 0 {
		t.Fatalf("gc: %+v, %v", rep, err)
	}
	if _, err := registry.Load(rec.Name); err != registry.ErrNotFound {
		t.Errorf("record after gc: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(os.TempDir(), screen.ProfilePrefix(rec.Name)+"*")); len(left) > 0 {
		t.Errorf("profile dirs after gc: %v", left)
	}
}
