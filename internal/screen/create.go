package screen

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/lock"
	"github.com/hexadecimil/hyprcage/internal/notify"
	"github.com/hexadecimil/hyprcage/internal/proc"
	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/session"
	"github.com/hexadecimil/hyprcage/internal/sysd"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// CreateOptions are the parameters of screen_create (cahier F1).
type CreateOptions struct {
	Name   string
	Width  int
	Height int
	// Mirror opens the human's mirror window. nil means the caller has no
	// opinion and screen.mirror in the configuration decides: that key is
	// the default, not a veto, so a caller may open a mirror on a machine
	// that does not ask for one and drop it on a machine that does.
	Mirror *bool
	Owner  registry.Owner
}

// RecordVersion is the format of the records this hyprcage writes.
const RecordVersion = 3

// mirrorWanted resolves the two opinions about the human's mirror window.
// The configuration carries the human's default and the caller may depart
// from it in either direction: a caller with no opinion passes nil.
func mirrorWanted(want *bool, configured bool) bool {
	if want != nil {
		return *want
	}
	return configured
}

// Create makes a screen: cage on wlroots' headless backend, with an output
// of its own that no other compositor knows about. Nothing here touches the
// human's compositor except, at the end, the launch of the mirror window.
func Create(c *Ctx, opts CreateOptions) (*registry.Screen, error) {
	cfg := c.Cfg
	if opts.Name == "" {
		opts.Name = NewName(cfg.OutputPrefix)
	} else {
		opts.Name = Prefixed(opts.Name, cfg.OutputPrefix)
	}
	if err := ValidateName(opts.Name); err != nil {
		return nil, err
	}
	if opts.Width <= 0 || opts.Height <= 0 {
		opts.Width, opts.Height = cfg.DefaultWidth, cfg.DefaultHeight
	}
	if opts.Width < 320 || opts.Height < 240 || opts.Width > cfg.MaxWidth || opts.Height > cfg.MaxHeight {
		return nil, errf(CodeLimit, "screen.max_width / screen.max_height in config.toml", "size %dx%d is outside 320x240 .. %dx%d", opts.Width, opts.Height, cfg.MaxWidth, cfg.MaxHeight)
	}
	if _, err := exec.LookPath("cage"); err != nil {
		return nil, errf(CodeCage, "run `hyprcage setup` (or the setup tool): it installs cage after the human enters their password", "cage not found in PATH")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}

	release, err := lock.Acquire(30 * time.Second)
	if err != nil {
		return nil, err
	}
	defer release()

	// Step 1: reap orphans first (M4), then the quota.
	_, _ = GC(c, GCOptions{})
	if _, err := registry.Load(opts.Name); err == nil {
		return nil, errf(CodeInvalidName, "pick another name", "screen %s already exists", opts.Name)
	}
	if cfg.MaxPerSession > 0 {
		recs, _ := registry.List()
		n := 0
		for _, r := range recs {
			if sameOwner(r.Owner, opts.Owner) {
				n++
			}
		}
		if n >= cfg.MaxPerSession {
			return nil, errf(CodeLimit, "destroy a screen you no longer need, or raise screen.max_per_session in config.toml", "this session already holds %d screen(s), its limit", n)
		}
	}

	// Step 2: the mirror's workspace, and the reason when there is none.
	wsMirror, mirrorNote := 0, ""
	switch {
	case !mirrorWanted(opts.Mirror, cfg.MirrorEnabled):
		mirrorNote = "not asked for"
	case c.Hypr == nil:
		if opts.Mirror == nil {
			// Without a compositor that places windows, a mirror would open
			// on whatever the human is looking at: only on explicit request.
			mirrorNote = "no Hyprland to place the window on, open one with `hyprcage mirror " + opts.Name + "`"
		} else {
			wsMirror = -1 // wanted, unplaced
		}
	default:
		if wsMirror = mirrorWorkspace(c, opts.Owner); wsMirror == 0 {
			mirrorNote = fmt.Sprintf("workspaces %d to %d are all taken", cfg.MirrorMin, cfg.MirrorMax)
		}
	}

	// Step 3: the GPU, then the record and the safety timer, before any
	// process exists (N2).
	device, how := RenderDevice(cfg.RenderDevice, humanDisplay(c))
	rec := &registry.Screen{
		Name: opts.Name, CreatedAt: time.Now(), State: "starting", Version: RecordVersion,
		Width: opts.Width, Height: opts.Height,
		WorkspaceMirror: max(wsMirror, 0), MirrorNote: mirrorNote,
		Slice: sysd.SliceName(opts.Name), Owner: opts.Owner, RenderDevice: device, RenderDeviceBy: how,
	}
	if err := registry.Save(rec); err != nil {
		return nil, err
	}
	useSystemd := sysd.Available()
	if useSystemd {
		if err := sysd.ArmTimer(sysd.GCTimerName(opts.Name), cfg.SafetyTimer, []string{exe, "gc"}); err != nil {
			_ = registry.Delete(opts.Name)
			return nil, fmt.Errorf("arming the safety timer: %w", err)
		}
	}
	fail := func(err error) (*registry.Screen, error) {
		_ = Destroy(c, rec)
		return nil, err
	}

	// Step 4 to 6: cage, its output at the asked size, and one capture to
	// prove the renderer works. The renderer is named explicitly: wlroots
	// left to itself falls back to software rendering without a word, and
	// the probe would then bless a screen no application can use a GPU on.
	// A GPU renderer that fails is retried in software and remembered; a
	// remembered software renderer that fails is forgotten.
	var cl *wl.Client
	candidates := rendererCandidates(cfg.Renderer)
	for i, renderer := range candidates {
		_ = os.Remove(registry.InnerPath(opts.Name))
		if err := startCage(c, rec, exe, renderer, useSystemd); err != nil {
			return fail(err)
		}
		inner, err := waitInner(opts.Name, 5*time.Second)
		if err == nil {
			rec.InnerDisplay, rec.InnerX11 = inner["WAYLAND_DISPLAY"], inner["DISPLAY"]
			rec.CagePID = atoi(inner["CAGE_PID"])
			rec.CagePIDStart, _ = session.ProcStart(rec.CagePID)
			cl, err = readyScreen(rec, opts.Width, opts.Height)
		}
		if err == nil {
			rec.Renderer = renderer
			if renderer != rendererPixman {
				rememberRenderer(renderer)
			}
			break
		}
		CloseConn(opts.Name)
		killCage(opts.Name, useSystemd)
		if i == len(candidates)-1 {
			forgetRenderer()
			return fail(err)
		}
		rememberRenderer(rendererPixman) // next candidate; skip the GPU next time
	}
	_ = cl

	// Step 7: the mirror.
	if wsMirror != 0 {
		if err := StartMirror(c, rec, exe, useSystemd); err != nil {
			rec.WorkspaceMirror, rec.MirrorNote = 0, "the mirror could not be started: "+err.Error()
		}
	}

	rec.State = "ready"
	if err := registry.Save(rec); err != nil {
		return fail(err)
	}
	if cfg.Notify {
		body := opts.Name
		if rec.WorkspaceMirror > 0 {
			body = fmt.Sprintf("%s, mirror on workspace %d", opts.Name, rec.WorkspaceMirror)
		}
		notify.Send("Agent screen opened", body)
	}
	return rec, nil
}

