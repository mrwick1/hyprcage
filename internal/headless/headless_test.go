package headless

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/hexadecimil/offstage/internal/screen"
	"github.com/hexadecimil/offstage/internal/wl"
)

const testPage = `<!doctype html><title>first</title><body style="margin:0">
<div style="position:absolute;left:100px;top:100px;width:200px;height:100px;background:#ff0000"></div>
<form id=f style="position:absolute;left:400px;top:50px"><input id=q style="width:200px;height:30px"></form>
<button id=b style="position:absolute;left:400px;top:150px;width:100px;height:40px">b</button>
<div id=sc style="position:absolute;left:600px;top:300px;width:300px;height:200px;overflow:auto"><div style="height:2000px"></div></div>
<p id=out style="position:absolute;top:600px">none</p><p id=clk style="position:absolute;top:650px">none</p>
<script>
f.onsubmit=e=>{e.preventDefault();out.textContent='submit:'+q.value};
b.onclick=e=>{clk.textContent=e.detail+':'+(e.shiftKey?'shift':'plain')};
</script>`

func dataURL(page string) string { return "data:text/html," + url.PathEscape(page) }

// start runs a browser on testPage and closes it when the test ends.
func start(t *testing.T) *Browser {
	t.Helper()
	if _, err := FindChrome(); err != nil {
		t.Skip("no Chrome on this machine: " + err.Error())
	}
	b, err := Start(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	if err := b.Navigate(context.Background(), dataURL(testPage)); err != nil {
		t.Fatal(err)
	}
	return b
}

// eval runs a JavaScript expression in the page and returns its value as a string.
func eval(t *testing.T, b *Browser, expr string) string {
	t.Helper()
	r, err := b.call("Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true})
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Result struct{ Value any } }
	if err := json.Unmarshal(r, &v); err != nil {
		t.Fatal(err)
	}
	if v.Result.Value == nil {
		return ""
	}
	if s, ok := v.Result.Value.(string); ok {
		return s
	}
	j, _ := json.Marshal(v.Result.Value)
	return string(j)
}

func TestCaptureAndClose(t *testing.T) {
	b := start(t)
	img, err := b.Capture(false)
	if err != nil {
		t.Fatal(err)
	}
	if sz := img.Bounds().Size(); sz.X != 1280 || sz.Y != 800 {
		t.Fatalf("size = %v, want 1280x800", sz)
	}
	if r, g, bl, _ := img.At(200, 150).RGBA(); r>>8 != 255 || g>>8 != 0 || bl>>8 != 0 {
		t.Errorf("pixel in the div = %d,%d,%d, want 255,0,0", r>>8, g>>8, bl>>8)
	}
	if r, g, bl, _ := img.At(50, 50).RGBA(); r>>8 != 255 || g>>8 != 255 || bl>>8 != 255 {
		t.Errorf("pixel outside the div = %d,%d,%d, want white", r>>8, g>>8, bl>>8)
	}
	profile := b.profile
	b.Close()
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Errorf("profile dir %s not removed (%v)", profile, err)
	}
	if out, _ := exec.Command("pgrep", "-f", profile).Output(); len(out) > 0 {
		t.Errorf("processes left after Close: %s", strings.TrimSpace(string(out)))
	}
}

func TestInput(t *testing.T) {
	b := start(t)
	// click focuses the input
	if err := screen.Click(b, 450, 65, wl.ButtonLeft, 1, nil); err != nil {
		t.Fatal(err)
	}
	if got := eval(t, b, "document.activeElement.id"); got != "q" {
		t.Fatalf("focused element = %q, want q", got)
	}
	if err := b.Type("hello"); err != nil {
		t.Fatal(err)
	}
	if err := b.Key("Return"); err != nil {
		t.Fatal(err)
	}
	if got := eval(t, b, "out.textContent"); got != "submit:hello" {
		t.Errorf("out = %q, want submit:hello", got)
	}
	// ctrl+a, BackSpace empties the input
	if err := b.Key("ctrl+a"); err != nil {
		t.Fatal(err)
	}
	if err := b.Key("BackSpace"); err != nil {
		t.Fatal(err)
	}
	if got := eval(t, b, "q.value"); got != "" {
		t.Errorf("input after ctrl+a, BackSpace = %q, want empty", got)
	}
	// typed keys reach the input
	for _, k := range []string{"h", "shift+i", "1", "space", "minus"} {
		if err := b.Key(k); err != nil {
			t.Fatalf("Key(%q): %v", k, err)
		}
	}
	if got := eval(t, b, "q.value"); got != "hI1 -" {
		t.Errorf("input after typed keys = %q, want %q", got, "hI1 -")
	}
	// every documented key is accepted
	for _, k := range []string{"Tab", "Escape", "Delete", "Left", "Right", "Up", "Down", "Home", "End",
		"Page_Up", "Page_Down", "F1", "F12", "ctrl+shift+Tab", "alt+Left"} {
		if err := b.Key(k); err != nil {
			t.Errorf("Key(%q): %v", k, err)
		}
	}
	// unknown names are errors
	for _, k := range []string{"NoSuchKey", "bogus+a", "ctrl+", ""} {
		if err := b.Key(k); err == nil {
			t.Errorf("Key(%q) = nil, want an error", k)
		}
	}
}

