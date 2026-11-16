package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/hypr"
	"github.com/hexadecimil/offstage/internal/perceive"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/screen"
	"github.com/hexadecimil/offstage/internal/setup"
	"github.com/hexadecimil/offstage/internal/sysd"
	"github.com/hexadecimil/offstage/internal/version"
)

type check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok, warn, fail
	Detail string `json:"detail"`
}

// runDoctor implements cahier §7 (partially: the --full self-test that
// creates a temporary screen waits for the wl package).
func runDoctor(e *Env) int {
	fs := e.flags("doctor")
	asJSON := fs.Bool("json", false, "JSON output")
	full := fs.Bool("full", false, "also create a temporary screen and probe cage's protocols (not implemented yet)")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	_ = full
	var checks []check
	add := func(name, status, detail string) { checks = append(checks, check{name, status, detail}) }
	if exe, err := os.Executable(); err == nil {
		add("offstage", "ok", version.Version+" ("+exe+")")
	}
	cfg, err := config.Load()
	switch {
	case err != nil:
		add("config", "fail", err.Error())
		cfg = config.Default()
	case cfg.Loaded && runtime.GOOS != "linux":
		add("config", "ok", fmt.Sprintf("%s (screens %dx%d, %d screens per session)", cfg.Path, cfg.DefaultWidth, cfg.DefaultHeight, cfg.MaxPerSession))
	case cfg.Loaded:
		add("config", "ok", fmt.Sprintf("%s (screens %dx%d, mirrors on workspaces %d-%d by %s, %d screens per session)", cfg.Path, cfg.DefaultWidth, cfg.DefaultHeight, cfg.MirrorMin, cfg.MirrorMax, cfg.MirrorGroup, cfg.MaxPerSession))
	default:
		add("config", "ok", fmt.Sprintf("defaults (no %s, `offstage config` writes one)", cfg.Path))
	}
	exe, _ := os.Executable()
	bst, bdetail := bareCheck(filepath.Dir(exe))
	add("bare", bst, bdetail)
	if runtime.GOOS != "linux" {
		return printChecks(e, checks, *asJSON)
	}

	if rt := os.Getenv("XDG_RUNTIME_DIR"); rt == "" {
		add("XDG_RUNTIME_DIR", "fail", "unset; the registry and Hyprland's sockets need it")
	} else {
		add("XDG_RUNTIME_DIR", "ok", rt)
	}

	inst, err := hypr.Discover()
	if err != nil {
		add("hyprland", "fail", err.Error())
	} else {
		if v, verr := inst.Version(); verr != nil {
			add("hyprland", "warn", "reachable, version unknown: "+verr.Error())
		} else {
			add("hyprland", "ok", v+" (instance "+inst.Signature+")")
		}
		add("config driver", "ok", inst.Driver().Mode()+" (override with OFFSTAGE_DRIVER=lua|classic)")
		if r := screen.KnownRenderer(cfg.Renderer); r != "" {
			add("cage renderer", "ok", r+" (learnt by a previous screen, forget it with rm "+screen.RendererStatePath()+")")
		} else {
			add("cage renderer", "ok", "default (GLES), pixman tried automatically if no window appears")
		}

		recs, _ := registry.List()
		known := map[string]bool{}
		for _, r := range recs {
			known[r.Name] = true
		}
		mons, _ := inst.Monitors()
		var orphans []string
		for _, m := range mons {
			if strings.HasPrefix(m.Name, cfg.OutputPrefix) && !known[m.Name] {
				orphans = append(orphans, m.Name)
			}
		}
		if len(orphans) > 0 {
			add("orphan outputs", "warn", strings.Join(orphans, ", ")+" (run `offstage gc`)")
		} else {
			add("orphan outputs", "ok", "none")
		}

		cls, _ := inst.Clients()
		busy := map[int]int{}
		for _, cl := range cls {
			if cl.Workspace.ID >= cfg.MirrorMin && cl.Workspace.ID <= cfg.MirrorMax {
				busy[cl.Workspace.ID]++
			}
		}
		if len(busy) > 0 {
			add("mirror workspaces", "warn", fmt.Sprintf("windows present on %v, mirrors take the first free one in [%d, %d]", keys(busy), cfg.MirrorMin, cfg.MirrorMax))
		} else {
			add("mirror workspaces", "ok", fmt.Sprintf("[%d, %d] free", cfg.MirrorMin, cfg.MirrorMax))
		}
	}

	tool := func(bin, level, why string) {
		if p, err := exec.LookPath(bin); err != nil {
			add(bin, level, "not found: "+why)
		} else {
			add(bin, "ok", p)
		}
	}
	tool("cage", "fail", "the agent's compositor; `offstage setup` installs it")
	tool("ffmpeg", "warn", "recording needs it; `offstage setup` installs it")
	tool("wl-copy", "warn", "the screen clipboard needs wl-clipboard; `offstage setup` installs it")
	if err := perceive.A11yBusOK(context.Background()); err != nil {
		add("a11y bus", "warn", "unreachable, snapshot cannot read GTK/Qt apps through AT-SPI: "+err.Error())
	} else {
		add("a11y bus", "ok", "org.a11y.Bus answers GetAddress")
	}
	recs, _ := registry.List()
	for _, r := range recs {
		if r.DebugPort > 0 {
			st, d := cdpCheck(r)
			add("cdp "+r.Name, st, d)
		}
	}
	tool("tesseract", "warn", "the OCR snapshot source needs it; `offstage setup` installs it")
	if _, err := os.Stat(perceive.OCRData); err != nil {
		add("ocr data", "warn", "no "+perceive.OCRData+"; run offstage setup")
	} else {
		add("ocr data", "ok", perceive.OCRData)
	}

	if len(setup.Missing()) > 0 {
		if _, err := exec.LookPath("pkexec"); err == nil {
			add("setup", "ok", "pkexec available: `offstage setup` will ask for your password in a dialog")
		} else {
			add("setup", "warn", "no pkexec: run `offstage setup` from a terminal, or "+setup.ManualCommand(setup.Missing()))
		}
	}
	tool("notify-send", "warn", "optional, desktop notifications")
	if sysd.Available() {
		add("systemd --user", "ok", "transient slices available")
	} else {
		add("systemd --user", "warn", "unreachable; falling back to process groups (less airtight against Chromium)")
	}
	st, detail := notifydCheck()
	add("notifyd", st, detail)

	return printChecks(e, checks, *asJSON)
}

