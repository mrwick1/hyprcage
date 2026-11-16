// Package headless is a headless Chrome driven only through the Chrome
// DevTools Protocol. It implements screen.Surface, so the screen tools act
// on it as they act on a Wayland screen.
package headless

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hexadecimil/hyprcage/internal/cdp"
	"github.com/hexadecimil/hyprcage/internal/proc"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// Options configure Start. Zero values pick the defaults.
type Options struct {
	Chrome     string // path of the Chrome binary
	Width      int    // default 1280
	Height     int    // default 800
	ProfileDir string // default: a new temp dir, removed on Close
	Port       int    // DevTools port on 127.0.0.1; default: a free one
}

// navigateTimeout bounds the wait for the load event.
const navigateTimeout = 15 * time.Second

// Browser is one headless Chrome and the page it drives.
type Browser struct {
	pid         int
	cmd         *exec.Cmd // nil when Attach found Chrome already running
	conn        *cdp.Conn
	port        int
	width       int
	height      int
	profile     string
	ownsProfile bool
	loaded      chan string   // session ids of Page.loadEventFired
	exited      chan struct{} // closes when Chrome has been reaped; nil after Attach
	closeOnce   sync.Once
	cast        atomic.Pointer[Screencast] // the running screencast; not under mu, because onEvent reads it

	mu             sync.Mutex // guards everything below, and serialises input
	session        string
	attachedTarget string // target id of the driven page
	pointer        pointerState
	ids            map[string]uint32 // target id -> toplevel id
	targets        map[uint32]string // toplevel id -> target id
}

var _ screen.Surface = (*Browser)(nil)

// FindChrome returns the first Chrome or Chromium binary it finds.
func FindChrome() (string, error) {
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	if p := installedChrome(); p != "" {
		return p, nil
	}
	return "", errors.New("headless: no Chrome found (looked for google-chrome, google-chrome-stable, chromium, chromium-browser)")
}

// Start runs Chrome and attaches to its page.
func Start(ctx context.Context, o Options) (*Browser, error) {
	if o.Chrome == "" {
		p, err := FindChrome()
		if err != nil {
			return nil, err
		}
		o.Chrome = p
	}
	if o.Width <= 0 {
		o.Width = 1280
	}
	if o.Height <= 0 {
		o.Height = 800
	}
	b := &Browser{width: o.Width, height: o.Height, profile: o.ProfileDir,
		loaded: make(chan string, 16), ids: map[string]uint32{}, targets: map[uint32]string{}}
	if b.profile == "" {
		d, err := os.MkdirTemp("", "hyprcage-headless-")
		if err != nil {
			return nil, err
		}
		b.profile, b.ownsProfile = d, true
	}
	b.port = o.Port
	if b.port == 0 {
		p, err := screen.FreePort()
		if err != nil {
			b.removeProfile()
			return nil, err
		}
		b.port = p
	}
	b.cmd = exec.Command(o.Chrome, "--headless=new", "--no-first-run", "--no-default-browser-check",
		"--remote-debugging-port="+strconv.Itoa(b.port), "--user-data-dir="+b.profile,
		fmt.Sprintf("--window-size=%d,%d", o.Width, o.Height), "about:blank")
	proc.Detach(b.cmd)
	if err := b.cmd.Start(); err != nil {
		b.removeProfile()
		return nil, fmt.Errorf("headless: start %s: %w", o.Chrome, err)
	}
	b.pid = b.cmd.Process.Pid
	b.exited = make(chan struct{})
	go func() { b.cmd.Wait(); close(b.exited) }()
	if err := b.attach(ctx); err != nil {
		b.Close()
		return nil, err
	}
	return b, nil
}

// Attach drives a Chrome that Start launched, maybe in another process: pid
// is its process id, port its DevTools port, and w and h the size Start
// gave it. Close kills that Chrome, as it does for a Browser from Start.
// Detach leaves it running.
func Attach(ctx context.Context, port, pid, w, h int) (*Browser, error) {
	b := &Browser{pid: pid, port: port, width: w, height: h,
		loaded: make(chan string, 16), ids: map[string]uint32{}, targets: map[uint32]string{}}
	if err := b.attach(ctx); err != nil {
		if b.conn != nil {
			b.conn.Close()
		}
		return nil, err
	}
	return b, nil
}

// Alive reports whether the DevTools connection still works.
func (b *Browser) Alive() bool {
	select {
	case <-b.conn.Done():
		return false
	default:
		return true
	}
}

