// Package mcpserver exposes hyprcage as an MCP server on stdio (cahier §6.2):
// one process per agent session, blocked on stdin, no Hyprland or Wayland
// connection until a screen exists (N1).
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/desktop"
	"github.com/hexadecimil/hyprcage/internal/devtools"
	"github.com/hexadecimil/hyprcage/internal/perceive"
	"github.com/hexadecimil/hyprcage/internal/record"
	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/session"
	"github.com/hexadecimil/hyprcage/internal/setup"
	"github.com/hexadecimil/hyprcage/internal/sysd"
	"github.com/hexadecimil/hyprcage/internal/version"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

const instructions = `hyprcage gives you a virtual screen on the human's Hyprland desktop. Anything graphical you run for yourself goes there, never on the human's screens.
Workflow: screen_create -> app_launch -> screenshot / click / type / key / scroll / drag / wait -> screen_destroy as soon as you are done.
Read a screen as text first: snapshot, then act by ref, and find (with timeout_ms) instead of wait plus screenshot. Launch Chromium and Electron apps with debug=true. Take a screenshot only when the snapshot does not explain the screen.
Coordinates are screen pixels (1280x800 by default). A screenshot costs ~1300 tokens: ask for screenshot_after only when you need to see the result, and prefer wait (stable_ms or title) over blind delays.
Never launch apps outside app_launch. Outside the desktop_* tools, never touch the human's focus, cursor or workspaces.
Recording: record_start / record_stop (a screen, or target "desktop"). Browser: browser_open, then snapshot / act / find; devtools_eval, devtools_console, devtools_trace and devtools_heap read page internals of any screen launched with DevTools. Clipboard: clipboard_get / clipboard_set (the screen's, not the human's).
The human's own windows: desktop_windows, desktop_focus, desktop_move, desktop_type, desktop_key. They are not silent (each key briefly takes the human's focus) and refuse while the session is locked; prefer an agent screen whenever the task allows it.`

// Server holds the per-session state.
type Server struct {
	cfg config.Config
	mu  sync.Mutex // one tool at a time: the Wayland client is single-threaded
	ctx *screen.Ctx
	// tables holds one ref table per screen instance (tableKey), guarded by mu.
	tables map[string]*perceive.Table
	// devtools holds one DevTools session per screen instance (tableKey), guarded by mu.
	devtools map[string]*devtools.Session
}

// Run serves MCP on stdin/stdout until the client goes away, then applies
// mechanism M1 of cahier §5.3.
func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	s := newServer(cfg)
	s.adopt()
	srv := s.mcpServer()
	stop := make(chan struct{})
	go s.heartbeat(stop)
	err = srv.Run(context.Background(), &mcp.StdioTransport{})
	close(stop)
	screen.CloseAll()
	s.onDisconnect()
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, mcp.ErrConnectionClosed) {
		return err
	}
	return nil
}

func newServer(cfg config.Config) *Server { return &Server{cfg: cfg} }

// mcpServer builds the MCP server with every tool registered.
func (s *Server) mcpServer() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "hyprcage", Version: version.Version},
		&mcp.ServerOptions{Instructions: instructions})
	s.register(srv)
	return srv
}

// adopt takes over the records of this session left by a previous server
// process (restart after compaction or --resume), cahier §5.2.
func (s *Server) adopt() {
	id := session.Current()
	if id.SessionID == "" {
		return
	}
	recs, _ := registry.List()
	for _, rec := range recs {
		if rec.Owner.SessionID != id.SessionID {
			continue
		}
		rec.Owner = id.Owner()
		_ = registry.Save(rec)
	}
}

// heartbeat touches the session's records every 30 s (cahier §5.2); with no
// owned screen it does nothing.
func (s *Server) heartbeat(stop <-chan struct{}) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.touchAll()
		}
	}
}

func (s *Server) touchAll() {
	id := session.Current()
	recs, _ := registry.List()
	for _, rec := range recs {
		if screen.CheckOwner(rec, id) == nil {
			_ = registry.Touch(rec.Name)
		}
	}
}

