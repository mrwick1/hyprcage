package cli

import (
	"flag"
	"fmt"

	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/screen"
	"github.com/hexadecimil/offstage/internal/session"
)

func runCreate(e *Env) int {
	fs := e.flags("create")
	size := fs.String("size", "", "WxH (default 1280x800)")
	name := fs.String("name", "", "screen name (default hc-<random>)")
	mirror := fs.Bool("mirror", false, "open a mirror window whatever screen.mirror says in the configuration")
	noMirror := fs.Bool("no-mirror", false, "do not open a mirror window, whatever screen.mirror says")
	asJSON := fs.Bool("json", false, "print the record as JSON")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	w, h, err := parseSize(*size)
	if err != nil {
		return e.errorf("%v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return e.fail(err)
	}
	c, err := screen.Connect(cfg)
	if err != nil {
		return e.fail(err)
	}
	// Mirror stays nil unless one of the two flags was given on the command
	// line, and nil is what tells Create to follow screen.mirror.
	var wantMirror *bool
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "mirror":
			v := *mirror
			wantMirror = &v
		case "no-mirror":
			v := !*noMirror
			wantMirror = &v
		}
	})
	rec, err := screen.Create(c, screen.CreateOptions{
		Name: *name, Width: w, Height: h, Mirror: wantMirror,
		Owner: session.Current().Owner(),
	})
	if err != nil {
		return e.fail(err)
	}
	if *asJSON {
		return e.printJSON(rec)
	}
	fmt.Fprintf(e.Stdout, "name=%s\nsize=%dx%d\nworkspace_mirror=%d\ninner_display=%s\nrenderer=%s\nrender_device=%s\n",
		rec.Name, rec.Width, rec.Height, rec.WorkspaceMirror, rec.InnerDisplay, rec.Renderer, rec.RenderDevice)
	if rec.MirrorNote != "" {
		fmt.Fprintf(e.Stdout, "mirror_note=%s\n", rec.MirrorNote)
	}
	return ExitOK
}

func parseSize(s string) (int, int, error) {
	if s == "" {
		return 0, 0, nil
	}
	var w, h int
	if _, err := fmt.Sscanf(s, "%dx%d", &w, &h); err != nil || w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("invalid --size %q, expected WxH", s)
	}
	return w, h, nil
}