// startCage launches cage on the headless backend in the screen's scope,
// with an environment built rather than inherited: what wlroots reads must
// be exactly what hyprcage decided, and nothing must point cage at the
// human's compositor.
func startCage(c *Ctx, rec *registry.Screen, exe, renderer string, useSystemd bool) error {
	env := map[string]string{
		"WLR_BACKENDS":         "headless",
		"WLR_HEADLESS_OUTPUTS": "1",
		"WLR_RENDERER":         renderer,
		"HYPRCAGE_SCREEN":      rec.Name,
	}
	if rec.RenderDevice != "" {
		env["WLR_RENDER_DRM_DEVICE"] = rec.RenderDevice
	}
	drop := func(k string) bool {
		switch k {
		case "WAYLAND_DISPLAY", "WAYLAND_SOCKET", "DISPLAY", "HYPRLAND_INSTANCE_SIGNATURE":
			return true
		}
		return strings.HasPrefix(k, "WLR_")
	}
	var full []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if drop(k) {
			continue
		}
		if _, ok := env[k]; ok {
			continue
		}
		full = append(full, kv)
	}
	for k, v := range env {
		full = append(full, k+"="+v)
	}
	argv := []string{"cage", "-d", "--", exe, "_holder", rec.Name}
	if useSystemd {
		argv = sysd.ScopeArgs(rec.Name, "cage", argv)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = full
	proc.Detach(cmd)
	if f := cageLog(rec.Name); f != nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer f.Close()
	}
	if err := cmd.Start(); err != nil {
		return errf(CodeCage, "", "starting cage: %v", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// readyScreen connects to the new cage, gives its output the asked size and
// checks that a capture completes. It returns the connection, cached for
// the other operations.
func readyScreen(rec *registry.Screen, width, height int) (*wl.Client, error) {
	cl, err := Open(rec)
	if err != nil {
		return nil, err
	}
	if err := cl.SetMode(width, height, 60, 1); err != nil {
		return nil, errf(CodeCage, "see "+CageLogPath(rec.Name), "cage did not take the size %dx%d: %v", width, height, err)
	}
	if err := probeCapture(cl); err != nil {
		return nil, err
	}
	return cl, nil
}

// captureProbe is how long the probe waits. It is generous because the
// deadline is shared with the machine: a loaded CPU can delay a capture that
// works perfectly well, and calling that a broken renderer would send every
// later screen to the slow one.
const captureProbe = 15 * time.Second

// probeCapture checks that screencopy actually completes on a screen, with a
// deadline: it hangs under virgl's GLES path, so a timeout here means the
// renderer must change.
func probeCapture(cl *wl.Client) error {
	done := make(chan error, 1)
	go func() {
		_, e := cl.Capture(false)
		done <- e
	}()
	select {
	case e := <-done:
		return e
	case <-time.After(captureProbe):
		return errf(CodeCapture, "the renderer in use cannot be captured, or the machine is too busy to answer in time", "screencopy did not complete within %s", captureProbe)
	}
}

// humanDisplay is the Wayland socket of the human's compositor: the
// environment's, or the Hyprland instance's when hyprcage runs from a
// place that has no WAYLAND_DISPLAY (a service, an ssh session).
func humanDisplay(c *Ctx) string {
	if d := os.Getenv("WAYLAND_DISPLAY"); d != "" {
		return d
	}
	if c.Hypr != nil {
		if d, err := c.Hypr.WaylandDisplay(); err == nil {
			return d
		}
	}
	return ""
}

func killCage(name string, useSystemd bool) {
	if useSystemd {
		_ = sysd.StopUnit(sysd.UnitName(name, "cage") + ".scope")
	}
	killScreenProcesses(name, syscall.SIGTERM)
	time.Sleep(300 * time.Millisecond)
}

// slot is what a screen's record says about its mirror: the workspace it
// was given, whose it is, and whether the screen still runs.
type slot struct {
	ws    int
	owner registry.Owner
	alive bool
}

// mirrorWorkspace picks the workspace for a screen's mirror window, 0 when
// none is left. Hyprland's workspaces are the ones holding windows; the
// records add the ones already given to a screen, because a mirror that
// was just assigned a workspace has not mapped its window yet, so Hyprland
// does not know about it and two screens would otherwise land on the same
// one.
func mirrorWorkspace(c *Ctx, owner registry.Owner) int {
	var occupied []int
	if wss, err := c.Hypr.Workspaces(); err == nil {
		for _, w := range wss {
			occupied = append(occupied, w.ID)
		}
	}
	var slots []slot
	if recs, err := registry.List(); err == nil {
		for _, r := range recs {
			if r.WorkspaceMirror > 0 {
				slots = append(slots, slot{r.WorkspaceMirror, r.Owner, Alive(r)})
			}
		}
	}
	return pickWorkspace(c.Cfg, owner, occupied, slots)
}

// pickWorkspace applies mirror.group. A workspace that already holds
// mirrors is joined when the group allows it, "session" for mirrors of the
// same session and "pack" for anyone's, and only while it holds fewer than
// mirror.per_workspace live ones; the lowest such workspace wins, so that
// the mirrors gather rather than spread. Failing that, the first workspace
// of the range that holds nothing at all. "screen" never joins.
func pickWorkspace(cfg config.Config, owner registry.Owner, occupied []int, slots []slot) int {
	used := map[int]bool{}
	for _, ws := range occupied {
		used[ws] = true
	}
	held, joinable := map[int]int{}, map[int]bool{}
	for _, s := range slots {
		used[s.ws] = true
		if !s.alive {
			continue
		}
		held[s.ws]++
		switch cfg.MirrorGroup {
		case "session":
			if sameOwner(s.owner, owner) {
				joinable[s.ws] = true
			}
		case "pack":
			joinable[s.ws] = true
		}
	}
	for ws := cfg.MirrorMin; ws <= cfg.MirrorMax; ws++ {
		if joinable[ws] && held[ws] < cfg.MirrorPerWorkspace {
			return ws
		}
	}
	return freeWorkspace(used, cfg.MirrorMin, cfg.MirrorMax)
}

func freeWorkspace(used map[int]bool, min, max int) int {
	for i := min; i <= max; i++ {
		if !used[i] {
			return i
		}
	}
	return 0
}

// waitInner waits for _holder to publish cage's sockets.
func waitInner(name string, timeout time.Duration) (map[string]string, error) {
	deadline := time.Now().Add(timeout)
	for {
		m, err := registry.ReadInner(name)
		if err == nil && m["WAYLAND_DISPLAY"] != "" {
			return m, nil
		}
		if time.Now().After(deadline) {
			return nil, errf(CodeCage, "see "+CageLogPath(name), "cage did not publish its socket within %s", timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func atoi(s string) int {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}