// onDisconnect is M1: nothing while the parent lives (only the server
// restarted); otherwise a deferred gc, because Claude Desktop may relaunch
// the session under a new pid within the grace period.
func (s *Server) onDisconnect() {
	id := session.Current()
	if session.PIDAlive(id.PID, id.PIDStart) {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := []string{exe, "gc"}
	if id.SessionID != "" {
		cmd = append(cmd, "--session", id.SessionID)
	}
	if sysd.Available() {
		if sysd.ScheduleOnce(s.cfg.SessionGrace, "", cmd) == nil {
			return
		}
	}
	time.Sleep(s.cfg.SessionGrace)
	_, _ = record.StopSession(id.SessionID)
	if c, err := screen.Connect(s.cfg); err == nil {
		_, _ = screen.GC(c, screen.GCOptions{Session: id.SessionID})
	}
}

func (s *Server) hypr() (*screen.Ctx, error) {
	if s.ctx == nil {
		c, err := screen.Connect(s.cfg)
		if err != nil {
			return nil, err
		}
		s.ctx = c
	}
	return s.ctx, nil
}

// resolve finds the screen (empty = the session's only one), checks the
// owner, touches the heartbeat and opens its Wayland connection on demand.
func (s *Server) resolve(name string, withWL bool) (*registry.Screen, *wl.Client, error) {
	c, err := s.hypr()
	if err != nil {
		return nil, nil, err
	}
	id := session.Current()
	rec, err := screen.Resolve(c, name, id)
	if err != nil {
		return nil, nil, err
	}
	if err := screen.CheckOwner(rec, id); err != nil {
		return nil, nil, err
	}
	_ = registry.Touch(rec.Name)
	if !withWL {
		return rec, nil, nil
	}
	cl, err := screen.Open(rec)
	if err != nil {
		return nil, nil, err
	}
	return rec, cl, nil
}

// --- results ----------------------------------------------------------------

func textResult(v any) *mcp.CallToolResult {
	data, _ := json.MarshalIndent(v, "", "  ")
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}
}

func imageResult(res *screen.ShotResult, rec *registry.Screen) *mcp.CallToolResult {
	geom, _ := json.Marshal(map[string]any{
		"screen": rec.Name, "width": res.Width, "height": res.Height, "scale": res.Scale,
		"screen_width": res.ScreenW, "screen_height": res.ScreenH, "region": res.Region,
		"note": "coordinates for click/move/drag are in screen pixels; divide image coordinates by scale",
	})
	return &mcp.CallToolResult{Content: []mcp.Content{
		&mcp.ImageContent{Data: res.Data, MIMEType: res.MIME},
		&mcp.TextContent{Text: string(geom)},
	}}
}

// afterAction optionally appends a screenshot to an action's result.
func (s *Server) afterAction(rec *registry.Screen, cl *wl.Client, want bool, settleMs int) (*mcp.CallToolResult, error) {
	if !want {
		return textResult(map[string]string{"status": "ok"}), nil
	}
	if settleMs <= 0 {
		settleMs = 150
	}
	time.Sleep(time.Duration(settleMs) * time.Millisecond)
	res, err := screen.Shot(cl, screen.ShotOptions{Cursor: true, MaxSide: s.cfg.ShotMaxSide, MaxBytes: s.cfg.ShotMaxBytes})
	if err != nil {
		return nil, err
	}
	return imageResult(res, rec), nil
}

// --- tool inputs ------------------------------------------------------------

type createIn struct {
	Name   string `json:"name,omitempty" jsonschema:"optional screen name matching [a-z0-9-]{1,32}; default hc-<random>"`
	Size   string `json:"size,omitempty" jsonschema:"WxH in pixels, default 1280x800 (configurable); keep every side at or below 2000"`
	Mirror *bool  `json:"mirror,omitempty" jsonschema:"open a mirror window on one of the human's workspaces so they can watch. Leave it out to follow the human's configuration, which is what they want by default. Pass true when they asked to see the work or when showing it is the point, false for a screen they have no reason to watch"`
}

type mirrorIn struct {
	Screen string `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
	Open   *bool  `json:"open,omitempty" jsonschema:"true opens the mirror window (default), false closes it"`
}

type screenIn struct {
	Screen string `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
}

