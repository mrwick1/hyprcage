package cli

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/screen"
	"github.com/hexadecimil/offstage/internal/session"
	"github.com/hexadecimil/offstage/internal/wl"
)

// openScreen resolves a screen (empty name = the session's only one) and
// opens its Wayland connection.
func openScreen(name string) (*registry.Screen, *wl.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	c, err := screen.Connect(cfg)
	if err != nil {
		return nil, nil, err
	}
	rec, err := screen.Resolve(c, name, session.Current())
	if err != nil {
		return nil, nil, err
	}
	cl, err := screen.Open(rec)
	if err != nil {
		return nil, nil, err
	}
	return rec, cl, nil
}

func atoi(e *Env, what, s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil {
		e.errorf("%s: expected an integer, got %q", what, s)
		return 0, false
	}
	return n, true
}

func runShot(e *Env) int {
	fs := e.flags("shot")
	out := fs.String("o", "", "output file (default: stdout)")
	scale := fs.Float64("scale", 1, "0.25..1")
	region := fs.String("region", "", "x,y,w,h in screen pixels")
	format := fs.String("format", "png", "png or jpeg")
	noCursor := fs.Bool("no-cursor", false, "hide cage's cursor")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 1 {
		return e.errorf("usage: offstage shot [screen] [-o file] [--scale S] [--region x,y,w,h] [--format png|jpeg] [--no-cursor]")
	}
	opts := screen.ShotOptions{Scale: *scale, Format: *format, Cursor: !*noCursor}
	if *region != "" {
		var r screen.Rect
		if _, err := fmt.Sscanf(*region, "%d,%d,%d,%d", &r.X, &r.Y, &r.W, &r.H); err != nil {
			return e.errorf("--region expects x,y,w,h")
		}
		opts.Region = &r
	}
	_, cl, err := openScreen(fs.Arg(0))
	if err != nil {
		return e.fail(err)
	}
	res, err := screen.Shot(cl, opts)
	if err != nil {
		return e.fail(err)
	}
	if *out == "" {
		if _, err := e.Stdout.Write(res.Data); err != nil {
			return e.fail(err)
		}
		return ExitOK
	}
	if err := os.WriteFile(*out, res.Data, 0o644); err != nil {
		return e.fail(err)
	}
	fmt.Fprintf(e.Stdout, "%s %dx%d scale=%g (screen %dx%d)\n", *out, res.Width, res.Height, res.Scale, res.ScreenW, res.ScreenH)
	return ExitOK
}

func runClick(e *Env) int {
	fs := e.flags("click")
	button := fs.String("button", "left", "left, right or middle")
	count := fs.Int("count", 1, "1 = click, 2 = double click")
	mods := fs.String("mod", "", "modifiers held during the click, e.g. ctrl,shift")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 3 {
		return e.errorf("usage: offstage click <screen> <x> <y> [--button B] [--count N] [--mod ctrl,shift]")
	}
	x, ok := atoi(e, "x", fs.Arg(1))
	if !ok {
		return ExitUsage
	}
	y, ok := atoi(e, "y", fs.Arg(2))
	if !ok {
		return ExitUsage
	}
	b, err := screen.ParseButton(*button)
	if err != nil {
		return e.errorf("%v", err)
	}
	var modifiers []string
	if *mods != "" {
		modifiers = strings.Split(*mods, ",")
	}
	_, cl, err := openScreen(fs.Arg(0))
	if err != nil {
		return e.fail(err)
	}
	if err := screen.Click(cl, x, y, b, *count, modifiers); err != nil {
		return e.fail(err)
	}
	return ExitOK
}

func runMove(e *Env) int {
	fs := e.flags("move")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 3 {
		return e.errorf("usage: offstage move <screen> <x> <y>")
	}
	x, ok := atoi(e, "x", fs.Arg(1))
	if !ok {
		return ExitUsage
	}
	y, ok := atoi(e, "y", fs.Arg(2))
	if !ok {
		return ExitUsage
	}
	_, cl, err := openScreen(fs.Arg(0))
	if err != nil {
		return e.fail(err)
	}
	if err := cl.Move(x, y); err != nil {
		return e.fail(err)
	}
	return ExitOK
}

func runScroll(e *Env) int {
	fs := e.flags("scroll")
	amount := fs.Int("amount", 3, "wheel clicks")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 4 {
		return e.errorf("usage: offstage scroll <screen> <x> <y> <up|down|left|right> [--amount N]")
	}
	x, ok := atoi(e, "x", fs.Arg(1))
	if !ok {
		return ExitUsage
	}
	y, ok := atoi(e, "y", fs.Arg(2))
	if !ok {
		return ExitUsage
	}
	_, cl, err := openScreen(fs.Arg(0))
	if err != nil {
		return e.fail(err)
	}
	if err := screen.ScrollAt(cl, x, y, fs.Arg(3), *amount); err != nil {
		return e.fail(err)
	}
	return ExitOK
}

