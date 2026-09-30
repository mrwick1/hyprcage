// Package devtools reads page internals over the DevTools port of a
// screen: JavaScript evaluation, the console, performance traces and heap
// snapshots.
package devtools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hexadecimil/hyprcage/internal/cdp"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

const hint = "open the page with browser_open, or launch the app with debug=true"

// MaxConsole caps the console buffer. The oldest messages go first.
const MaxConsole = 1000

// traceCategories are the categories the DevTools Performance panel
// records, so the file opens there with a full timeline.
var traceCategories = []string{
	"-*", "devtools.timeline", "v8.execute", "disabled-by-default-devtools.timeline",
	"disabled-by-default-devtools.timeline.frame", "toplevel", "blink.console", "blink.user_timing",
	"latencyInfo", "disabled-by-default-devtools.timeline.stack", "disabled-by-default-v8.cpu_profiler",
}

type conn interface {
	Call(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error)
	CallFor(ctx context.Context, timeout time.Duration, sessionID, method string, params any) (json.RawMessage, error)
	Done() <-chan struct{}
	Close() error
}

// Msg is one console message, uncaught exception or browser log entry.
type Msg struct {
	Level  string `json:"level"`
	Text   string `json:"text"`
	Source string `json:"source,omitempty"` // url:line
	Page   string `json:"page,omitempty"`   // url of the page that logged it
}

type target struct{ id, url string }

// Session holds one connection to a screen's browser. It attaches to every
// page as it appears and buffers the console from then on.
type Session struct {
	c    conn
	port int

	mu      sync.Mutex
	pages   map[string]target // by session id
	console []Msg
	dropped int
	heap    *os.File // sink of the heap snapshot being taken
	heapErr error
	traced  chan string // the trace stream handle, once Tracing.end completes
	emu     Emulation   // applied to every page, and to each new page on attach
}

// Emulation is the device the pages of a screen pretend to be. The zero
// value is the plain desktop browser.
type Emulation struct {
	Width     int     `json:"width,omitempty"`
	Height    int     `json:"height,omitempty"`
	Scale     float64 `json:"scale,omitempty"` // device pixel ratio
	Mobile    bool    `json:"mobile,omitempty"`
	Touch     bool    `json:"touch,omitempty"`
	UserAgent string  `json:"user_agent,omitempty"`
	Network   string  `json:"network,omitempty"`
	CPU       float64 `json:"cpu,omitempty"` // slowdown factor
	Color     string  `json:"color_scheme,omitempty"`
}

// networks are the DevTools throttling presets: latency in ms, throughput
// in bytes per second. Slow 4G is Fast 3G, as in DevTools and Lighthouse.
var networks = map[string]struct{ latency, down, up float64 }{
	"offline": {0, -1, -1},
	"slow-3g": {2000, 500e3 / 8 * 0.8, 500e3 / 8 * 0.8},
	"fast-3g": {562.5, 1.6e6 / 8 * 0.9, 750e3 / 8 * 0.9},
	"slow-4g": {562.5, 1.6e6 / 8 * 0.9, 750e3 / 8 * 0.9},
	"fast-4g": {165, 9e6 / 8 * 0.9, 1.5e6 / 8 * 0.9},
}

// Open connects to 127.0.0.1:port and starts buffering the console.
func Open(ctx context.Context, port int) (*Session, error) {
	s := &Session{pages: map[string]target{}, port: port}
	c, err := cdp.Dial(ctx, port, s.event)
	if err != nil {
		return nil, screen.Errf(screen.CodeCDP, hint, "%v", err)
	}
	s.c = c
	// Attach to the pages there now and to every page opened later.
	if _, err := c.Call(ctx, "", "Target.setAutoAttach", map[string]any{
		"autoAttach": true, "flatten": true, "waitForDebuggerOnStart": false,
		"filter": []map[string]any{{"type": "page"}, {"type": "iframe"}, {"exclude": true}},
	}); err != nil {
		c.Close()
		return nil, screen.Errf(screen.CodeCDP, hint, "%v", err)
	}
	// Target.targetInfoChanged, which keeps a page's URL current, only comes
	// with discovery on.
	if _, err := c.Call(ctx, "", "Target.setDiscoverTargets", map[string]any{
		"discover": true, "filter": []map[string]any{{"type": "page"}, {"exclude": true}},
	}); err != nil {
		c.Close()
		return nil, screen.Errf(screen.CodeCDP, hint, "%v", err)
	}
	return s, nil
}