type listIn struct {
	All bool `json:"all,omitempty" jsonschema:"include screens of other sessions"`
}

type launchIn struct {
	Screen       string            `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
	Command      []string          `json:"command" jsonschema:"argv of the application, e.g. [\"chromium\",\"--ozone-platform=wayland\",\"--user-data-dir=/tmp/hc-profile\",\"--no-first-run\",\"https://example.org\"]"`
	Cwd          string            `json:"cwd,omitempty" jsonschema:"working directory"`
	Env          map[string]string `json:"env,omitempty" jsonschema:"extra environment variables"`
	WaitWindowMs *int              `json:"wait_window_ms,omitempty" jsonschema:"wait up to this long for a new window (default 10000, 0 = return immediately)"`
	Debug        bool              `json:"debug,omitempty" jsonschema:"Chromium or Electron app: open the DevTools port that snapshot, act and find read"`
	Workspace    int               `json:"workspace,omitempty" jsonschema:"desktop only: workspace to launch on, without switching to it (default: the active workspace)"`
}

type closeIn struct {
	Screen   string `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
	Toplevel uint32 `json:"toplevel" jsonschema:"window id from the windows tool"`
}

type shotIn struct {
	Screen   string       `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
	Scale    float64      `json:"scale,omitempty" jsonschema:"0.25 to 1 (default 1); the reply states the scale actually used"`
	Region   *screen.Rect `json:"region,omitempty" jsonschema:"capture only this rectangle, in screen pixels"`
	Format   string       `json:"format,omitempty" jsonschema:"png (default) or jpeg"`
	Cursor   *bool        `json:"cursor,omitempty" jsonschema:"draw the pointer (default true)"`
	SettleMs int          `json:"settle_ms,omitempty" jsonschema:"wait this long before capturing, for a toast or an animation to settle (default 0)"`
}

type clickIn struct {
	Screen          string   `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
	X               int      `json:"x" jsonschema:"screen pixel x"`
	Y               int      `json:"y" jsonschema:"screen pixel y"`
	Button          string   `json:"button,omitempty" jsonschema:"left (default), right or middle"`
	Count           int      `json:"count,omitempty" jsonschema:"1 (default), 2 for a double click, 3 for a triple click"`
	Modifiers       []string `json:"modifiers,omitempty" jsonschema:"modifiers held during the click: ctrl, shift, alt, super"`
	ScreenshotAfter bool     `json:"screenshot_after,omitempty" jsonschema:"return a screenshot after the action (default false)"`
	SettleMs        int      `json:"settle_ms,omitempty" jsonschema:"delay before that screenshot, default 150"`
}

type moveIn struct {
	Screen string `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
	X      int    `json:"x" jsonschema:"screen pixel x"`
	Y      int    `json:"y" jsonschema:"screen pixel y"`
}

type scrollIn struct {
	Screen          string `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
	X               int    `json:"x" jsonschema:"screen pixel x"`
	Y               int    `json:"y" jsonschema:"screen pixel y"`
	Direction       string `json:"direction" jsonschema:"up, down, left or right"`
	Amount          int    `json:"amount,omitempty" jsonschema:"wheel clicks, default 3"`
	ScreenshotAfter bool   `json:"screenshot_after,omitempty" jsonschema:"return a screenshot after the action (default false)"`
	SettleMs        int    `json:"settle_ms,omitempty" jsonschema:"delay before that screenshot, default 150"`
}

type dragIn struct {
	Screen          string `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
	X1              int    `json:"x1"`
	Y1              int    `json:"y1"`
	X2              int    `json:"x2"`
	Y2              int    `json:"y2"`
	DurationMs      int    `json:"duration_ms,omitempty" jsonschema:"time between press and release, default 250"`
	ScreenshotAfter bool   `json:"screenshot_after,omitempty" jsonschema:"return a screenshot after the action (default false)"`
	SettleMs        int    `json:"settle_ms,omitempty" jsonschema:"delay before that screenshot, default 150"`
}

type typeIn struct {
	Screen          string `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
	Text            string `json:"text" jsonschema:"Unicode text to type; \\n presses Return, \\t presses Tab"`
	ScreenshotAfter bool   `json:"screenshot_after,omitempty" jsonschema:"return a screenshot after the action (default false)"`
	SettleMs        int    `json:"settle_ms,omitempty" jsonschema:"delay before that screenshot, default 150"`
}

