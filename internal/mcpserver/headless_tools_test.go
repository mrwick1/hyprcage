package mcpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const toolsPageA = `<!doctype html><title>A</title>
<body style="margin:0;height:3000px">
<form onsubmit="return false">
<input id=q aria-label="Name" style="position:absolute;left:100px;top:100px;width:300px;height:40px">
<input id=c type=checkbox aria-label="Agree" style="position:absolute;left:100px;top:160px;width:24px;height:24px">
<button id=b type=button aria-label="Send" onclick="document.getElementById('out').textContent='sent';fetch('/missing')" style="position:absolute;left:100px;top:200px;width:120px;height:40px">Send</button>
</form>
<div id=out style="position:absolute;left:300px;top:200px;width:100px;height:40px"></div>
<a id=l href="/b" style="position:absolute;left:100px;top:260px">Go to B</a>
<div style="position:absolute;top:2900px">BOTTOM-MARKER</div>
<script>// A request made while the page loads starts before the network log attaches.
console.log("hello-console");fetch("/missing")</script>
</body>`

const toolsPageB = `<!doctype html><title>B</title><body><h1>Page B</h1></body>`

func TestHeadlessTools(t *testing.T) {
	requireChrome(t)
	_, cs := guardServer(t, nil)
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, toolsPageA)
		case "/b":
			fmt.Fprint(w, toolsPageB)
		default:
			http.NotFound(w, r)
		}
	}))
	defer web.Close()
	rec := createHeadlessScreen(t, cs)
	mustCall(t, cs, "browser_open", map[string]any{"url": web.URL})

	eval := func(expr string) string {
		t.Helper()
		return text(mustCall(t, cs, "devtools_eval", map[string]any{"expression": expr}))
	}
	// ref returns the ref of the snapshot line that holds all of the words.
	ref := func(snap string, words ...string) string {
		t.Helper()
		for _, line := range strings.Split(snap, "\n") {
			ok := true
			for _, w := range words {
				ok = ok && strings.Contains(line, w)
			}
			if m := regexp.MustCompile(`\[([^\]\s]+)\]`).FindStringSubmatch(line); ok && m != nil {
				return m[1]
			}
		}
		t.Fatalf("no ref for %v in:\n%s", words, snap)
		return ""
	}
	var snap string

	t.Run("snapshot", func(t *testing.T) {
		snap = text(mustCall(t, cs, "snapshot", nil))
		if !strings.Contains(snap, "source=cdp") || !strings.Contains(snap, `textbox "Name"`) {
			t.Errorf("snapshot: %s", snap)
		}
	})
	t.Run("act", func(t *testing.T) {
		mustCall(t, cs, "act", map[string]any{"ref": ref(snap, `textbox "Name"`), "op": "type", "text": "abc"})
		mustCall(t, cs, "act", map[string]any{"ref": ref(snap, `"Agree"`), "op": "click"})
		mustCall(t, cs, "act", map[string]any{"ref": ref(snap, `"Send"`), "op": "click"})
		if got := eval("document.getElementById('q').value"); got != `"abc"` {
			t.Errorf("value = %s", got)
		}
		if got := eval("document.getElementById('c').checked"); got != "true" {
			t.Errorf("checked = %s", got)
		}
		if got := eval("document.getElementById('out').textContent"); got != `"sent"` {
			t.Errorf("out = %s", got)
		}
	})
	t.Run("find", func(t *testing.T) {
		if got := text(mustCall(t, cs, "find", map[string]any{"text": "sent", "timeout_ms": 3000})); !strings.Contains(got, "sent") {
			t.Errorf("find: %s", got)
		}
	})
	t.Run("key", func(t *testing.T) {
		eval("document.getElementById('q').focus()")
		mustCall(t, cs, "key", map[string]any{"keys": []string{"x"}})
		if got := eval("document.getElementById('q').value"); got != `"abcx"` {
			t.Errorf("value after key = %s", got)
		}
	})
	t.Run("pointer", func(t *testing.T) {
		mustCall(t, cs, "scroll", map[string]any{"x": 600, "y": 400, "direction": "down"})
		mustCall(t, cs, "scroll", map[string]any{"x": 600, "y": 400, "direction": "up"})
		mustCall(t, cs, "move", map[string]any{"x": 700, "y": 500})
		mustCall(t, cs, "double_click", map[string]any{"x": 700, "y": 500})
		mustCall(t, cs, "drag", map[string]any{"x1": 600, "y1": 400, "x2": 700, "y2": 450})
	})
	t.Run("wait", func(t *testing.T) {
		mustCall(t, cs, "wait", map[string]any{"stable_ms": 300, "timeout_ms": 5000})
	})
	t.Run("read_page", func(t *testing.T) {
		if got := text(mustCall(t, cs, "read_page", map[string]any{"mode": "full"})); !strings.Contains(got, "BOTTOM-MARKER") {
			t.Errorf("read_page: %s", got)
		}
	})
	t.Run("watch", func(t *testing.T) {
		if got := text(mustCall(t, cs, "watch", map[string]any{"seconds": 2})); !strings.Contains(got, "stable") {
			t.Errorf("watch: %s", got)
		}
	})
	t.Run("console_network", func(t *testing.T) {
		if got := text(mustCall(t, cs, "devtools_console", nil)); !strings.Contains(got, "hello-console") {
			t.Errorf("console: %s", got)
		}
		if got := text(mustCall(t, cs, "devtools_network", nil)); !strings.Contains(got, "404") || !strings.Contains(got, "/missing") {
			t.Errorf("network: %s", got)
		}
	})
	t.Run("clipboard", func(t *testing.T) {
		set := hcall(t, cs, "clipboard_set", map[string]any{"text": "clip-text"})
		if set.IsError {
			// Headless has no clipboard: the refusal must carry its code.
			t.Logf("clipboard_set refused on headless: %s", text(set))
			if !strings.Contains(text(set), "unsupported_kind") {
				t.Errorf("clipboard_set error: %s", text(set))
			}
			if got := hcall(t, cs, "clipboard_get", nil); !got.IsError || !strings.Contains(text(got), "unsupported_kind") {
				t.Errorf("clipboard_get: %s", text(got))
			}
			return
		}
		if got := text(mustCall(t, cs, "clipboard_get", nil)); !strings.Contains(got, "clip-text") {
			t.Errorf("clipboard_get: %s", got)
		}
	})
	t.Run("link", func(t *testing.T) {
		s := text(mustCall(t, cs, "snapshot", map[string]any{"mode": "full"}))
		mustCall(t, cs, "act", map[string]any{"ref": ref(s, "link", "Go to B"), "op": "click"})
		if got := text(mustCall(t, cs, "find", map[string]any{"text": "Page B", "timeout_ms": 5000})); !strings.Contains(got, "Page B") {
			t.Errorf("find Page B: %s", got)
		}
	})
	t.Run("screenshot", func(t *testing.T) {
		shot := mustCall(t, cs, "screenshot", map[string]any{"scale": 1})
		for _, c := range shot.Content {
			if ic, ok := c.(*mcp.ImageContent); ok {
				cfg, err := png.DecodeConfig(bytes.NewReader(ic.Data))
				if err != nil {
					t.Fatal(err)
				}
				if cfg.Width != 1280 || cfg.Height != 800 {
					t.Errorf("size %dx%d", cfg.Width, cfg.Height)
				}
				return
			}
		}
		t.Errorf("no image: %s", text(shot))
	})
	t.Run("screen_list", func(t *testing.T) {
		var list []struct{ Screen, Kind string }
		raw := text(mustCall(t, cs, "screen_list", nil))
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			t.Fatalf("%v: %s", err, raw)
		}
		for _, s := range list {
			if s.Screen == rec.Name && s.Kind == "headless" {
				return
			}
		}
		t.Errorf("screen_list lacks %s: %s", rec.Name, raw)
	})
}