// attach waits for DevTools, then attaches to the first page.
func (b *Browser) attach(ctx context.Context) error {
	deadline := time.Now().Add(20 * time.Second)
	var err error
	for {
		if b.conn, err = cdp.Dial(ctx, b.port, b.onEvent); err == nil {
			break
		}
		select {
		case <-b.exited:
			return errors.New("headless: Chrome exited before DevTools was ready")
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("headless: DevTools not ready: %w", err)
		}
	}
	var page string
	for time.Now().Before(deadline) {
		infos, err := b.targetInfos(ctx)
		if err != nil {
			return err
		}
		if len(infos) > 0 {
			page = infos[0].TargetID
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if page == "" {
		return errors.New("headless: Chrome has no page target")
	}
	return b.attachTo(ctx, page)
}

// attachTo makes targetID the driven page.
func (b *Browser) attachTo(ctx context.Context, targetID string) error {
	r, err := b.conn.Call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": targetID, "flatten": true})
	if err != nil {
		return err
	}
	var a struct{ SessionID string }
	if err := json.Unmarshal(r, &a); err != nil {
		return err
	}
	if _, err := b.conn.Call(ctx, a.SessionID, "Page.enable", nil); err != nil {
		return err
	}
	if _, err := b.conn.Call(ctx, a.SessionID, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width": b.width, "height": b.height, "deviceScaleFactor": 1, "mobile": false}); err != nil {
		return err
	}
	b.mu.Lock()
	b.session, b.attachedTarget = a.SessionID, targetID
	b.mu.Unlock()
	return nil
}

// onEvent runs on the read loop: it must not block.
func (b *Browser) onEvent(e cdp.Event) {
	if e.Method == "Page.screencastFrame" {
		if c := b.cast.Load(); c != nil {
			c.onFrame(e)
		}
	}
	if e.Method == "Page.loadEventFired" {
		select {
		case b.loaded <- e.SessionID:
		default:
		}
	}
}

// Port is the DevTools port on 127.0.0.1.
func (b *Browser) Port() int { return b.port }

// PID is the process id of Chrome.
func (b *Browser) PID() int { return b.pid }

// Close ends Chrome and its children and removes the temp profile.
func (b *Browser) Close() error {
	b.closeOnce.Do(func() {
		if b.conn != nil {
			b.conn.Close()
		}
		proc.KillTree(b.pid)
		if b.exited != nil {
			<-b.exited
		}
		b.removeProfile()
	})
	return nil
}

// Detach closes the DevTools connection and leaves Chrome running: for a
// process that only watches a screen it does not own.
func (b *Browser) Detach() error {
	if c := b.cast.Load(); c != nil {
		c.Stop()
	}
	return b.conn.Close()
}

// removeProfile deletes a profile dir that Start created. Chrome may
// still hold files for a moment after it is killed, so it retries.
func (b *Browser) removeProfile() {
	if !b.ownsProfile {
		return
	}
	for i := 0; i < 20; i++ {
		if os.RemoveAll(b.profile) == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (b *Browser) call(method string, params any) (json.RawMessage, error) {
	b.mu.Lock()
	s := b.session
	b.mu.Unlock()
	return b.conn.Call(context.Background(), s, method, params)
}

// Navigate loads url in the page and waits for its load event.
func (b *Browser) Navigate(ctx context.Context, url string) error {
	b.mu.Lock()
	s := b.session
	b.mu.Unlock()
	for len(b.loaded) > 0 {
		<-b.loaded
	}
	r, err := b.conn.Call(ctx, s, "Page.navigate", map[string]any{"url": url})
	if err != nil {
		return err
	}
	var n struct{ ErrorText string }
	if json.Unmarshal(r, &n) == nil && n.ErrorText != "" {
		return fmt.Errorf("headless: navigate %s: %s", url, n.ErrorText)
	}
	timeout := time.After(navigateTimeout)
	for {
		select {
		case got := <-b.loaded:
			if got == s {
				return nil
			}
		case <-timeout:
			return fmt.Errorf("headless: navigate %s: no load event in %s", url, navigateTimeout)
		case <-ctx.Done():
			return ctx.Err()
		case <-b.conn.Done():
			return errors.New("headless: Chrome is gone")
		}
	}
}

// Capture returns a screenshot of the page. cursor is ignored: the
// browser has no pointer.
func (b *Browser) Capture(cursor bool) (*image.RGBA, error) {
	r, err := b.call("Page.captureScreenshot", map[string]any{"format": "png"})
	if err != nil {
		return nil, err
	}
	var s struct{ Data string }
	if err := json.Unmarshal(r, &s); err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(s.Data)
	if err != nil {
		return nil, err
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	return out, nil
}

type targetInfo struct {
	TargetID string
	Type     string
	Title    string
}

// targetInfos lists the page targets.
func (b *Browser) targetInfos(ctx context.Context) ([]targetInfo, error) {
	r, err := b.conn.Call(ctx, "", "Target.getTargets", nil)
	if err != nil {
		return nil, err
	}
	var t struct{ TargetInfos []targetInfo }
	if err := json.Unmarshal(r, &t); err != nil {
		return nil, err
	}
	var pages []targetInfo
	for _, i := range t.TargetInfos {
		if i.Type == "page" {
			pages = append(pages, i)
		}
	}
	return pages, nil
}

// Toplevels lists the pages. The one the browser drives is Activated.
func (b *Browser) Toplevels() ([]wl.Toplevel, error) {
	infos, err := b.targetInfos(context.Background())
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []wl.Toplevel
	for _, i := range infos {
		out = append(out, wl.Toplevel{ID: b.idFor(i.TargetID), Title: i.Title, AppID: "chrome",
			Activated: b.attachedTarget == i.TargetID})
	}
	return out, nil
}

// idFor returns the stable toplevel id of a target. The caller holds mu.
func (b *Browser) idFor(targetID string) uint32 {
	id, ok := b.ids[targetID]
	if !ok {
		id = uint32(len(b.ids) + 1)
		b.ids[targetID] = id
		b.targets[id] = targetID
	}
	return id
}

// CloseToplevel closes one page by its id.
func (b *Browser) CloseToplevel(id uint32) error {
	b.mu.Lock()
	target, ok := b.targets[id]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("headless: no window with id %d", id)
	}
	if _, err := b.conn.Call(context.Background(), "", "Target.closeTarget", map[string]any{"targetId": target}); err != nil {
		return err
	}
	b.mu.Lock()
	wasActive := b.attachedTarget == target
	b.mu.Unlock()
	if !wasActive {
		return nil
	}
	// The driven page is gone: drive another one when there is one.
	infos, err := b.targetInfos(context.Background())
	if err != nil || len(infos) == 0 {
		return err
	}
	return b.attachTo(context.Background(), infos[0].TargetID)
}