type keyIn struct {
	Screen          string   `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
	Keys            []string `json:"keys" jsonschema:"combinations pressed in order, xkb keysym names, e.g. [\"ctrl+l\", \"Return\", \"alt+F4\"]"`
	ScreenshotAfter bool     `json:"screenshot_after,omitempty" jsonschema:"return a screenshot after the action (default false)"`
	SettleMs        int      `json:"settle_ms,omitempty" jsonschema:"delay before that screenshot, default 150"`
}

type waitIn struct {
	Screen    string `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
	Ms        int    `json:"ms,omitempty" jsonschema:"plain delay in milliseconds"`
	StableMs  int    `json:"stable_ms,omitempty" jsonschema:"return once the image has not changed for this long"`
	Title     string `json:"title,omitempty" jsonschema:"return once a window title matches this regular expression"`
	TimeoutMs int    `json:"timeout_ms,omitempty" jsonschema:"give up after this long, default 5000"`
}

type batchAction struct {
	Op        string   `json:"op" jsonschema:"click, double_click, move, scroll, drag, type, key or wait"`
	X         int      `json:"x,omitempty"`
	Y         int      `json:"y,omitempty"`
	X2        int      `json:"x2,omitempty"`
	Y2        int      `json:"y2,omitempty"`
	Button    string   `json:"button,omitempty"`
	Count     int      `json:"count,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
	Direction string   `json:"direction,omitempty"`
	Amount    int      `json:"amount,omitempty"`
	Text      string   `json:"text,omitempty"`
	Keys      []string `json:"keys,omitempty"`
	Ms        int      `json:"ms,omitempty"`
	StableMs  int      `json:"stable_ms,omitempty"`
	Title     string   `json:"title,omitempty"`
	TimeoutMs int      `json:"timeout_ms,omitempty"`
}

type batchIn struct {
	Screen          string        `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
	Actions         []batchAction `json:"actions" jsonschema:"actions executed in order; stops at the first error"`
	ScreenshotAfter bool          `json:"screenshot_after,omitempty" jsonschema:"return a screenshot after the last action"`
	SettleMs        int           `json:"settle_ms,omitempty" jsonschema:"delay before that screenshot, default 150"`
}

type batchResult struct {
	Op     string `json:"op"`
	Status string `json:"status"` // ok, error, skipped
	Error  string `json:"error,omitempty"`
}

// --- registration -----------------------------------------------------------

type handler[In any] func(in In) (*mcp.CallToolResult, error)

// tool wraps a handler: serialised, errors become isError results.
func tool[In any](s *Server, srv *mcp.Server, name, desc string, h handler[In]) {
	mcp.AddTool(srv, &mcp.Tool{Name: name, Description: desc},
		func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			res, err := h(in)
			if err != nil {
				return nil, nil, err
			}
			return res, nil, nil
		})
}

