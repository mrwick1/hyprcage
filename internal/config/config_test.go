package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadFileMissingGivesDefaults(t *testing.T) {
	cfg, err := LoadFile(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil || cfg.Loaded || cfg != func() Config { d := Default(); d.Path = cfg.Path; return d }() {
		t.Fatalf("missing file: err=%v loaded=%v", err, cfg.Loaded)
	}
}

func TestLoadFileOverrides(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(`
[screen]
width = 1024
height = 640
max_width = 2560
max_height = 1440
max_per_session = 2
mirror = false
notify = false

[workspaces]
mirror = [7, 8]

[mirror]
group = "Screen"
per_workspace = 2

[lifecycle]
safety_timer = "30m"

[cage]
renderer = "Pixman"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Loaded || cfg.Path != p {
		t.Errorf("loaded=%v path=%s", cfg.Loaded, cfg.Path)
	}
	want := Default()
	want.Path = p
	want.Loaded = true
	want.DefaultWidth, want.DefaultHeight = 1024, 640
	want.MaxWidth, want.MaxHeight = 2560, 1440
	want.MaxPerSession = 2
	want.MirrorEnabled, want.Notify = false, false
	want.MirrorMin, want.MirrorMax = 7, 8
	want.SafetyTimer = 30 * time.Minute
	want.Renderer = "pixman"
	want.MirrorGroup = "screen"
	want.MirrorPerWorkspace = 2
	if cfg != want {
		t.Errorf("got  %+v\nwant %+v", cfg, want)
	}
}

func TestApplyRejects(t *testing.T) {
	cases := map[string]string{
		`[screen]` + "\n" + `widht = 10`:               "unknown key",
		`[screen]` + "\n" + `width = 100`:              "below 320x240",
		`[screen]` + "\n" + `max_width = 800`:          "below the default size",
		`[screen]` + "\n" + `max_per_session = -1`:     "negative",
		`[workspaces]` + "\n" + `agent = [11, 99]`:     "unknown key",
		`[workspaces]` + "\n" + `mirror = [5]`:         "expected [min, max]",
		`[workspaces]` + "\n" + `mirror = [0, 9]`:      ">= 1",
		`[workspaces]` + "\n" + `mirror = [9, 6]`:      ">= 1",
		`[mirror]` + "\n" + `fps = 0`:                  "between 1 and 240",
		`[mirror]` + "\n" + `group = "human"`:          "not session, pack or screen",
		`[mirror]` + "\n" + `per_workspace = 0`:        "below 1",
		`[lifecycle]` + "\n" + `safety_timer = "soon"`: "safety_timer",
		`[lifecycle]` + "\n" + `safety_timer = "10s"`:  "below 1m",
		`[cage]` + "\n" + `renderer = "vulkan"`:        "not auto, gles or pixman",
		`screen = 3`:                                   "",
	}
	for text, want := range cases {
		cfg := Default()
		err := Apply(&cfg, text)
		if err == nil {
			t.Errorf("%q: expected an error", text)
			continue
		}
		if want != "" && !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %q, want it to mention %q", text, err, want)
		}
	}
}

func TestPathHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/x")
	if got := Path(); got != "/tmp/x/hyprcage/config.toml" {
		t.Errorf("Path() = %s", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	if got := Path(); !strings.HasSuffix(got, "/.config/hyprcage/config.toml") {
		t.Errorf("Path() = %s", got)
	}
}

func TestTemplateIsTheDefaults(t *testing.T) {
	cfg := Default()
	if err := Apply(&cfg, Template); err != nil {
		t.Fatal(err)
	}
	if cfg != Default() {
		t.Errorf("the template changes a default:\ngot  %+v\nwant %+v", cfg, Default())
	}
}

func TestWriteDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, written, err := WriteDefault()
	if err != nil || !written || path != Path() {
		t.Fatalf("first write: path=%s written=%v err=%v", path, written, err)
	}
	if err := os.WriteFile(path, []byte("[screen]\nwidth = 1024\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, written, err := WriteDefault(); err != nil || written {
		t.Fatalf("second write: written=%v err=%v", written, err)
	}
	cfg, err := LoadFile(path)
	if err != nil || cfg.DefaultWidth != 1024 {
		t.Errorf("the existing file was touched: width=%d err=%v", cfg.DefaultWidth, err)
	}
}

func TestRecordAndBrowserKeys(t *testing.T) {
	cfg := Default()
	if cfg.RecordDir != "~/Videos/agent" || cfg.RecordMax != 30*time.Minute || cfg.RecordFPS != 30 {
		t.Fatalf("defaults: %+v", cfg)
	}
	err := Apply(&cfg, "[record]\ndir = \"/tmp/rec\"\nmax = \"5m\"\nfps = 5\n[browser]\ncommand = \"chromium\"\nport = 9333\n")
	if err != nil || cfg.RecordDir != "/tmp/rec" || cfg.RecordMax != 5*time.Minute || cfg.RecordFPS != 5 || cfg.BrowserCommand != "chromium" {
		t.Fatalf("overrides: %v %+v", err, cfg)
	}
	for _, bad := range []string{"[record]\nfps = 0", "[record]\nfps = 61", "[record]\nmax = \"0s\"", "[browser]\nport = \"x\""} {
		c := Default()
		if Apply(&c, bad) == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestExpandHome(t *testing.T) {
	t.Setenv("HOME", "/home/x")
	if got := ExpandHome("~/Videos/agent"); got != "/home/x/Videos/agent" {
		t.Errorf("got %s", got)
	}
	if got := ExpandHome("/abs"); got != "/abs" {
		t.Errorf("got %s", got)
	}
}