// bareCheck reports whether bare-mcp and bare-drive, the e2e browser that the
// installer ships, are in dir (the directory of offstage) or in PATH. A
// missing one fails the check: Bare is required.
func bareCheck(dir string) (string, string) {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	var missing []string
	var mcp string
	for _, name := range []string{"bare-mcp", "bare-drive"} {
		p := filepath.Join(dir, name+ext)
		if _, err := os.Stat(p); err != nil {
			if p, err = exec.LookPath(name); err != nil {
				missing = append(missing, name)
				continue
			}
		}
		if name == "bare-mcp" {
			mcp = p
		}
	}
	if len(missing) > 0 {
		return "fail", strings.Join(missing, " and ") + " not found; the e2e browser needs both: rerun the installer"
	}
	return "ok", mcp
}

// printChecks prints the checks and returns ExitDependency when one failed.
func printChecks(e *Env, checks []check, asJSON bool) int {
	if asJSON {
		return e.printJSON(checks)
	}
	worst := ExitOK
	for _, c := range checks {
		fmt.Fprintf(e.Stdout, "%-18s %-4s %s\n", c.Name, c.Status, c.Detail)
		if c.Status == "fail" {
			worst = ExitDependency
		}
	}
	return worst
}

// cdpCheck reports whether the DevTools port of rec answers and whether
// the process recorded as its owner still listens on it.
func cdpCheck(rec *registry.Screen) (string, string) {
	if err := screen.WaitDebug(rec.DebugPort, 300*time.Millisecond); err != nil {
		return "warn", fmt.Sprintf("no answer on 127.0.0.1:%d; snapshot falls back to AT-SPI or OCR", rec.DebugPort)
	}
	if !screen.OwnsPort(rec) {
		return "warn", fmt.Sprintf("127.0.0.1:%d answers but its recorded owner no longer holds it; relaunch the app with debug=true", rec.DebugPort)
	}
	return "ok", fmt.Sprintf("127.0.0.1:%d answers and its recorded owner holds it", rec.DebugPort)
}

// notifydCheck reports the state of the notification daemon's unit and,
// when it runs, its resident memory.
func notifydCheck() (string, string) {
	out, _ := exec.Command("systemctl", "--user", "is-active", setup.NotifydUnit).Output()
	state := strings.TrimSpace(string(out))
	if state != "active" {
		return "warn", setup.NotifydUnit + " " + state + "; notify_list and notify_wait need it: `offstage setup`, or systemctl --user start " + setup.NotifydUnit
	}
	pid, _ := exec.Command("systemctl", "--user", "show", "-p", "MainPID", "--value", setup.NotifydUnit).Output()
	status, _ := os.ReadFile("/proc/" + strings.TrimSpace(string(pid)) + "/status")
	for _, l := range strings.Split(string(status), "\n") {
		if rss, ok := strings.CutPrefix(l, "VmRSS:"); ok {
			return "ok", setup.NotifydUnit + " active, RSS " + strings.TrimSpace(rss)
		}
	}
	return "ok", setup.NotifydUnit + " active, RSS unknown"
}

func keys(m map[int]int) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}