func (s *Server) register(srv *mcp.Server) {
	tool(s, srv, "setup", "Install what hyprcage needs on this machine (cage, ffmpeg, wl-clipboard, and tesseract with its English data for the OCR snapshot) through the package manager. A password dialog opens on the human's screen: tell the human before calling it. Use it when screen_create fails with cage_missing, or when recording, the clipboard or the OCR snapshot fails because ffmpeg, wl-clipboard or tesseract is missing, then retry.", s.setup)
	tool(s, srv, "screen_create", "Create a virtual screen for yourself: a compositor with an output of its own, invisible to the human's desktop. Returns its name; use it in every other tool. Destroy it when done. The reply carries mirror_note when there is no mirror window for the human, and why.", s.screenCreate)
	tool(s, srv, "screen_destroy", "Close a screen you created: its applications, its mirror window and its compositor.", s.screenDestroy)
	tool(s, srv, "screen_list", "List your screens (or every session's with all=true).", s.screenList)
	tool(s, srv, "mirror", "Open (open=true, default) or close (open=false) the human's mirror window for a screen, without touching the screen. Use it when the human asks to watch, or to stop showing a screen.", s.mirror)
	tool(s, srv, "app_launch", "Run a graphical application inside a screen. Returns its pid and, once it appears, its window.", s.appLaunch)
	tool(s, srv, "app_close", "Ask a window of the screen to close gracefully. Window ids change when a window remaps, so take the id from windows rather than from an older reply.", s.appClose)
	tool(s, srv, "windows", "List the windows of a screen with their ids, titles and app ids.", s.windows)
	tool(s, srv, "screenshot", "Capture the screen. Returns the image plus its geometry; coordinates for other tools are screen pixels.", s.screenshot)
	tool(s, srv, "click", "Click at screen coordinates (left by default; count=2 for a double click).", s.click)
	tool(s, srv, "double_click", "Double-click at screen coordinates with the left button.", s.doubleClick)
	tool(s, srv, "move", "Move the pointer to screen coordinates without clicking (hover).", s.move)
	tool(s, srv, "scroll", "Scroll the mouse wheel at screen coordinates.", s.scroll)
	tool(s, srv, "drag", "Press the left button at (x1,y1), move to (x2,y2) and release.", s.drag)
	tool(s, srv, "type", "Type Unicode text with the virtual keyboard (accents and emoji included).", s.typeText)
	tool(s, srv, "key", "Press key combinations such as ctrl+l, Return, alt+F4, ctrl+shift+t.", s.key)
	tool(s, srv, "wait", "Wait for a delay, for the image to stop changing (stable_ms) or for a window title (regexp).", s.wait)
	tool(s, srv, "batch", "Run several actions in one call; stops at the first error.", s.batch)
	s.registerFork(srv)
	s.registerPerceive(srv)
}

// --- handlers ---------------------------------------------------------------

func (s *Server) screenCreate(in createIn) (*mcp.CallToolResult, error) {
	if desktop.IsDesktop(in.Name) {
		return nil, screen.Errf(screen.CodeInvalidName, "choose another name", "%q is reserved for the human's desktop", in.Name)
	}
	c, err := s.hypr()
	if err != nil {
		return nil, err
	}
	var w, h int
	if in.Size != "" {
		if _, err := fmt.Sscanf(in.Size, "%dx%d", &w, &h); err != nil || w <= 0 || h <= 0 {
			return nil, fmt.Errorf("size must be WxH, got %q", in.Size)
		}
	}
	rec, err := screen.Create(c, screen.CreateOptions{Name: in.Name, Width: w, Height: h, Mirror: in.Mirror, Owner: session.Current().Owner()})
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"screen": rec.Name, "width": rec.Width, "height": rec.Height,
		"workspace_mirror": rec.WorkspaceMirror, "renderer": rec.Renderer,
		"note": "coordinates are screen pixels; call screen_destroy when done",
	}
	if rec.MirrorNote != "" {
		out["mirror_note"] = rec.MirrorNote
	}
	return textResult(out), nil
}

func (s *Server) setup(in struct{}) (*mcp.CallToolResult, error) {
	rep, err := setup.Run()
	err = errors.Join(err, setup.Notifyd())
	if err != nil {
		rep.Manual = err.Error()
		data, _ := json.MarshalIndent(rep, "", "  ")
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
	}
	return textResult(rep), nil
}

func (s *Server) screenDestroy(in screenIn) (*mcp.CallToolResult, error) {
	rec, _, err := s.resolve(in.Screen, false)
	if err != nil {
		return nil, err
	}
	screen.CloseConn(rec.Name)
	s.dropTable(rec)
	s.dropDevtools(rec)
	if err := screen.Destroy(s.ctx, rec); err != nil {
		return nil, err
	}
	return textResult(map[string]string{"destroyed": rec.Name}), nil
}