func runDrag(e *Env) int {
	fs := e.flags("drag")
	duration := fs.Duration("duration", 250*time.Millisecond, "time between press and release")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 5 {
		return e.errorf("usage: offstage drag <screen> <x1> <y1> <x2> <y2> [--duration 250ms]")
	}
	var v [4]int
	for i := range v {
		n, ok := atoi(e, "coordinate", fs.Arg(i+1))
		if !ok {
			return ExitUsage
		}
		v[i] = n
	}
	_, cl, err := openScreen(fs.Arg(0))
	if err != nil {
		return e.fail(err)
	}
	if err := screen.Drag(cl, v[0], v[1], v[2], v[3], *duration); err != nil {
		return e.fail(err)
	}
	return ExitOK
}

func runType(e *Env) int {
	fs := e.flags("type")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 2 {
		return e.errorf("usage: offstage type <screen> <text>")
	}
	_, cl, err := openScreen(fs.Arg(0))
	if err != nil {
		return e.fail(err)
	}
	if err := cl.Type(fs.Arg(1)); err != nil {
		return e.fail(err)
	}
	return ExitOK
}

func runKey(e *Env) int {
	fs := e.flags("key")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if fs.NArg() < 2 {
		return e.errorf("usage: offstage key <screen> <combo> [<combo>...]   (e.g. ctrl+l Return)")
	}
	_, cl, err := openScreen(fs.Arg(0))
	if err != nil {
		return e.fail(err)
	}
	if err := screen.Keys(cl, fs.Args()[1:]); err != nil {
		return e.fail(err)
	}
	return ExitOK
}

func runWindows(e *Env) int {
	fs := e.flags("windows")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 1 {
		return e.errorf("usage: offstage windows [screen] [--json]")
	}
	_, cl, err := openScreen(fs.Arg(0))
	if err != nil {
		return e.fail(err)
	}
	tls, err := cl.Toplevels()
	if err != nil {
		return e.fail(err)
	}
	if *asJSON {
		return e.printJSON(tls)
	}
	if len(tls) == 0 {
		fmt.Fprintln(e.Stdout, "no window")
		return ExitOK
	}
	for _, t := range tls {
		var st []string
		if t.Activated {
			st = append(st, "active")
		}
		if t.Fullscreen {
			st = append(st, "fullscreen")
		}
		if t.Maximized {
			st = append(st, "maximized")
		}
		if t.Minimized {
			st = append(st, "minimized")
		}
		fmt.Fprintf(e.Stdout, "%d\t%s\t%q\t%s\n", t.ID, t.AppID, t.Title, strings.Join(st, ","))
	}
	return ExitOK
}

func runClose(e *Env) int {
	fs := e.flags("close")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 2 {
		return e.errorf("usage: offstage close <screen> <window-id>")
	}
	id, ok := atoi(e, "window id", fs.Arg(1))
	if !ok {
		return ExitUsage
	}
	_, cl, err := openScreen(fs.Arg(0))
	if err != nil {
		return e.fail(err)
	}
	if _, err := screen.CloseWindow(cl, uint32(id)); err != nil {
		return e.fail(err)
	}
	return ExitOK
}

func runWait(e *Env) int {
	fs := e.flags("wait")
	ms := fs.Int("ms", 0, "plain delay in milliseconds")
	stable := fs.Duration("stable", 0, "wait until the image is stable for this long, e.g. 300ms")
	title := fs.String("title", "", "wait for a window title matching this regexp")
	timeout := fs.Duration("timeout", 5*time.Second, "give up after this long")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 1 {
		return e.errorf("usage: offstage wait [screen] [--ms N] [--stable D] [--title RE] [--timeout D]")
	}
	if *ms > 0 {
		time.Sleep(time.Duration(*ms) * time.Millisecond)
	}
	if *stable == 0 && *title == "" {
		return ExitOK
	}
	_, cl, err := openScreen(fs.Arg(0))
	if err != nil {
		return e.fail(err)
	}
	if *stable > 0 {
		cfg, err := config.Load()
		if err != nil {
			return e.fail(err)
		}
		if err := screen.WaitStable(cl, *stable, *timeout, cfg.StableThreshold); err != nil {
			return e.fail(err)
		}
	}
	if *title != "" {
		re, err := regexp.Compile(*title)
		if err != nil {
			return e.errorf("--title: %v", err)
		}
		t, err := screen.WaitTitle(cl, re, *timeout)
		if err != nil {
			return e.fail(err)
		}
		fmt.Fprintf(e.Stdout, "%d\t%s\t%q\n", t.ID, t.AppID, t.Title)
	}
	return ExitOK
}
