//go:build windows

// mcp-e2e drives the bundled Bare browser through offstage MCP. It checks
// form input, navigation, screenshots and desktop isolation. Run it in Windows.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"unsafe"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const page = `<!doctype html><title>e2e</title>
<form id=f><label>Name <input id=q autocomplete=off></label><button>Send</button></form>
<p id=out>none</p><a href="/next">Next page</a>
<script>f.onsubmit=e=>{e.preventDefault();out.textContent='got:'+q.value}</script>`

var (
	user32     = syscall.NewLazyDLL("user32.dll")
	getCursor  = user32.NewProc("GetCursorPos")
	foreground = user32.NewProc("GetForegroundWindow")
)

func human() string {
	var p struct{ X, Y int32 }
	getCursor.Call(uintptr(unsafe.Pointer(&p)))
	fg, _, _ := foreground.Call()
	return fmt.Sprintf("cursor=%d,%d foreground=%#x", p.X, p.Y, fg)
}

func main() {
	exe := flag.String("offstage", `C:\hc\offstage.exe`, "offstage binary")
	flag.Parse()
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/next" {
			fmt.Fprint(w, `<!doctype html><title>next</title><h1>Second page</h1>`)
			return
		}
		fmt.Fprint(w, page)
	}))
	defer web.Close()

	before := human()
	ctx := context.Background()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "0"}, nil).
		Connect(ctx, &mcp.CommandTransport{Command: exec.Command(*exe, "mcp")}, nil)
	must(err)
	defer cs.Close()

	tools, err := cs.ListTools(ctx, nil)
	must(err)
	var names []string
	for _, t := range tools.Tools {
		names = append(names, t.Name)
	}
	fmt.Printf("tools (%d): %s\n", len(names), strings.Join(names, " "))

	sc := map[string]any{"session": "e2e"}
	defer call(cs, "browser_close", sc)
	call(cs, "browser_open", with(sc, "url", web.URL))
	snap := call(cs, "browser_snapshot", sc)
	ref := regexp.MustCompile(`\[(e\d+)\][^\n]*textbox[^\n]*Name`).FindStringSubmatch(snap)
	if ref == nil {
		fail("no Name textbox in snapshot:\n" + snap)
	}
	call(cs, "browser_act", with(sc, "ref", ref[1], "op", "type", "text", "hello windows"))
	call(cs, "browser_act", with(sc, "op", "press", "key", "Enter"))
	got := call(cs, "browser_read", sc)
	if !strings.Contains(got, "got:hello windows") {
		fail("form did not submit: " + got)
	}
	call(cs, "browser_act", with(sc, "op", "click", "loc", `text="Next page"`))
	got = call(cs, "browser_read", sc)
	if !strings.Contains(got, "Second page") {
		fail("navigation did not finish: " + got)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "browser_screenshot", Arguments: with(sc, "inline", true)})
	must(err)
	for _, c := range res.Content {
		if img, ok := c.(*mcp.ImageContent); ok {
			fmt.Printf("screenshot: %s, %d bytes\n", img.MIMEType, len(img.Data))
		}
	}
	after := human()
	fmt.Println("human before:", before)
	fmt.Println("human after: ", after)
	if before != after {
		fail("the human's cursor or foreground window changed")
	}
	fmt.Println("E2E OK")
}

func with(m map[string]any, kv ...any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

func call(cs *mcp.ClientSession, tool string, args map[string]any) string {
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	must(err)
	var b strings.Builder
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	if res.IsError {
		fail(tool + ": " + b.String())
	}
	return b.String()
}

func fail(msg string) { fmt.Fprintln(os.Stderr, "FAIL:", msg); os.Exit(1) }

func must(err error) {
	if err != nil {
		fail(err.Error())
	}
}