func TestClicks(t *testing.T) {
	b := start(t)
	if err := screen.Click(b, 450, 170, wl.ButtonLeft, 2, nil); err != nil {
		t.Fatal(err)
	}
	if got := eval(t, b, "clk.textContent"); got != "2:plain" {
		t.Errorf("double click = %q, want 2:plain", got)
	}
	time.Sleep(600 * time.Millisecond) // past the multi-click gap
	if err := screen.Click(b, 450, 170, wl.ButtonLeft, 1, []string{"shift"}); err != nil {
		t.Fatal(err)
	}
	if got := eval(t, b, "clk.textContent"); got != "1:shift" {
		t.Errorf("shift click = %q, want 1:shift", got)
	}
	if err := b.HoldModifiers([]string{"nonsense"}); err == nil {
		t.Error("HoldModifiers(nonsense) = nil, want an error")
	}
}

func TestScroll(t *testing.T) {
	b := start(t)
	if err := screen.ScrollAt(b, 700, 400, "down", 3); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := eval(t, b, "sc.scrollTop"); got == "0" || got == "" {
		t.Errorf("scrollTop after scrolling down = %q, want > 0", got)
	}
	if err := screen.ScrollAt(b, 700, 400, "up", 10); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := eval(t, b, "sc.scrollTop"); got != "0" {
		t.Errorf("scrollTop after scrolling up = %q, want 0", got)
	}
}

func TestToplevels(t *testing.T) {
	b := start(t)
	r, err := b.conn.Call(context.Background(), "", "Target.createTarget",
		map[string]any{"url": dataURL("<title>second</title>x")})
	if err != nil {
		t.Fatal(err)
	}
	var created struct{ TargetID string }
	json.Unmarshal(r, &created)
	var tl []wl.Toplevel
	for i := 0; i < 50; i++ {
		if tl, err = b.Toplevels(); err != nil {
			t.Fatal(err)
		}
		if len(tl) == 2 && tl[0].Title != "" && tl[1].Title != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(tl) != 2 {
		t.Fatalf("Toplevels = %+v, want 2", tl)
	}
	titles := map[string]wl.Toplevel{}
	for _, w := range tl {
		titles[w.Title] = w
		if w.AppID != "chrome" {
			t.Errorf("AppID = %q, want chrome", w.AppID)
		}
	}
	first, second := titles["first"], titles["second"]
	if first.ID == 0 || second.ID == 0 || first.ID == second.ID {
		t.Fatalf("titles = %+v, want first and second with distinct ids", titles)
	}
	if !first.Activated || second.Activated {
		t.Errorf("Activated: first=%v second=%v, want true, false", first.Activated, second.Activated)
	}
	again, _ := b.Toplevels()
	for _, w := range again {
		if titles[w.Title].ID != w.ID {
			t.Errorf("id of %q changed: %d then %d", w.Title, titles[w.Title].ID, w.ID)
		}
	}
	if err := b.CloseToplevel(second.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50 && len(tl) != 1; i++ {
		time.Sleep(100 * time.Millisecond)
		tl, _ = b.Toplevels()
	}
	if len(tl) != 1 || tl[0].Title != "first" {
		t.Errorf("after CloseToplevel = %+v, want only first", tl)
	}
	if err := b.CloseToplevel(9999); err == nil {
		t.Error("CloseToplevel(9999) = nil, want an error")
	}
}

func TestAttach(t *testing.T) {
	b := start(t)
	eval(t, b, "q.value='kept'")
	a, err := Attach(context.Background(), b.Port(), b.PID(), 1280, 800)
	if err != nil {
		t.Fatal(err)
	}
	if got := eval(t, a, "q.value"); got != "kept" {
		t.Errorf("value through the attached browser = %q, want kept", got)
	}
	if img, err := a.Capture(false); err != nil || img.Bounds().Dx() != 1280 {
		t.Errorf("capture through the attached browser: %v", err)
	}
	if !a.Alive() {
		t.Error("attached browser is not alive")
	}
	a.Close() // kills Chrome: it is the same process as b's
	select {
	case <-b.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("Chrome still runs after the attached browser closed")
	}
	if b.Alive() {
		t.Error("original browser still alive")
	}
}

const motionPage = `<!doctype html><body style="margin:0;background:#fff">
<style>@keyframes m{from{left:0}to{left:1000px}}</style>
<div style="position:absolute;top:300px;width:200px;height:200px;background:#c00;animation:m 2s linear infinite alternate"></div>`

func TestScreencastFramesAndDetach(t *testing.T) {
	b := start(t)
	if err := b.Navigate(context.Background(), dataURL(motionPage)); err != nil {
		t.Fatal(err)
	}
	// A second connection, as the recorder has: Detach must leave Chrome alone.
	w, err := Attach(context.Background(), b.Port(), b.PID(), 1280, 800)
	if err != nil {
		t.Fatal(err)
	}
	cast, err := w.StartScreencast()
	if err != nil {
		t.Fatal(err)
	}
	first, err := cast.Capture(true)
	if err != nil {
		t.Fatal(err)
	}
	if first.Bounds().Size().X != 1280 || first.Bounds().Size().Y != 800 {
		t.Errorf("frame size %v", first.Bounds().Size())
	}
	changed, deadline := 0, time.Now().Add(time.Second)
	prev := first
	for time.Now().Before(deadline) {
		img, err := cast.Capture(true)
		if err != nil {
			t.Fatal(err)
		}
		if img != prev {
			changed++
			prev = img
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("%d new frames in 1 s", changed)
	if changed < 3 { // 47 without -race; the decoding is slow with it
		t.Errorf("only %d new frames in 1 s of animation", changed)
	}
	if err := w.Detach(); err != nil {
		t.Fatal(err)
	}
	if !b.Alive() {
		t.Error("Detach ended Chrome")
	}
	if _, err := b.Capture(false); err != nil {
		t.Errorf("the owner cannot capture after Detach: %v", err)
	}
}