// Alive reports whether the connection still stands.
func (s *Session) Alive() bool {
	select {
	case <-s.c.Done():
		return false
	default:
		return true
	}
}

// Port is the DevTools port the session is connected to.
func (s *Session) Port() int { return s.port }

// Close drops the connection.
func (s *Session) Close() error { return s.c.Close() }

// event runs on the read loop: it must not block and must not Call.
func (s *Session) event(ev cdp.Event) {
	switch ev.Method {
	case "Target.attachedToTarget":
		var p struct {
			SessionID  string `json:"sessionId"`
			TargetInfo struct {
				ID   string `json:"targetId"`
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(ev.Params, &p) != nil {
			return
		}
		s.mu.Lock()
		page := p.TargetInfo.Type == "page"
		if page {
			s.pages[p.SessionID] = target{p.TargetInfo.ID, p.TargetInfo.URL}
		}
		emu := s.emu
		s.mu.Unlock()
		// Runtime.enable replays the console messages the page already holds.
		go func() {
			ctx := context.Background()
			s.c.Call(ctx, p.SessionID, "Runtime.enable", nil)
			s.c.Call(ctx, p.SessionID, "Log.enable", nil)
			// A popup waits for the debugger despite waitForDebuggerOnStart
			// false, and holds its opener's renderer until released.
			s.c.Call(ctx, p.SessionID, "Runtime.runIfWaitingForDebugger", nil)
			if page && emu != (Emulation{}) {
				s.apply(ctx, p.SessionID, emu)
			}
		}()
	case "Target.detachedFromTarget":
		var p struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			s.mu.Lock()
			delete(s.pages, p.SessionID)
			s.mu.Unlock()
		}
	case "Target.targetInfoChanged":
		var p struct {
			TargetInfo struct {
				ID  string `json:"targetId"`
				URL string `json:"url"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			s.mu.Lock()
			for sid, t := range s.pages {
				if t.id == p.TargetInfo.ID {
					s.pages[sid] = target{t.id, p.TargetInfo.URL}
				}
			}
			s.mu.Unlock()
		}
	case "Runtime.consoleAPICalled":
		var p struct {
			Type       string         `json:"type"`
			Args       []remoteObject `json:"args"`
			StackTrace *struct {
				CallFrames []frame `json:"callFrames"`
			} `json:"stackTrace"`
		}
		if json.Unmarshal(ev.Params, &p) != nil {
			return
		}
		parts := make([]string, len(p.Args))
		for i, a := range p.Args {
			parts[i] = a.String()
		}
		m := Msg{Level: p.Type, Text: strings.Join(parts, " ")}
		if p.StackTrace != nil && len(p.StackTrace.CallFrames) > 0 {
			m.Source = p.StackTrace.CallFrames[0].String()
		}
		s.add(ev.SessionID, m)
	case "Runtime.exceptionThrown":
		var p struct {
			Details exception `json:"exceptionDetails"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			s.add(ev.SessionID, Msg{Level: "exception", Text: p.Details.String(), Source: p.Details.source()})
		}
	case "Log.entryAdded":
		var p struct {
			Entry struct {
				Level string `json:"level"`
				Text  string `json:"text"`
				URL   string `json:"url"`
				Line  *int   `json:"lineNumber"`
			} `json:"entry"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			e := p.Entry
			src := e.URL
			if src != "" && e.Line != nil {
				src = fmt.Sprintf("%s:%d", e.URL, *e.Line+1)
			}
			s.add(ev.SessionID, Msg{Level: e.Level, Text: e.Text, Source: src})
		}
	case "HeapProfiler.addHeapSnapshotChunk":
		var p struct {
			Chunk string `json:"chunk"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			s.mu.Lock()
			if s.heap != nil && s.heapErr == nil {
				_, s.heapErr = s.heap.WriteString(p.Chunk)
			}
			s.mu.Unlock()
		}
	case "Tracing.tracingComplete":
		var p struct {
			Stream string `json:"stream"`
		}
		json.Unmarshal(ev.Params, &p)
		s.mu.Lock()
		if s.traced != nil {
			select {
			case s.traced <- p.Stream:
			default:
			}
		}
		s.mu.Unlock()
	}
}

func (s *Session) add(sessionID string, m Msg) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m.Page = s.pages[sessionID].url
	if len(s.console) >= MaxConsole {
		s.console = s.console[1:]
		s.dropped++
	}
	s.console = append(s.console, m)
}

// Console returns the buffered messages and how many were dropped for the
// cap. clear empties the buffer afterwards.
func (s *Session) Console(clear bool) ([]Msg, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, dropped := append([]Msg(nil), s.console...), s.dropped
	if clear {
		s.console, s.dropped = nil, 0
	}
	return out, dropped
}

// page returns the session of the page whose URL contains match, or the
// only page when match is empty.
func (s *Session) page(match string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var hits []string
	var urls []string
	for sid, t := range s.pages {
		urls = append(urls, t.url)
		if match == "" || strings.Contains(t.url, match) {
			hits = append(hits, sid)
		}
	}
	switch {
	case len(hits) == 1:
		return hits[0], nil
	case len(s.pages) == 0:
		return "", screen.Errf(screen.CodeCDP, hint, "the browser has no page open")
	case len(hits) == 0:
		return "", screen.Errf(screen.CodeUnsupported, "pass part of one of these URLs as page", "no page URL contains %q; pages: %s", match, strings.Join(urls, ", "))
	default:
		return "", screen.Errf(screen.CodeUnsupported, "pass part of one URL as page", "%d pages match; pages: %s", len(hits), strings.Join(urls, ", "))
	}
}

// Eval runs expression in the page and returns its value as JSON, after
// awaiting a promise.
func (s *Session) Eval(ctx context.Context, page, expression string, timeout time.Duration) (json.RawMessage, error) {
	sid, err := s.page(page)
	if err != nil {
		return nil, err
	}
	raw, err := s.c.CallFor(ctx, timeout, sid, "Runtime.evaluate", map[string]any{
		"expression": expression, "awaitPromise": true,
		"userGesture": true, "replMode": true, "objectGroup": evalGroup,
	})
	defer func() {
		go s.c.Call(context.Background(), sid, "Runtime.releaseObjectGroup", map[string]any{"objectGroup": evalGroup})
	}()
	r, err := evalResult(raw, err)
	if err == nil && r.Result.ObjectID != "" {
		// REPL mode awaits the script, not its value, so a promise comes back
		// as itself. Returning the object from a function awaits it and
		// serializes the result.
		raw, err = s.c.CallFor(ctx, timeout, sid, "Runtime.callFunctionOn", map[string]any{
			"objectId": r.Result.ObjectID, "functionDeclaration": "function() { return this }",
			"returnByValue": true, "awaitPromise": true,
		})
		r, err = evalResult(raw, err)
	}
	if err != nil {
		return nil, err
	}
	if r.Result.Value != nil {
		return r.Result.Value, nil
	}
	// undefined, or a value JSON cannot carry: NaN, Infinity, a bigint.
	return json.Marshal(r.Result.String())
}

// Emulate makes every page, open now or later, pretend to be the device e
// describes. The zero value returns them to the plain desktop browser.
func (s *Session) Emulate(ctx context.Context, e Emulation) error {
	if (e.Width == 0) != (e.Height == 0) || e.Width < 0 || e.Height < 0 {
		return screen.Errf(screen.CodeUnsupported, "pass both width and height, or neither", "viewport %dx%d", e.Width, e.Height)
	}
	if _, ok := networks[e.Network]; e.Network != "" && !ok {
		return screen.Errf(screen.CodeUnsupported, "use offline, slow-3g, fast-3g, slow-4g or fast-4g", "unknown network %q", e.Network)
	}
	if e.CPU != 0 && e.CPU < 1 {
		return screen.Errf(screen.CodeUnsupported, "pass a slowdown factor of 1 or more", "cpu %g", e.CPU)
	}
	if e.Color != "" && e.Color != "dark" && e.Color != "light" {
		return screen.Errf(screen.CodeUnsupported, "use dark or light", "unknown color_scheme %q", e.Color)
	}
	s.mu.Lock()
	s.emu = e
	sids := make([]string, 0, len(s.pages))
	for sid := range s.pages {
		sids = append(sids, sid)
	}
	s.mu.Unlock()
	if len(sids) == 0 {
		return screen.Errf(screen.CodeCDP, hint, "the browser has no page open")
	}
	for _, sid := range sids {
		if err := s.apply(ctx, sid, e); err != nil {
			return screen.Errf(screen.CodeCDP, "", "%v", err)
		}
	}
	return nil
}

// apply sets every override of e on one page, so a field left out clears
// what an earlier call set.
func (s *Session) apply(ctx context.Context, sid string, e Emulation) error {
	// A metrics override with every field zero still pins the viewport.
	type command struct {
		method string
		params any
	}
	metrics := command{"Emulation.clearDeviceMetricsOverride", nil}
	if e.Width != 0 || e.Scale != 0 || e.Mobile {
		metrics.method = "Emulation.setDeviceMetricsOverride"
		metrics.params = map[string]any{"width": e.Width, "height": e.Height, "deviceScaleFactor": e.Scale, "mobile": e.Mobile}
	}
	n, ok := networks[e.Network]
	if !ok {
		n.down, n.up = -1, -1
	}
	media := []map[string]string{} // not nil: the browser rejects null
	if e.Color != "" {
		media = []map[string]string{{"name": "prefers-color-scheme", "value": e.Color}}
	}
	for _, c := range []command{
		metrics,
		{"Emulation.setTouchEmulationEnabled", map[string]any{"enabled": e.Touch}},
		{"Emulation.setUserAgentOverride", map[string]any{"userAgent": e.UserAgent}},
		{"Emulation.setCPUThrottlingRate", map[string]any{"rate": max(e.CPU, 1)}},
		{"Emulation.setEmulatedMedia", map[string]any{"features": media}},
		// Throttling needs the network domain on; the page stays unthrottled without it.
		{"Network.enable", nil},
		{"Network.emulateNetworkConditions", map[string]any{
			"offline": e.Network == "offline", "latency": n.latency, "downloadThroughput": n.down, "uploadThroughput": n.up,
		}},
	} {
		if _, err := s.c.Call(ctx, sid, c.method, c.params); err != nil {
			return err
		}
	}
	return nil
}

// evalGroup holds the objects an Eval keeps alive until it returns.
const evalGroup = "hyprcage-eval"

type evalReply struct {
	Result    remoteObject `json:"result"`
	Exception *exception   `json:"exceptionDetails"`
}

func evalResult(raw json.RawMessage, err error) (evalReply, error) {
	var r evalReply
	if err != nil {
		return r, screen.Errf(screen.CodeCDP, "", "%v", err)
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, err
	}
	if r.Exception != nil {
		return r, fmt.Errorf("the page threw: %s", r.Exception.String())
	}
	return r, nil
}

// HeapSnapshot writes a heap snapshot of the page to path and returns its
// size in bytes. DevTools' Memory panel loads the file.
func (s *Session) HeapSnapshot(ctx context.Context, page, path string) (int64, error) {
	sid, err := s.page(page)
	if err != nil {
		return 0, err
	}
	f, err := create(path)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	s.heap, s.heapErr = f, nil
	s.mu.Unlock()
	// The chunks all arrive before the reply.
	_, err = s.c.CallFor(ctx, 2*time.Minute, sid, "HeapProfiler.takeHeapSnapshot", map[string]any{"reportProgress": false})
	s.mu.Lock()
	s.heap, err = nil, firstErr(err, s.heapErr)
	s.mu.Unlock()
	return finish(f, path, err)
}

// Tracing reports whether a trace runs.
func (s *Session) Tracing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.traced != nil
}

