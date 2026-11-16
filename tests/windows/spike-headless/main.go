//go:build windows

// Spike: drive headless Chrome on Windows through CDP only, with no
// desktop. It checks input (click, text, Enter), a CDP screenshot and a
// screencast. Run it from SSH (session 0) and from the interactive
// session. See docs/superpowers/specs/2026-11-16-offstage-windows-spike-rulings.md.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"github.com/hexadecimil/offstage/internal/cdp"
)

const page = `<!doctype html><title>spike</title>
<style>#box{position:absolute;top:300px;width:80px;height:80px;background:#c33;animation:m 2s linear infinite alternate}
@keyframes m{from{left:0}to{left:1100px}}</style>
<form id=f><input id=q autocomplete=off><button>go</button></form><p id=out>none</p>
<a id=l href="#next">link</a><div id=box></div>
<script>
f.onsubmit=e=>{e.preventDefault();out.textContent='submit:'+q.value};
l.onclick=()=>{out.textContent+=' clicked'};
</script>`

var (
	user32     = syscall.NewLazyDLL("user32.dll")
	getCursor  = user32.NewProc("GetCursorPos")
	foreground = user32.NewProc("GetForegroundWindow")
)

func human() string {
	var p struct{ X, Y int32 }
	ok, _, _ := getCursor.Call(uintptr(unsafe.Pointer(&p)))
	fg, _, _ := foreground.Call()
	return fmt.Sprintf("cursor=%d,%d ok=%d foreground=%#x", p.X, p.Y, ok, fg)
}

func main() {
	out := flag.String("out", `C:\spike\headless`, "output folder")
	chrome := flag.String("chrome", `C:\Program Files\Google\Chrome\Application\chrome.exe`, "chrome path")
	port := flag.Int("port", 9339, "DevTools port")
	secs := flag.Int("rec", 4, "screencast seconds")
	flag.Parse()
	must(os.MkdirAll(filepath.Join(*out, "frames"), 0o755))
	fmt.Println("before:", human())

	prof := filepath.Join(*out, "profile")
	os.RemoveAll(prof)
	cmd := exec.Command(*chrome, "--headless=new", "--no-first-run", "--no-default-browser-check",
		"--remote-debugging-port="+strconv.Itoa(*port), "--user-data-dir="+prof, "--window-size=1280,800", "about:blank")
	must(cmd.Start())
	defer exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()

	ctx := context.Background()
	events := make(chan cdp.Event, 1024)
	var c *cdp.Conn
	var err error
	for i := 0; i < 50; i++ {
		if c, err = cdp.Dial(ctx, *port, func(e cdp.Event) { events <- e }); err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	must(err)
	defer c.Close()

	var t struct{ TargetID string }
	must(json.Unmarshal(call(c, "", "Target.createTarget", map[string]any{"url": "data:text/html," + url.PathEscape(page)}), &t))
	var a struct{ SessionID string }
	must(json.Unmarshal(call(c, "", "Target.attachToTarget", map[string]any{"targetId": t.TargetID, "flatten": true}), &a))
	s := a.SessionID
	call(c, s, "Page.enable", nil)
	call(c, s, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1280, "height": 800, "deviceScaleFactor": 1, "mobile": false})
	time.Sleep(500 * time.Millisecond)

	// Input: click the field, insert text, press Enter, click the link.
	click(c, s, center(c, s, "q"))
	call(c, s, "Input.insertText", map[string]any{"text": "hello from cdp"})
	for _, typ := range []string{"keyDown", "keyUp"} {
		call(c, s, "Input.dispatchKeyEvent", map[string]any{"type": typ, "key": "Enter", "code": "Enter", "windowsVirtualKeyCode": 13, "text": "\r"})
	}
	click(c, s, center(c, s, "l"))
	fmt.Println("page result:", eval(c, s, "out.textContent+' | hash='+location.hash"))

	// Screenshot.
	t0 := time.Now()
	var shot struct{ Data string }
	must(json.Unmarshal(call(c, s, "Page.captureScreenshot", map[string]any{"format": "png"}), &shot))
	png, _ := base64.StdEncoding.DecodeString(shot.Data)
	must(os.WriteFile(filepath.Join(*out, "shot.png"), png, 0o644))
	fmt.Printf("screenshot: %d bytes, %d ms\n", len(png), time.Since(t0).Milliseconds())

	// Screencast: frames arrive as events; each needs an ack.
	call(c, s, "Page.startScreencast", map[string]any{"format": "jpeg", "quality": 80, "maxWidth": 1280, "maxHeight": 800, "everyNthFrame": 1})
	end := time.After(time.Duration(*secs) * time.Second)
	n := 0
	var first, last float64
	ts, _ := os.Create(filepath.Join(*out, "frames", "times.txt"))
loop:
	for {
		select {
		case e := <-events:
			if e.Method != "Page.screencastFrame" {
				continue
			}
			var f struct {
				Data      string
				SessionID int
				Metadata  struct{ Timestamp float64 }
			}
			must(json.Unmarshal(e.Params, &f))
			b, _ := base64.StdEncoding.DecodeString(f.Data)
			n++
			must(os.WriteFile(filepath.Join(*out, "frames", fmt.Sprintf("f%04d.jpg", n)), b, 0o644))
			fmt.Fprintf(ts, "%d %.3f\n", n, f.Metadata.Timestamp)
			if n == 1 {
				first = f.Metadata.Timestamp
			}
			last = f.Metadata.Timestamp
			call(c, s, "Page.screencastFrameAck", map[string]any{"sessionId": f.SessionID})
		case <-end:
			break loop
		}
	}
	ts.Close()
	call(c, s, "Page.stopScreencast", nil)
	fps := 0.0
	if last > first {
		fps = float64(n-1) / (last - first)
	}
	fmt.Printf("screencast: %d frames over %.2f s, %.1f fps\n", n, last-first, fps)
	fmt.Println("after:", human())
}

func call(c *cdp.Conn, s, m string, p any) json.RawMessage {
	r, err := c.Call(context.Background(), s, m, p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", m, err)
		os.Exit(1)
	}
	return r
}

func eval(c *cdp.Conn, s, expr string) string {
	var r struct{ Result struct{ Value any } }
	must(json.Unmarshal(call(c, s, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true}), &r))
	return fmt.Sprint(r.Result.Value)
}

func center(c *cdp.Conn, s, id string) [2]float64 {
	var p [2]float64
	must(json.Unmarshal([]byte(eval(c, s, "JSON.stringify((r=>[r.x+r.width/2,r.y+r.height/2])(document.getElementById('"+id+"').getBoundingClientRect()))")), &p))
	return p
}

func click(c *cdp.Conn, s string, p [2]float64) {
	for _, typ := range []string{"mousePressed", "mouseReleased"} {
		call(c, s, "Input.dispatchMouseEvent", map[string]any{"type": typ, "x": p[0], "y": p[1], "button": "left", "clickCount": 1})
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