func (s *Server) screenList(in listIn) (*mcp.CallToolResult, error) {
	recs, err := registry.List()
	if err != nil {
		return nil, err
	}
	id := session.Current()
	type row struct {
		Name    string `json:"screen"`
		Width   int    `json:"width"`
		Height  int    `json:"height"`
		State   string `json:"state"`
		Mine    bool   `json:"mine"`
		Alive   bool   `json:"alive"`
		Mirror  int    `json:"workspace_mirror"`
		Created string `json:"created_at"`
	}
	rows := []row{}
	for _, r := range recs {
		mine := screen.CheckOwner(r, id) == nil
		if !in.All && !mine {
			continue
		}
		hb, _ := registry.Heartbeat(r.Name)
		rows = append(rows, row{r.Name, r.Width, r.Height, r.State, mine, session.IsAlive(r.Owner, hb, s.cfg.SessionGrace), r.WorkspaceMirror, r.CreatedAt.Format(time.RFC3339)})
	}
	return textResult(rows), nil
}

func (s *Server) appLaunch(in launchIn) (*mcp.CallToolResult, error) {
	if len(in.Command) == 0 {
		return nil, fmt.Errorf("command must not be empty")
	}
	if desktop.IsDesktop(in.Screen) {
		return s.desktopLaunch(in)
	}
	rec, _, err := s.resolve(in.Screen, false)
	if err != nil {
		return nil, err
	}
	wait := int(s.cfg.WindowTimeout / time.Millisecond)
	if in.WaitWindowMs != nil {
		wait = *in.WaitWindowMs
	}
	var before map[uint32]bool
	cl, clErr := screen.Open(rec)
	if clErr == nil {
		before = map[uint32]bool{}
		if tls, err := cl.Toplevels(); err == nil {
			for _, t := range tls {
				before[t.ID] = true
			}
		}
	}
	command, port := in.Command, 0
	if in.Debug {
		if command, port, err = screen.PrepareDebug(rec, command); err != nil {
			return nil, err
		}
	}
	pid, logPath, err := screen.Launch(s.ctx, rec, command, in.Cwd, in.Env)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"pid": pid, "screen": rec.Name, "log": logPath}
	if in.Debug {
		if err := screen.ConfirmDebug(rec, port, 10*time.Second); err != nil {
			// The app keeps running: the agent can still drive it.
			if se := (*screen.Error)(nil); errors.As(err, &se) {
				se.Msg = fmt.Sprintf("%s; the app runs as pid %d, log %s", se.Msg, pid, logPath)
			}
			return nil, err
		}
		out["debug_port"] = port
		s.startDevtools(rec)
	}
	if clErr != nil || wait <= 0 {
		if clErr != nil {
			out["note"] = "launched; window tracking unavailable: " + clErr.Error()
		}
		return textResult(out), nil
	}
	deadline := time.Now().Add(time.Duration(wait) * time.Millisecond)
	for {
		tls, err := cl.Toplevels()
		if err == nil {
			for _, t := range tls {
				if !before[t.ID] {
					out["window"] = t
					return textResult(out), nil
				}
			}
		}
		if time.Now().After(deadline) {
			out["note"] = fmt.Sprintf("no new window within %d ms; the app may still be starting (use wait with title=...)", wait)
			return textResult(out), nil
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (s *Server) appClose(in closeIn) (*mcp.CallToolResult, error) {
	if err := refuseDesktop(in.Screen, "app_close has no desktop equivalent"); err != nil {
		return nil, err
	}
	_, cl, err := s.resolve(in.Screen, true)
	if err != nil {
		return nil, err
	}
	closed, err := screen.CloseWindow(cl, in.Toplevel)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"closed": closed}
	if closed != in.Toplevel {
		out["note"] = "the window had remapped and carried a new id, the only window of the screen was closed"
	}
	return textResult(out), nil
}

func (s *Server) mirror(in mirrorIn) (*mcp.CallToolResult, error) {
	if err := refuseDesktop(in.Screen, "mirror has no desktop equivalent"); err != nil {
		return nil, err
	}
	rec, _, err := s.resolve(in.Screen, false)
	if err != nil {
		return nil, err
	}
	if in.Open != nil && !*in.Open {
		screen.StopMirror(rec.Name)
		return textResult(map[string]any{"screen": rec.Name, "mirror": "closed"}), nil
	}
	c, err := s.hypr()
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if err := screen.StartMirror(c, rec, exe, sysd.Available()); err != nil {
		return nil, err
	}
	out := map[string]any{"screen": rec.Name, "mirror": "open"}
	if st, alive := screen.ReadMirror(rec.Name); alive {
		out["workspace_mirror"] = st.Workspace
	}
	return textResult(out), nil
}

func (s *Server) windows(in screenIn) (*mcp.CallToolResult, error) {
	if err := refuseDesktop(in.Screen, "use desktop_windows"); err != nil {
		return nil, err
	}
	_, cl, err := s.resolve(in.Screen, true)
	if err != nil {
		return nil, err
	}
	tls, err := cl.Toplevels()
	if err != nil {
		return nil, err
	}
	if tls == nil {
		tls = []wl.Toplevel{}
	}
	return textResult(tls), nil
}

func (s *Server) screenshot(in shotIn) (*mcp.CallToolResult, error) {
	rec, cl, err := s.resolve(in.Screen, true)
	if err != nil {
		return nil, err
	}
	cursor := true
	if in.Cursor != nil {
		cursor = *in.Cursor
	}
	if in.SettleMs > 0 {
		time.Sleep(time.Duration(min(in.SettleMs, 10000)) * time.Millisecond)
	}
	res, err := screen.Shot(cl, screen.ShotOptions{Scale: in.Scale, Region: in.Region, Format: in.Format, Cursor: cursor,
		MaxSide: s.cfg.ShotMaxSide, MaxBytes: s.cfg.ShotMaxBytes})
	if err != nil {
		return nil, err
	}
	return imageResult(res, rec), nil
}

func (s *Server) click(in clickIn) (*mcp.CallToolResult, error) {
	rec, cl, err := s.resolve(in.Screen, true)
	if err != nil {
		return nil, err
	}
	b, err := screen.ParseButton(in.Button)
	if err != nil {
		return nil, err
	}
	if err := screen.Click(cl, in.X, in.Y, b, in.Count, in.Modifiers); err != nil {
		return nil, err
	}
	return s.afterAction(rec, cl, in.ScreenshotAfter, in.SettleMs)
}

func (s *Server) doubleClick(in moveIn) (*mcp.CallToolResult, error) {
	rec, cl, err := s.resolve(in.Screen, true)
	if err != nil {
		return nil, err
	}
	if err := screen.Click(cl, in.X, in.Y, wl.ButtonLeft, 2, nil); err != nil {
		return nil, err
	}
	return s.afterAction(rec, cl, false, 0)
}

func (s *Server) move(in moveIn) (*mcp.CallToolResult, error) {
	rec, cl, err := s.resolve(in.Screen, true)
	if err != nil {
		return nil, err
	}
	if err := cl.Move(in.X, in.Y); err != nil {
		return nil, err
	}
	return s.afterAction(rec, cl, false, 0)
}

func (s *Server) scroll(in scrollIn) (*mcp.CallToolResult, error) {
	rec, cl, err := s.resolve(in.Screen, true)
	if err != nil {
		return nil, err
	}
	if err := screen.ScrollAt(cl, in.X, in.Y, in.Direction, in.Amount); err != nil {
		return nil, err
	}
	return s.afterAction(rec, cl, in.ScreenshotAfter, in.SettleMs)
}

func (s *Server) drag(in dragIn) (*mcp.CallToolResult, error) {
	rec, cl, err := s.resolve(in.Screen, true)
	if err != nil {
		return nil, err
	}
	if err := screen.Drag(cl, in.X1, in.Y1, in.X2, in.Y2, time.Duration(in.DurationMs)*time.Millisecond); err != nil {
		return nil, err
	}
	return s.afterAction(rec, cl, in.ScreenshotAfter, in.SettleMs)
}

func (s *Server) typeText(in typeIn) (*mcp.CallToolResult, error) {
	if err := refuseDesktop(in.Screen, "use desktop_type"); err != nil {
		return nil, err
	}
	rec, cl, err := s.resolve(in.Screen, true)
	if err != nil {
		return nil, err
	}
	if err := cl.Type(in.Text); err != nil {
		return nil, err
	}
	return s.afterAction(rec, cl, in.ScreenshotAfter, in.SettleMs)
}

func (s *Server) key(in keyIn) (*mcp.CallToolResult, error) {
	if err := refuseDesktop(in.Screen, "use desktop_key"); err != nil {
		return nil, err
	}
	rec, cl, err := s.resolve(in.Screen, true)
	if err != nil {
		return nil, err
	}
	if err := screen.Keys(cl, in.Keys); err != nil {
		return nil, err
	}
	return s.afterAction(rec, cl, in.ScreenshotAfter, in.SettleMs)
}

func (s *Server) wait(in waitIn) (*mcp.CallToolResult, error) {
	if err := refuseDesktop(in.Screen, "wait has no desktop equivalent"); err != nil {
		return nil, err
	}
	if in.Ms > 0 {
		time.Sleep(time.Duration(in.Ms) * time.Millisecond)
	}
	if in.StableMs <= 0 && in.Title == "" {
		return textResult(map[string]string{"status": "ok"}), nil
	}
	_, cl, err := s.resolve(in.Screen, true)
	if err != nil {
		return nil, err
	}
	return s.doWait(cl, in.StableMs, in.Title, in.TimeoutMs)
}

func (s *Server) doWait(cl *wl.Client, stableMs int, title string, timeoutMs int) (*mcp.CallToolResult, error) {
	timeout := 5 * time.Second
	if timeoutMs > 0 {
		timeout = time.Duration(timeoutMs) * time.Millisecond
	}
	out := map[string]any{"status": "ok"}
	if stableMs > 0 {
		if err := screen.WaitStable(cl, time.Duration(stableMs)*time.Millisecond, timeout, s.cfg.StableThreshold); err != nil {
			return nil, err
		}
		out["stable"] = true
	}
	if title != "" {
		re, err := regexp.Compile(title)
		if err != nil {
			return nil, fmt.Errorf("title: %v", err)
		}
		t, err := screen.WaitTitle(cl, re, timeout)
		if err != nil {
			return nil, err
		}
		out["window"] = t
	}
	return textResult(out), nil
}

func (s *Server) batch(in batchIn) (*mcp.CallToolResult, error) {
	if err := refuseDesktop(in.Screen, "call desktop_type and desktop_key one by one"); err != nil {
		return nil, err
	}
	rec, cl, err := s.resolve(in.Screen, true)
	if err != nil {
		return nil, err
	}
	results := make([]batchResult, len(in.Actions))
	failed := false
	for i, a := range in.Actions {
		results[i].Op = a.Op
		if failed {
			results[i].Status = "skipped"
			continue
		}
		if err := s.runAction(cl, a); err != nil {
			results[i].Status, results[i].Error = "error", err.Error()
			failed = true
			continue
		}
		results[i].Status = "ok"
	}
	if failed {
		data, _ := json.MarshalIndent(results, "", "  ")
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
	}
	if !in.ScreenshotAfter {
		return textResult(results), nil
	}
	res, err := s.afterAction(rec, cl, true, in.SettleMs)
	if err != nil {
		return nil, err
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	res.Content = append(res.Content, &mcp.TextContent{Text: string(data)})
	return res, nil
}

func (s *Server) runAction(cl *wl.Client, a batchAction) error {
	switch strings.ToLower(a.Op) {
	case "click":
		b, err := screen.ParseButton(a.Button)
		if err != nil {
			return err
		}
		return screen.Click(cl, a.X, a.Y, b, a.Count, a.Modifiers)
	case "double_click":
		return screen.Click(cl, a.X, a.Y, wl.ButtonLeft, 2, nil)
	case "move":
		return cl.Move(a.X, a.Y)
	case "scroll":
		return screen.ScrollAt(cl, a.X, a.Y, a.Direction, a.Amount)
	case "drag":
		return screen.Drag(cl, a.X, a.Y, a.X2, a.Y2, 0)
	case "type":
		return cl.Type(a.Text)
	case "key":
		return screen.Keys(cl, a.Keys)
	case "wait":
		if a.Ms > 0 {
			time.Sleep(time.Duration(a.Ms) * time.Millisecond)
		}
		if a.StableMs > 0 || a.Title != "" {
			_, err := s.doWait(cl, a.StableMs, a.Title, a.TimeoutMs)
			return err
		}
		return nil
	}
	return fmt.Errorf("unknown op %q (click, double_click, move, scroll, drag, type, key, wait)", a.Op)
}