// StartTrace starts a browser-wide performance trace.
func (s *Session) StartTrace(ctx context.Context) error {
	if s.Tracing() {
		return screen.Errf(screen.CodeLimit, "stop it with action stop", "a trace already runs")
	}
	_, err := s.c.Call(ctx, "", "Tracing.start", map[string]any{
		"transferMode": "ReturnAsStream",
		"traceConfig":  map[string]any{"includedCategories": traceCategories[1:], "excludedCategories": []string{"*"}},
	})
	if err != nil {
		return screen.Errf(screen.CodeCDP, "", "%v", err)
	}
	s.mu.Lock()
	s.traced = make(chan string, 1)
	s.mu.Unlock()
	return nil
}

// StopTrace ends the trace and writes it to path, which the DevTools
// Performance panel loads. It returns the size in bytes.
func (s *Session) StopTrace(ctx context.Context, path string) (int64, error) {
	s.mu.Lock()
	done := s.traced
	s.mu.Unlock()
	if done == nil {
		return 0, screen.Errf(screen.CodeUnsupported, "start one with action start", "no trace runs")
	}
	defer func() {
		s.mu.Lock()
		s.traced = nil
		s.mu.Unlock()
	}()
	if _, err := s.c.Call(ctx, "", "Tracing.end", nil); err != nil {
		return 0, screen.Errf(screen.CodeCDP, "", "%v", err)
	}
	var stream string
	select {
	case stream = <-done:
	case <-time.After(time.Minute):
		return 0, screen.Errf(screen.CodeTimeout, "", "the browser did not finish the trace within a minute")
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	f, err := create(path)
	if err != nil {
		return 0, err
	}
	defer s.c.Call(context.Background(), "", "IO.close", map[string]any{"handle": stream})
	for err == nil {
		var r struct {
			Data   string `json:"data"`
			Base64 bool   `json:"base64Encoded"`
			EOF    bool   `json:"eof"`
		}
		var raw json.RawMessage
		if raw, err = s.c.Call(ctx, "", "IO.read", map[string]any{"handle": stream, "size": 1 << 20}); err != nil {
			break
		}
		if err = json.Unmarshal(raw, &r); err != nil {
			break
		}
		data := []byte(r.Data)
		if r.Base64 {
			if data, err = base64.StdEncoding.DecodeString(r.Data); err != nil {
				break
			}
		}
		if _, err = f.Write(data); err != nil || r.EOF {
			break
		}
	}
	return finish(f, path, err)
}

// DefaultPath is where an artifact goes when the caller names none:
// ~/.cache/hyprcage/devtools/<screen>-<time>.<ext>.
func DefaultPath(screenName, ext string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "hyprcage", "devtools", fmt.Sprintf("%s-%s.%s", screenName, time.Now().Format("20060102-150405"), ext))
}

func create(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
}

// finish closes f and returns its size. On err the partial file is removed.
func finish(f *os.File, path string, err error) (int64, error) {
	err = firstErr(err, f.Close())
	if err != nil {
		os.Remove(path)
		var se *screen.Error
		if errors.As(err, &se) {
			return 0, err
		}
		return 0, screen.Errf(screen.CodeCDP, "", "%v", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

func firstErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

type remoteObject struct {
	Type        string          `json:"type"`
	Value       json.RawMessage `json:"value"`
	Unserial    string          `json:"unserializableValue"`
	Description string          `json:"description"`
	ObjectID    string          `json:"objectId"`
}

// String renders the object as the console shows it.
func (o remoteObject) String() string {
	switch {
	case o.Unserial != "":
		return o.Unserial
	case o.Value != nil:
		var str string
		if json.Unmarshal(o.Value, &str) == nil {
			return str
		}
		return string(o.Value)
	case o.Description != "":
		return o.Description
	default:
		return o.Type // undefined
	}
}

type frame struct {
	URL  string `json:"url"`
	Line int    `json:"lineNumber"`
}

func (f frame) String() string {
	if f.URL == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", f.URL, f.Line+1)
}

type exception struct {
	Text      string        `json:"text"`
	URL       string        `json:"url"`
	Line      int           `json:"lineNumber"`
	Exception *remoteObject `json:"exception"`
}

func (e exception) String() string {
	if e.Exception != nil && e.Exception.Description != "" {
		return e.Exception.Description
	}
	if e.Exception != nil {
		return e.Text + " " + e.Exception.String()
	}
	return e.Text
}

func (e exception) source() string { return frame{e.URL, e.Line}.String() }
