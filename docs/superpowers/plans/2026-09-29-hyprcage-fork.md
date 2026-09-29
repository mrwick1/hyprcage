# hyprcage fork Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use subagent-driven-development (recommended) or executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the hyprcage fork with recording, Chrome control, clipboard access, control of the human's own windows, and a waybar menu. The result runs on openSUSE and on Arch.

**Architecture:** All new behaviour lives in the existing Go binary. Each feature gets a small package with pure, testable functions. The MCP server and the CLI stay thin wrappers around those packages. Recording runs as a detached `hyprcage _record` child process with its own Wayland capture connection, and pipes raw frames to `ffmpeg`. Chrome control reuses `chrome-devtools-mcp --browserUrl`.

**Tech Stack:** Go 1.27 (module `github.com/hexadecimil/hyprcage`, kept unchanged so upstream merges stay easy), `github.com/modelcontextprotocol/go-sdk/mcp`, `ffmpeg`, `wl-clipboard`, `cage`, Hyprland 0.56 IPC, bash and `jq` for the waybar scripts.

**Spec:** `docs/superpowers/specs/2026-09-29-hyprcage-fork-design.md`

## Global Constraints

- Hyprland ≥ 0.56 in both configuration modes (Lua and classic). Every new dispatcher string exists for both drivers.
- Look up every external tool on `PATH`. Never hard-code a path.
- Package managers: `pacman` (Arch) and `zypper` (openSUSE).
- Agent Chrome: port `9222`, bound to `127.0.0.1`, temporary profile, one instance at a time.
- Recording folder default `~/Videos/agent/`. Recording cap default `30m`.
- Desktop tools and desktop recording refuse with `session_locked` while `hyprlock` runs.
- Every tool error is a named code (`screen.Error`), never a silent failure.
- Commit identity for this repository: `Arjun KR <arjunkrishnaraj123@gmail.com>`. No AI attribution in commits or PRs.
- Commit messages and comments follow ASD-STE100 style (short, active voice, present tense).
- Every MCP tool has a CLI twin (upstream convention, `internal/cli/cli.go`).

## Spec amendments (found while planning, 2026-09-29)

These are verified facts that change the spec. Task 1 writes them into the spec.

1. **Recording uses `ffmpeg` for both targets.** `wf-recorder` 0.6.0 on openSUSE fails with `symbol lookup error: /lib64/libavformat.so.62: undefined symbol: rist_peer_config_defaults_set_versioned`. hyprcage's own screencopy capture feeds `ffmpeg` instead, for agent screens and for the desktop.
2. **`file_put` and `file_get` are dropped.** Applications on an agent screen run as the human's user and see the same file system. A file picker on an agent screen already reaches every path. The tools would add nothing.
3. **No stand-alone Hyprland rules file.** hyprcage places the mirror window itself. No rule is needed.
4. **Clipboard works in cage (verified).** `wl-copy` and `wl-paste` with `WAYLAND_DISPLAY` set to the inner display round-tripped the text `clip-test-123`. No extra window appeared on the screen.
5. **`ffmpeg` encoders differ per distribution.** openSUSE's `ffmpeg` 9.0.1 offers `libopenh264` and `mpeg4`, but not `libx264`. The recorder picks the first of `libx264`, `libopenh264`, `mpeg4`.

## Review Focus

1. **A window address with Lua or shell syntax in it** (for example `0x1" }) hl.exec_cmd("rm -rf ~`). Expected: the desktop tools reject it with `invalid_address` before anything reaches Hyprland. The test is in Task 7.
2. **`desktop_type` with a character outside the US map** (`é`, an emoji). Expected: an `unsupported_input` error that names the character, and no key is sent at all, so there is no half-typed text. The test is in Task 7.
3. **A screen destroyed while it records.** Expected: the recorder gets SIGTERM, closes `ffmpeg`'s input, and the MP4 file is playable. The test is in Task 11 (smoke script).
4. **Port 9222 already taken**, for example by the human's own Chrome with remote debugging on. Expected: `browser_open` fails with `browser_running` and never attaches to that browser. The test is in Task 8.
5. **The screen changes size during a recording.** Expected: the recording ends with a clear error, and the frames written so far stay a valid file. The test is in Task 3.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/setup/setup.go` (modify) | Install `cage`, `ffmpeg` and `wl-clipboard` through `pacman` or `zypper`. |
| `internal/setup/setup_test.go` (create) | Test the package-manager argv. |
| `install.sh` (modify) | The same `pacman`/`zypper` logic in bash. |
| `internal/config/config.go`, `template.go` (modify) | Add the `[record]` and `[browser]` sections. |
| `internal/screen/errors.go` (modify) | Add the new error codes and an exported `Errf`. |
| `internal/record/record.go` (create) | Encoder choice, `ffmpeg` argv, the frame loop. Pure and testable. |
| `internal/record/state.go` (create) | Recording state files, and starting and stopping the child process. |
| `internal/screen/clipboard.go` (create) | `wl-copy` and `wl-paste` against a screen. |
| `internal/hypr/driver.go` (modify) | `SendShortcutCmd`, `FocusWindowCmd`, `MoveWindowCmd` for both drivers. |
| `internal/desktop/desktop.go` (create) | Lock check, address check, the US key map, window operations on the human's desktop. |
| `internal/browser/browser.go` (create) | Chrome argv, binary lookup, DevTools readiness, state file. |
| `internal/mcpserver/server_fork.go` (create) | Registration and handlers of every new MCP tool. |
| `internal/cli/fork.go` (create) | CLI twins: `record`, `_record`, `clip`, `desktop`, `browser`, `show`. |
| `contrib/waybar/hyprcage-agents`, `hyprcage-menu`, `README.md` (create) | The waybar module and its click menu. |
| `skills/hyprcage/SKILL.md` (modify) | Teach the agent the new tools. |
| `tests/smoke.sh` (create) | End-to-end smoke test for both machines. |

---

### Task 1: Spec amendments

**Files:**
- Modify: `docs/superpowers/specs/2026-09-29-hyprcage-fork-design.md`

**Interfaces:**
- Consumes: nothing.
- Produces: an updated spec that later tasks cite.

- [ ] **Step 1: Edit the spec**

In the "Architecture → 1. The hyprcage fork" table, apply these changes:
- Replace the two `record_start`/`record_stop` rows with one row: `record_start`, `record_stop` for an agent screen or the desktop | Run `hyprcage _record`, which sends frames from hyprcage's wlr-screencopy capture to `ffmpeg`.
- Delete the `file_put`, `file_get` row.

In "Portability", delete the sentence about the Hyprland rules file. Keep the sentence about the waybar module.

In the machine table, change the `wf-recorder` row to: `wf-recorder` | broken on openSUSE (libavformat symbol error), not used | not used.

Add a section "Amendments 2026-09-29" that copies the five numbered amendments from this plan.

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/specs/2026-09-29-hyprcage-fork-design.md
git commit -m "docs: amend the spec with the facts found while planning"
```

---

### Task 2: Package installation on openSUSE and Arch

**Files:**
- Modify: `internal/setup/setup.go`
- Create: `internal/setup/setup_test.go`
- Modify: `install.sh:33`, `install.sh:54-66`, `install.sh:242`

**Interfaces:**
- Consumes: nothing.
- Produces: `setup.InstallArgv(lookPath func(string) (string, error), pkgs []string) ([]string, error)`. `setup.Packages` now lists `cage`, `ffmpeg`, `wl-clipboard`.

- [ ] **Step 1: Write the failing test**

`internal/setup/setup_test.go`:

```go
package setup

import (
	"errors"
	"reflect"
	"testing"
)

func fakeLook(present ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, p := range present {
			if p == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestInstallArgv(t *testing.T) {
	pkgs := []string{"cage", "ffmpeg"}
	cases := []struct {
		name    string
		present []string
		want    []string
	}{
		{"arch", []string{"pacman"}, []string{"pacman", "-S", "--needed", "--noconfirm", "cage", "ffmpeg"}},
		{"opensuse", []string{"zypper"}, []string{"zypper", "--non-interactive", "install", "--no-recommends", "cage", "ffmpeg"}},
	}
	for _, c := range cases {
		got, err := InstallArgv(fakeLook(c.present...), pkgs)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, %v; want %v", c.name, got, err, c.want)
		}
	}
	if _, err := InstallArgv(fakeLook("apt"), pkgs); err == nil {
		t.Error("no known package manager: want an error")
	}
}

func TestPackagesCoverTheNewTools(t *testing.T) {
	want := map[string]string{"cage": "cage", "ffmpeg": "ffmpeg", "wl-clipboard": "wl-copy"}
	for _, p := range Packages {
		if want[p.Package] == p.Binary {
			delete(want, p.Package)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing packages: %v", want)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/setup/ -run 'TestInstallArgv|TestPackagesCoverTheNewTools' -v`
Expected: FAIL, `undefined: InstallArgv`.

- [ ] **Step 3: Implement**

In `internal/setup/setup.go`, replace `Packages`:

```go
var Packages = []struct{ Package, Binary string }{
	{"cage", "cage"},
	{"ffmpeg", "ffmpeg"},             // recording
	{"wl-clipboard", "wl-copy"},      // clipboard of a screen
}
```

Add after `Missing`:

```go
// InstallArgv returns the command that installs pkgs with the first known
// package manager on PATH: pacman (Arch) or zypper (openSUSE).
func InstallArgv(lookPath func(string) (string, error), pkgs []string) ([]string, error) {
	if _, err := lookPath("pacman"); err == nil {
		return append([]string{"pacman", "-S", "--needed", "--noconfirm"}, pkgs...), nil
	}
	if _, err := lookPath("zypper"); err == nil {
		return append([]string{"zypper", "--non-interactive", "install", "--no-recommends"}, pkgs...), nil
	}
	return nil, errors.New("no known package manager (pacman or zypper): install " + strings.Join(pkgs, " and ") + " by hand")
}
```

Replace `ManualCommand`:

```go
// ManualCommand is what the human can run themselves.
func ManualCommand(pkgs []string) string {
	argv, err := InstallArgv(exec.LookPath, pkgs)
	if err != nil {
		return err.Error()
	}
	return "sudo " + strings.Join(argv, " ")
}
```

In `Run`, replace the `pacman` lookup and the `pacman := …` line with:

```go
	pm, err := InstallArgv(exec.LookPath, rep.Missing)
	if err != nil {
		rep.Manual = err.Error()
		return rep, err
	}
```

Then rename every later use of `pacman` inside `Run` to `pm`. Update the package comment "through the distribution's package manager" (it stays correct) and nothing else.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/setup/ -v && go vet ./internal/setup/`
Expected: PASS.

- [ ] **Step 5: Update `install.sh`**

Line 33: `PACKAGES=(cage:cage ffmpeg:ffmpeg wl-clipboard:wl-copy)`

Replace `missing_packages` and `install_packages` (lines 50–66) with:

```bash
missing_packages() {
  local p; for p in "${PACKAGES[@]}"; do have "${p#*:}" || echo "${p%%:*}"; done
}

pm_install() {
  if have pacman; then echo "pacman -S --needed --noconfirm"
  elif have zypper; then echo "zypper --non-interactive install --no-recommends"
  else return 1; fi
}

install_packages() {
  local missing pm; mapfile -t missing < <(missing_packages)
  if [ ${#missing[@]} -eq 0 ]; then say "packages already installed"; return; fi
  pm=$(pm_install) || die "no pacman or zypper: install ${missing[*]} with your package manager, then rerun"
  say "installing ${missing[*]} (sudo will ask for your password)"
  if sudo -n true 2>/dev/null || [ -t 0 ]; then
    sudo $pm "${missing[@]}" || die "package install failed"
  elif have pkexec; then
    pkexec $pm "${missing[@]}" || die "package install failed"
  else
    die "no terminal for sudo and no pkexec: run  sudo $pm ${missing[*]}  then rerun"
  fi
}
```

Line 242: `say "done. cage, ffmpeg and wl-clipboard stay installed; remove them with your package manager"`

- [ ] **Step 6: Check the script**

Run: `bash -n install.sh && bash -c 'source <(sed -n "/^have()/p;/^PACKAGES=/p;/^missing_packages()/,/^}/p;/^pm_install()/,/^}/p" install.sh); missing_packages; pm_install'`
Expected: no syntax error. On `arjun-e16` it prints no missing package, then `zypper --non-interactive install --no-recommends`.

- [ ] **Step 7: Commit**

```bash
git add internal/setup install.sh
git commit -m "feat(setup): install cage, ffmpeg and wl-clipboard with pacman or zypper"
```

---

### Task 3: Error codes, configuration and the recording core

**Files:**
- Modify: `internal/screen/errors.go`
- Modify: `internal/config/config.go`, `internal/config/template.go`, `internal/config/config_test.go`
- Create: `internal/record/record.go`, `internal/record/record_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `screen.Errf(code Code, hint, format string, a ...any) *Error`
  - Codes: `CodeRecording = "recording_active"`, `CodeNotRecording = "not_recording"`, `CodeLocked = "session_locked"`, `CodeBrowserBusy = "browser_running"`, `CodeBrowserDown = "browser_not_running"`, `CodeUnsupported = "unsupported_input"`, `CodeAddress = "invalid_address"`
  - `config.Config` fields: `RecordDir string` (default `"~/Videos/agent"`), `RecordMax time.Duration` (default `30*time.Minute`), `RecordFPS int` (default `10`), `BrowserCommand string` (default `""`), `BrowserPort int` (default `9222`)
  - `config.ExpandHome(p string) string`
  - `record.Capturer` interface `{ Capture(cursor bool) (*image.RGBA, error) }`
  - `record.Encoder(encodersOutput string) string`
  - `record.FFmpegArgs(w, h, fps int, encoder, out string) []string`
  - `record.Loop(ctx context.Context, c Capturer, w io.Writer, fps int, max time.Duration, first *image.RGBA) (int, error)`

- [ ] **Step 1: Add the error codes**

In `internal/screen/errors.go`, add to the `const` block:

```go
	CodeRecording    Code = "recording_active"
	CodeNotRecording Code = "not_recording"
	CodeLocked       Code = "session_locked"
	CodeBrowserBusy  Code = "browser_running"
	CodeBrowserDown  Code = "browser_not_running"
	CodeUnsupported  Code = "unsupported_input"
	CodeAddress      Code = "invalid_address"
```

Add at the end of the file:

```go
// Errf builds an Error for the packages outside screen (record, desktop,
// browser), so that every tool reports the same way.
func Errf(code Code, hint, format string, a ...any) *Error { return errf(code, hint, format, a...) }
```

- [ ] **Step 2: Write the failing configuration test**

Append to `internal/config/config_test.go`:

```go
func TestRecordAndBrowserKeys(t *testing.T) {
	cfg := Default()
	if cfg.RecordDir != "~/Videos/agent" || cfg.RecordMax != 30*time.Minute || cfg.RecordFPS != 10 || cfg.BrowserPort != 9222 {
		t.Fatalf("defaults: %+v", cfg)
	}
	err := Apply(&cfg, "[record]\ndir = \"/tmp/rec\"\nmax = \"5m\"\nfps = 5\n[browser]\ncommand = \"chromium\"\nport = 9333\n")
	if err != nil || cfg.RecordDir != "/tmp/rec" || cfg.RecordMax != 5*time.Minute || cfg.RecordFPS != 5 || cfg.BrowserCommand != "chromium" || cfg.BrowserPort != 9333 {
		t.Fatalf("overrides: %v %+v", err, cfg)
	}
	for _, bad := range []string{"[record]\nfps = 0", "[record]\nfps = 61", "[record]\nmax = \"0s\"", "[browser]\nport = 80"} {
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
```

- [ ] **Step 3: Run it to verify it fails**

Run: `go test ./internal/config/ -run 'TestRecordAndBrowserKeys|TestExpandHome' -v`
Expected: FAIL, `cfg.RecordDir undefined`.

- [ ] **Step 4: Implement the configuration**

In `Config`, after `StableThreshold`, add:

```go
	RecordDir      string        // where recordings go; ~ is the home directory
	RecordMax      time.Duration // a recording stops by itself after this long
	RecordFPS      int           // frames per second of a recording
	BrowserCommand string        // Chrome binary; empty to look one up on PATH
	BrowserPort    int           // DevTools port of the agent's Chrome, on 127.0.0.1
```

In `Default()`, add:

```go
		RecordDir:   "~/Videos/agent",
		RecordMax:   30 * time.Minute,
		RecordFPS:   10,
		BrowserPort: 9222,
```

In `file`, add two sections:

```go
	Record struct {
		Dir string `toml:"dir"`
		Max string `toml:"max"`
		FPS *int   `toml:"fps"`
	} `toml:"record"`
	Browser struct {
		Command *string `toml:"command"`
		Port    *int    `toml:"port"`
	} `toml:"browser"`
```

In `Apply`, before `return cfg.Validate()`:

```go
	if f.Record.Dir != "" {
		cfg.RecordDir = f.Record.Dir
	}
	if f.Record.Max != "" {
		d, err := time.ParseDuration(f.Record.Max)
		if err != nil {
			return fmt.Errorf("record.max: %w", err)
		}
		cfg.RecordMax = d
	}
	setInt(&cfg.RecordFPS, f.Record.FPS)
	if f.Browser.Command != nil {
		cfg.BrowserCommand = strings.TrimSpace(*f.Browser.Command)
	}
	setInt(&cfg.BrowserPort, f.Browser.Port)
```

In `Validate`, add cases to the first `switch`:

```go
	case c.RecordFPS < 1 || c.RecordFPS > 60:
		return fmt.Errorf("record.fps: %d is not between 1 and 60", c.RecordFPS)
	case c.RecordMax < time.Second:
		return fmt.Errorf("record.max: %s is below 1s", c.RecordMax)
	case c.BrowserPort < 1024 || c.BrowserPort > 65535:
		return fmt.Errorf("browser.port: %d is not between 1024 and 65535", c.BrowserPort)
```

Add at the end of `config.go`:

```go
// ExpandHome replaces a leading ~/ with the home directory.
func ExpandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return p
}
```

In `template.go`, append before the closing backtick:

```toml

[record]
dir = "~/Videos/agent" # recordings of screens and of the desktop go here
max = "30m"           # a recording stops by itself after this long
fps = 10              # frames per second of a recording

[browser]
command = ""          # Chrome binary for browser_open, empty to look one up on PATH
port = 9222           # DevTools port of the agent's Chrome, always on 127.0.0.1
```

- [ ] **Step 5: Run the configuration tests**

Run: `go test ./internal/config/ -v`
Expected: PASS, `TestTemplateIsTheDefaults` included.

- [ ] **Step 6: Write the failing recording test**

`internal/record/record_test.go`:

```go
package record

import (
	"bytes"
	"context"
	"image"
	"strings"
	"testing"
	"time"
)

type fakeCap struct {
	img   *image.RGBA
	calls int
	grow  int // after this many calls, return a bigger image (0 = never)
}

func (f *fakeCap) Capture(bool) (*image.RGBA, error) {
	f.calls++
	if f.grow > 0 && f.calls >= f.grow {
		return image.NewRGBA(image.Rect(0, 0, 8, 8)), nil
	}
	return f.img, nil
}

func TestEncoder(t *testing.T) {
	cases := map[string]string{
		" V....D libx264   H.264\n V....D libopenh264 H.264\n": "libx264",
		" V....D libopenh264 OpenH264\n V....D mpeg4 MPEG-4\n":   "libopenh264",
		" V....D mpeg4 MPEG-4 part 2\n":                          "mpeg4",
	}
	for in, want := range cases {
		if got := Encoder(in); got != want {
			t.Errorf("Encoder(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestFFmpegArgs(t *testing.T) {
	a := strings.Join(FFmpegArgs(1280, 800, 10, "libopenh264", "/tmp/o.mp4"), " ")
	for _, want := range []string{"-f rawvideo", "-pix_fmt rgba", "-s 1280x800", "-framerate 10", "-i -", "-c:v libopenh264", "-pix_fmt yuv420p", "/tmp/o.mp4"} {
		if !strings.Contains(a, want) {
			t.Errorf("argv lacks %q: %s", want, a)
		}
	}
}

func TestLoopKeepsRealTime(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	var buf bytes.Buffer
	n, err := Loop(context.Background(), &fakeCap{img: img}, &buf, 20, 250*time.Millisecond, img)
	if err != nil {
		t.Fatal(err)
	}
	if n < 4 || n > 7 {
		t.Errorf("%d frames for 250 ms at 20 fps, want about 5", n)
	}
	if buf.Len() != n*4*2*4 {
		t.Errorf("%d bytes for %d frames of 4x2 RGBA", buf.Len(), n)
	}
}

func TestLoopStopsOnCancel(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	start := time.Now()
	if _, err := Loop(ctx, &fakeCap{img: img}, &bytes.Buffer{}, 10, time.Hour, img); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Error("Loop did not stop on cancel")
	}
}

func TestLoopRejectsSizeChange(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	n, err := Loop(context.Background(), &fakeCap{img: img, grow: 2}, &buf, 50, time.Second, img)
	if err == nil || !strings.Contains(err.Error(), "changed size") {
		t.Fatalf("want a size error, got %v", err)
	}
	if buf.Len() != n*4*4*4 {
		t.Errorf("partial frame written: %d bytes for %d frames", buf.Len(), n)
	}
}
```

- [ ] **Step 7: Run it to verify it fails**

Run: `go test ./internal/record/ -v`
Expected: FAIL, the package does not exist.

- [ ] **Step 8: Implement the recording core**

`internal/record/record.go`:

```go
// Package record turns a screen or the human's desktop into an MP4 file:
// hyprcage captures frames through wlr-screencopy and pipes raw RGBA to
// ffmpeg. wf-recorder is not used: on openSUSE it fails with a libavformat
// symbol error.
package record

import (
	"context"
	"fmt"
	"image"
	"io"
	"strconv"
	"strings"
	"time"
)

// Capturer grabs one frame. *wl.Client satisfies it.
type Capturer interface {
	Capture(cursor bool) (*image.RGBA, error)
}

// Encoder picks the first H.264 encoder in the output of `ffmpeg -encoders`,
// else mpeg4, which every ffmpeg build has. Arch ships libx264, openSUSE
// only libopenh264.
func Encoder(encodersOutput string) string {
	for _, e := range []string{"libx264", "libopenh264"} {
		if strings.Contains(encodersOutput, " "+e+" ") {
			return e
		}
	}
	return "mpeg4"
}

// FFmpegArgs is the ffmpeg command that reads raw w x h RGBA frames on
// stdin and writes out. The scale filter rounds the size down to even
// numbers, which yuv420p needs.
func FFmpegArgs(w, h, fps int, encoder, out string) []string {
	return []string{"ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "rawvideo", "-pix_fmt", "rgba", "-s", fmt.Sprintf("%dx%d", w, h), "-framerate", strconv.Itoa(fps), "-i", "-",
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2", "-c:v", encoder, "-pix_fmt", "yuv420p", out}
}

// Loop writes first, then one capture per frame period, to w until ctx
// ends or max elapses. When a capture comes late, the previous image is
// written again, so the video keeps real time. It returns the number of
// frames written. A frame is always written whole.
func Loop(ctx context.Context, c Capturer, w io.Writer, fps int, max time.Duration, first *image.RGBA) (int, error) {
	start := time.Now()
	period := time.Second / time.Duration(fps)
	img, written := first, 0
	for {
		due := int(time.Since(start)/period) + 1
		for ; written < due; written++ {
			if err := writeFrame(w, img); err != nil {
				return written, err
			}
		}
		if time.Since(start) >= max {
			return written, nil
		}
		select {
		case <-ctx.Done():
			return written, nil
		case <-time.After(time.Until(start.Add(time.Duration(written) * period))):
		}
		next, err := c.Capture(true)
		if err != nil {
			return written, err
		}
		if next.Bounds() != first.Bounds() {
			return written, fmt.Errorf("record: the screen changed size to %v", next.Bounds().Size())
		}
		img = next
	}
}

func writeFrame(w io.Writer, img *image.RGBA) error {
	b := img.Bounds()
	row := b.Dx() * 4
	for y := 0; y < b.Dy(); y++ {
		off := y * img.Stride
		if _, err := w.Write(img.Pix[off : off+row]); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 9: Run the tests**

Run: `go test ./internal/record/ ./internal/config/ ./internal/screen/ -v && go vet ./...`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/screen/errors.go internal/config internal/record
git commit -m "feat(record): add the frame loop, the ffmpeg argv and the record settings"
```

---

### Task 4: Recording process, CLI and MCP tools

**Files:**
- Create: `internal/record/state.go`, `internal/record/state_test.go`
- Create: `internal/cli/fork.go`
- Modify: `internal/cli/cli.go` (the `commands` list)
- Create: `internal/mcpserver/server_fork.go`
- Modify: `internal/mcpserver/server.go` (`register` calls `s.registerFork(srv)`)
- Modify: `internal/mcpserver/server_test.go` (`want` list)

**Interfaces:**
- Consumes: `record.Loop`, `record.Encoder`, `record.FFmpegArgs`, `screen.Errf`, the codes and config fields from Task 3. `desktop.Locked` comes from Task 7. Until Task 7 lands, this task uses a local `lockedFn` variable (see Step 3).
- Produces:
  - `record.State{Target, Path string; PID int; Started time.Time}`
  - `record.StatePath(target string) string`, `record.Load(target string) (State, error)`, `record.List() ([]State, error)`
  - `record.Start(exe, target string, isScreen bool, cfg config.Config) (State, error)`
  - `record.Stop(target string) (State, error)`
  - `record.RunChild(ctx context.Context, target, display, out string, fps int, max time.Duration) error`
  - CLI: `hyprcage record start [screen|desktop]`, `hyprcage record stop [screen|desktop] [--json]`, `hyprcage record status [--json]`, hidden `hyprcage _record <target> <out>`
  - MCP: `record_start {target?}`, `record_stop {target?}`
  - `(s *Server) registerFork(srv *mcp.Server)` in `server_fork.go`, extended by later tasks

- [ ] **Step 1: Write the failing state test**

`internal/record/state_test.go`:

```go
package record

import (
	"os"
	"testing"
	"time"
)

func TestStateRoundTripAndDeadCleanup(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	live := State{Target: "hc-a", PID: os.Getpid(), Path: "/tmp/a.mp4", Started: time.Now()}
	dead := State{Target: "desktop", PID: 1 << 30, Path: "/tmp/d.mp4", Started: time.Now()}
	for _, s := range []State{live, dead} {
		if err := save(s); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Load("hc-a")
	if err != nil || got.Path != live.Path {
		t.Fatalf("Load: %+v %v", got, err)
	}
	list, err := List()
	if err != nil || len(list) != 1 || list[0].Target != "hc-a" {
		t.Fatalf("List keeps only live recorders: %+v %v", list, err)
	}
	if _, err := os.Stat(StatePath("desktop")); !os.IsNotExist(err) {
		t.Error("the dead recorder's state file is still there")
	}
}

func TestStopWithoutRecording(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if _, err := Stop("hc-none"); err == nil {
		t.Fatal("want not_recording")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/record/ -run 'TestState|TestStop' -v`
Expected: FAIL, `undefined: save`.

- [ ] **Step 3: Implement the state and the child process**

`internal/record/state.go`:

```go
package record

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// Desktop is the target name of the human's own screen.
const Desktop = "desktop"

// lockedFn reports whether the human's session is locked. Task 7 points it
// at desktop.Locked.
var lockedFn = func() bool { return false }

// State is the record of one running recorder.
type State struct {
	Target  string    `json:"target"`
	PID     int       `json:"pid"`
	Path    string    `json:"path"`
	Started time.Time `json:"started"`
}

// StatePath is the state file of the recorder of target.
func StatePath(target string) string {
	return filepath.Join(registry.Dir(), "rec-"+target+".json")
}

func save(s State) error {
	if _, err := registry.EnsureDir(); err != nil {
		return err
	}
	data, _ := json.Marshal(s)
	return os.WriteFile(StatePath(s.Target), data, 0o600)
}

func alive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }

// Load returns the state of target's recorder, or not_recording.
func Load(target string) (State, error) {
	var s State
	data, err := os.ReadFile(StatePath(target))
	if err == nil {
		err = json.Unmarshal(data, &s)
	}
	if err != nil || !alive(s.PID) {
		_ = os.Remove(StatePath(target))
		return State{}, screen.Errf(screen.CodeNotRecording, "record_start first", "no recording of %s", target)
	}
	return s, nil
}

// List returns the live recorders and removes the files of dead ones.
func List() ([]State, error) {
	files, err := filepath.Glob(filepath.Join(registry.Dir(), "rec-*.json"))
	if err != nil {
		return nil, err
	}
	out := []State{}
	for _, f := range files {
		target := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "rec-"), ".json")
		if s, err := Load(target); err == nil {
			out = append(out, s)
		}
	}
	return out, nil
}

// Start runs `exe _record target out` detached and waits for its state
// file. A screen's recorder carries HYPRCAGE_SCREEN, so destroying the
// screen sends it SIGTERM and the file is finalised.
func Start(exe, target string, isScreen bool, cfg config.Config) (State, error) {
	if s, err := Load(target); err == nil {
		return s, screen.Errf(screen.CodeRecording, "record_stop first", "%s is already recording to %s", target, s.Path)
	}
	if !isScreen && lockedFn() {
		return State{}, screen.Errf(screen.CodeLocked, "wait until the human unlocks", "the desktop is locked")
	}
	dir := config.ExpandHome(cfg.RecordDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return State{}, err
	}
	out := filepath.Join(dir, fmt.Sprintf("%s-%s.mp4", target, time.Now().Format("20060102-150405")))
	cmd := exec.Command(exe, "_record", target, out)
	cmd.Env = os.Environ()
	if isScreen {
		cmd.Env = append(cmd.Env, "HYPRCAGE_SCREEN="+target)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return State{}, err
	}
	go func() { _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s, err := Load(target); err == nil {
			return s, nil
		}
		if !alive(cmd.Process.Pid) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return State{}, screen.Errf(screen.CodeCapture, "see "+filepath.Join(screen.LogDir(), "record-"+target+".log"), "the recorder of %s did not start", target)
}

// Stop sends SIGTERM to target's recorder and waits until ffmpeg has
// written the file.
func Stop(target string) (State, error) {
	s, err := Load(target)
	if err != nil {
		return s, err
	}
	_ = syscall.Kill(s.PID, syscall.SIGTERM)
	for i := 0; i < 150 && alive(s.PID); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if alive(s.PID) {
		return s, screen.Errf(screen.CodeTimeout, "", "the recorder of %s did not stop within 15 s", target)
	}
	return s, nil
}

// RunChild is the body of `hyprcage _record`: capture display, pipe the
// frames to ffmpeg, stop on ctx, max or a capture error.
func RunChild(ctx context.Context, target, display, out string, fps int, max time.Duration) error {
	_ = os.MkdirAll(screen.LogDir(), 0o700)
	cl, err := wl.ConnectCapture(display)
	if err != nil {
		return err
	}
	defer cl.Close()
	// ponytail: the capture connection binds the first wl_output, so on a
	// desktop with several monitors only one is recorded; add an output
	// choice when a second monitor is in use.
	first, err := cl.Capture(true)
	if err != nil {
		return err
	}
	encs, _ := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	b := first.Bounds()
	argv := FFmpegArgs(b.Dx(), b.Dy(), fps, Encoder(string(encs)), out)
	ff := exec.Command(argv[0], argv[1:]...)
	// ffmpeg must not carry HYPRCAGE_SCREEN: destroy signals the recorder,
	// which closes ffmpeg's input so that the file is finalised.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "HYPRCAGE_SCREEN=") {
			ff.Env = append(ff.Env, kv)
		}
	}
	logf, _ := os.OpenFile(filepath.Join(screen.LogDir(), "record-"+target+".log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if logf != nil {
		defer logf.Close()
		ff.Stdout, ff.Stderr = logf, logf
	}
	stdin, err := ff.StdinPipe()
	if err != nil {
		return err
	}
	if err := ff.Start(); err != nil {
		return err
	}
	if err := save(State{Target: target, PID: os.Getpid(), Path: out, Started: time.Now()}); err != nil {
		_ = ff.Process.Kill()
		return err
	}
	defer os.Remove(StatePath(target))
	_, loopErr := Loop(ctx, cl, stdin, fps, max, first)
	_ = stdin.Close()
	ffErr := ff.Wait()
	return errors.Join(ignoreClosedPipe(loopErr), ffErr)
}

func ignoreClosedPipe(err error) error {
	if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, syscall.EPIPE) {
		return nil
	}
	return err
}
```

- [ ] **Step 4: Run the state tests**

Run: `go test ./internal/record/ -v`
Expected: PASS.

- [ ] **Step 5: Add the CLI twins**

`internal/cli/fork.go`:

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/hypr"
	"github.com/hexadecimil/hyprcage/internal/record"
	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/session"
)

// recordTarget resolves "" or a screen name to a screen, and keeps "desktop".
func recordTarget(name string) (string, bool, error) {
	if name == record.Desktop {
		return record.Desktop, false, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return "", false, err
	}
	c, err := screen.Connect(cfg)
	if err != nil {
		return "", false, err
	}
	rec, err := screen.Resolve(c, name, session.Current())
	if err != nil {
		return "", false, err
	}
	return rec.Name, true, nil
}

func runRecord(e *Env) int {
	fs := e.flags("record")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	usage := "usage: hyprcage record start|stop [screen|desktop] [--json] | record status [--json]"
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return e.errorf(usage)
	}
	if fs.Arg(0) == "status" {
		list, err := record.List()
		if err != nil {
			return e.fail(err)
		}
		if *asJSON {
			return e.printJSON(list)
		}
		for _, s := range list {
			fmt.Fprintf(e.Stdout, "%-12s pid=%d  %s\n", s.Target, s.PID, s.Path)
		}
		return ExitOK
	}
	target, isScreen, err := recordTarget(fs.Arg(1))
	if err != nil {
		return e.fail(err)
	}
	var s record.State
	switch fs.Arg(0) {
	case "start":
		cfg, err := config.Load()
		if err != nil {
			return e.fail(err)
		}
		exe, err := os.Executable()
		if err != nil {
			return e.fail(err)
		}
		s, err = record.Start(exe, target, isScreen, cfg)
		if err != nil {
			return e.fail(err)
		}
	case "stop":
		s, err = record.Stop(target)
		if err != nil {
			return e.fail(err)
		}
	default:
		return e.errorf(usage)
	}
	if *asJSON {
		return e.printJSON(s)
	}
	fmt.Fprintln(e.Stdout, s.Path)
	return ExitOK
}

// runRecordChild is `hyprcage _record <target> <out>`.
func runRecordChild(e *Env) int {
	if len(e.Args) != 2 {
		return e.errorf("usage: hyprcage _record <target> <out>")
	}
	target, out := e.Args[0], e.Args[1]
	cfg := config.Fallback()
	var display string
	if target == record.Desktop {
		inst, err := hypr.Discover()
		if err != nil {
			return e.fail(err)
		}
		if display, err = inst.WaylandDisplay(); err != nil {
			return e.fail(err)
		}
	} else {
		rec, err := registry.Load(target)
		if err != nil {
			return e.fail(err)
		}
		display = rec.InnerDisplay
		if display == "" {
			inner, err := registry.ReadInner(target)
			if err != nil {
				return e.fail(err)
			}
			display = inner["WAYLAND_DISPLAY"]
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := record.RunChild(ctx, target, display, out, cfg.RecordFPS, cfg.RecordMax); err != nil {
		return e.fail(err)
	}
	return ExitOK
}
```

In `internal/cli/cli.go`, add to `commands` before the hidden entries:

```go
		{"record", "record a screen or the desktop to MP4 (start, stop, status)", runRecord, false},
```

and among the hidden entries:

```go
		{"_record", "internal: captures a screen or the desktop into ffmpeg", runRecordChild, true},
```

- [ ] **Step 6: Add the MCP tools**

`internal/mcpserver/server_fork.go`:

```go
package mcpserver

import (
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/record"
)

type recordIn struct {
	Target string `json:"target,omitempty" jsonschema:"a screen name, or \"desktop\" for the human's own screen; optional when the session owns exactly one screen"`
}

// registerFork adds the tools of the mrwick1 fork.
func (s *Server) registerFork(srv *mcp.Server) {
	tool(s, srv, "record_start", "Start recording a screen (or the human's desktop with target=\"desktop\") to an MP4 file. It stops by itself after the configured maximum (30 min by default), when the screen closes, or with record_stop. Desktop recording is refused while the human's session is locked.", s.recordStart)
	tool(s, srv, "record_stop", "Stop a recording and return the path of the MP4 file.", s.recordStop)
}

func (s *Server) recordTarget(target string) (string, bool, error) {
	if target == record.Desktop {
		return record.Desktop, false, nil
	}
	rec, _, err := s.resolve(target, false)
	if err != nil {
		return "", false, err
	}
	return rec.Name, true, nil
}

func (s *Server) recordStart(in recordIn) (*mcp.CallToolResult, error) {
	target, isScreen, err := s.recordTarget(in.Target)
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	st, err := record.Start(exe, target, isScreen, s.cfg)
	if err != nil {
		return nil, err
	}
	return textResult(map[string]any{"target": st.Target, "path": st.Path, "max": s.cfg.RecordMax.String()}), nil
}

func (s *Server) recordStop(in recordIn) (*mcp.CallToolResult, error) {
	target, _, err := s.recordTarget(in.Target)
	if err != nil {
		return nil, err
	}
	st, err := record.Stop(target)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"target": st.Target, "path": st.Path}
	if fi, err := os.Stat(st.Path); err == nil {
		out["bytes"] = fi.Size()
	}
	return textResult(out), nil
}
```

In `server.go`, add `s.registerFork(srv)` as the last line of `register`.

In `server_test.go`, add `"record_start", "record_stop"` to `want`.

- [ ] **Step 7: Run the tests**

Run: `go test ./... && go vet ./...`
Expected: PASS.

- [ ] **Step 8: Try it on a live screen**

Run:

```bash
make build
./hyprcage create -name rec -no-mirror
./hyprcage launch hc-rec -- kitty
./hyprcage record start hc-rec
sleep 5
./hyprcage record stop hc-rec
ffprobe -v error -count_frames -select_streams v -show_entries stream=codec_name,width,height,nb_read_frames -of csv=p=0 ~/Videos/agent/hc-rec-*.mp4
./hyprcage destroy hc-rec
```

Expected: one line like `h264,1280,800,50`, which means about 50 frames for 5 s at 10 fps. Record the exact line in the commit message body.

Then repeat with the desktop while unlocked: `./hyprcage record start desktop; sleep 3; ./hyprcage record stop desktop`. Expected: a 1920x1200 file with about 30 frames.

- [ ] **Step 9: Commit**

```bash
git add internal/record internal/cli internal/mcpserver
git commit -m "feat(record): record a screen or the desktop to MP4"
```

---

### Task 5: Clipboard of a screen

**Files:**
- Create: `internal/screen/clipboard.go`, `internal/screen/clipboard_test.go`
- Modify: `internal/cli/fork.go`, `internal/cli/cli.go`, `internal/mcpserver/server_fork.go`, `internal/mcpserver/server_test.go`

**Interfaces:**
- Consumes: `registry.Screen.InnerDisplay`, `registry.ReadInner`.
- Produces: `screen.ClipboardSet(rec *registry.Screen, text string) error`, `screen.ClipboardGet(rec *registry.Screen) (string, error)`, `screen.innerEnv(rec *registry.Screen, base []string) []string`. CLI `hyprcage clip get|set [screen] [text]`. MCP `clipboard_get {screen?}`, `clipboard_set {screen?, text}`.

- [ ] **Step 1: Write the failing test**

`internal/screen/clipboard_test.go`:

```go
package screen

import (
	"slices"
	"testing"

	"github.com/hexadecimil/hyprcage/internal/registry"
)

func TestInnerEnv(t *testing.T) {
	rec := &registry.Screen{Name: "hc-a", InnerDisplay: "wayland-7"}
	base := []string{"WAYLAND_DISPLAY=wayland-1", "HYPRLAND_INSTANCE_SIGNATURE=x", "HOME=/h"}
	got := innerEnv(rec, base)
	for _, want := range []string{"WAYLAND_DISPLAY=wayland-7", "HYPRCAGE_SCREEN=hc-a", "HOME=/h"} {
		if !slices.Contains(got, want) {
			t.Errorf("env lacks %s: %v", want, got)
		}
	}
	for _, bad := range []string{"WAYLAND_DISPLAY=wayland-1", "HYPRLAND_INSTANCE_SIGNATURE=x"} {
		if slices.Contains(got, bad) {
			t.Errorf("env keeps %s", bad)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/screen/ -run TestInnerEnv -v`
Expected: FAIL, `undefined: innerEnv`.

- [ ] **Step 3: Implement**

`internal/screen/clipboard.go`:

```go
package screen

import (
	"bytes"
	"os"
	"os/exec"
	"strings"

	"github.com/hexadecimil/hyprcage/internal/registry"
)

// innerEnv points a client at the screen's cage and tags it with the
// screen, so that destroy stops it (wl-copy keeps serving in background).
func innerEnv(rec *registry.Screen, base []string) []string {
	var env []string
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if k == "WAYLAND_DISPLAY" || k == "HYPRLAND_INSTANCE_SIGNATURE" || k == "HYPRCAGE_SCREEN" {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "WAYLAND_DISPLAY="+rec.InnerDisplay, "HYPRCAGE_SCREEN="+rec.Name)
}

func withInner(rec *registry.Screen) error {
	if rec.InnerDisplay != "" {
		return nil
	}
	inner, err := registry.ReadInner(rec.Name)
	if err != nil {
		return errf(CodeDead, "", "screen %s has no inner socket", rec.Name)
	}
	rec.InnerDisplay = inner["WAYLAND_DISPLAY"]
	return nil
}

// ClipboardSet puts text on the screen's clipboard.
func ClipboardSet(rec *registry.Screen, text string) error {
	if err := withInner(rec); err != nil {
		return err
	}
	cmd := exec.Command("wl-copy")
	cmd.Env = innerEnv(rec, os.Environ())
	cmd.Stdin = strings.NewReader(text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return errf(CodeCapture, "is wl-clipboard installed?", "wl-copy: %v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// ClipboardGet reads the screen's clipboard; an empty clipboard is "".
func ClipboardGet(rec *registry.Screen) (string, error) {
	if err := withInner(rec); err != nil {
		return "", err
	}
	cmd := exec.Command("wl-paste", "--no-newline")
	cmd.Env = innerEnv(rec, os.Environ())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if strings.Contains(stderr.String(), "Nothing is copied") {
			return "", nil
		}
		return "", errf(CodeCapture, "is wl-clipboard installed?", "wl-paste: %v: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	return string(out), nil
}
```

- [ ] **Step 4: Add the CLI twin**

Append to `internal/cli/fork.go`:

```go
func runClip(e *Env) int {
	fs := e.flags("clip")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	usage := "usage: hyprcage clip get [screen] | clip set [screen] <text>"
	if fs.NArg() < 1 {
		return e.errorf(usage)
	}
	cfg, err := config.Load()
	if err != nil {
		return e.fail(err)
	}
	c, err := screen.Connect(cfg)
	if err != nil {
		return e.fail(err)
	}
	switch {
	case fs.Arg(0) == "get" && fs.NArg() <= 2:
		rec, err := screen.Resolve(c, fs.Arg(1), session.Current())
		if err != nil {
			return e.fail(err)
		}
		text, err := screen.ClipboardGet(rec)
		if err != nil {
			return e.fail(err)
		}
		fmt.Fprint(e.Stdout, text)
		return ExitOK
	case fs.Arg(0) == "set" && (fs.NArg() == 2 || fs.NArg() == 3):
		name, text := "", fs.Arg(1)
		if fs.NArg() == 3 {
			name, text = fs.Arg(1), fs.Arg(2)
		}
		rec, err := screen.Resolve(c, name, session.Current())
		if err != nil {
			return e.fail(err)
		}
		if err := screen.ClipboardSet(rec, text); err != nil {
			return e.fail(err)
		}
		return ExitOK
	}
	return e.errorf(usage)
}
```

Add to `commands` in `cli.go`: `{"clip", "read or write the clipboard of a screen", runClip, false},`

- [ ] **Step 5: Add the MCP tools**

In `server_fork.go`, add the input type:

```go
type clipIn struct {
	Screen string `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
	Text   string `json:"text,omitempty" jsonschema:"text to put on the clipboard (clipboard_set only)"`
}
```

Add to `registerFork`:

```go
	tool(s, srv, "clipboard_get", "Read the clipboard of a screen (not the human's clipboard).", s.clipboardGet)
	tool(s, srv, "clipboard_set", "Put text on the clipboard of a screen (not the human's clipboard); paste it in the app with key ctrl+v.", s.clipboardSet)
```

Add the handlers (import `github.com/hexadecimil/hyprcage/internal/screen`):

```go
func (s *Server) clipboardGet(in clipIn) (*mcp.CallToolResult, error) {
	rec, _, err := s.resolve(in.Screen, false)
	if err != nil {
		return nil, err
	}
	text, err := screen.ClipboardGet(rec)
	if err != nil {
		return nil, err
	}
	return textResult(map[string]any{"screen": rec.Name, "text": text}), nil
}

func (s *Server) clipboardSet(in clipIn) (*mcp.CallToolResult, error) {
	rec, _, err := s.resolve(in.Screen, false)
	if err != nil {
		return nil, err
	}
	if err := screen.ClipboardSet(rec, in.Text); err != nil {
		return nil, err
	}
	return textResult(map[string]string{"status": "ok"}), nil
}
```

Add `"clipboard_get", "clipboard_set"` to `want` in `server_test.go`.

- [ ] **Step 6: Run the tests and a live check**

Run: `go test ./... && go vet ./... && make build`
Then:

```bash
./hyprcage create -name clip -no-mirror && ./hyprcage launch hc-clip -- kitty
./hyprcage clip set hc-clip 'clip-test-123' && test "$(./hyprcage clip get hc-clip)" = clip-test-123 && echo CLIP_OK
./hyprcage destroy hc-clip
grep -l 'HYPRCAGE_SCREEN=hc-clip' /proc/[0-9]*/environ 2>/dev/null | wc -l
```

Expected: `CLIP_OK`, then `0` (the background `wl-copy` is gone after destroy).

- [ ] **Step 7: Commit**

```bash
git add internal/screen internal/cli internal/mcpserver
git commit -m "feat(screen): read and write the clipboard of a screen"
```

---

### Task 6: Hyprland dispatchers for the human's windows

**Files:**
- Modify: `internal/hypr/driver.go`, `internal/hypr/driver_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: three new `ConfigDriver` methods:
  - `SendShortcutCmd(mods, key, address string) string`
  - `FocusWindowCmd(address string) string`
  - `MoveWindowCmd(address string, workspace int) string`

- [ ] **Step 1: Write the failing test**

Append to `internal/hypr/driver_test.go`:

```go
func TestWindowDispatchers(t *testing.T) {
	lua, classic := &luaDriver{}, &classicDriver{}
	cases := []struct{ got, want string }{
		{lua.SendShortcutCmd("SHIFT", "x", "0xabc"), `dispatch hl.dsp.send_shortcut({ mods = "SHIFT", key = "x", window = "address:0xabc" })`},
		{classic.SendShortcutCmd("SHIFT", "x", "0xabc"), "dispatch sendshortcut SHIFT, x, address:0xabc"},
		{lua.FocusWindowCmd("0xabc"), `dispatch hl.dsp.focus({ window = "address:0xabc" })`},
		{classic.FocusWindowCmd("0xabc"), "dispatch focuswindow address:0xabc"},
		{lua.MoveWindowCmd("0xabc", 4), `dispatch hl.dsp.window.move({ workspace = "4", follow = false, window = "address:0xabc" })`},
		{classic.MoveWindowCmd("0xabc", 4), "dispatch movetoworkspacesilent 4,address:0xabc"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got  %s\nwant %s", c.got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/hypr/ -run TestWindowDispatchers -v`
Expected: FAIL, `lua.SendShortcutCmd undefined`.

- [ ] **Step 3: Implement**

In the `ConfigDriver` interface, after `MoveCursorCmd`:

```go
	// SendShortcutCmd, FocusWindowCmd and MoveWindowCmd act on one of the
	// human's windows by address. send_shortcut moves keyboard focus to the
	// window and back for each key (verified in the 0.56.2 source), so the
	// human's window sees a leave/enter pair.
	SendShortcutCmd(mods, key, address string) string
	FocusWindowCmd(address string) string
	MoveWindowCmd(address string, workspace int) string
```

Lua driver:

```go
func (d *luaDriver) SendShortcutCmd(mods, key, address string) string {
	return fmt.Sprintf("dispatch hl.dsp.send_shortcut({ mods = %s, key = %s, window = %s })",
		LuaString(mods), LuaString(key), LuaString("address:"+address))
}

func (d *luaDriver) FocusWindowCmd(address string) string {
	return "dispatch hl.dsp.focus({ window = " + LuaString("address:"+address) + " })"
}

func (d *luaDriver) MoveWindowCmd(address string, workspace int) string {
	return fmt.Sprintf(`dispatch hl.dsp.window.move({ workspace = "%d", follow = false, window = %s })`,
		workspace, LuaString("address:"+address))
}
```

Classic driver:

```go
func (d *classicDriver) SendShortcutCmd(mods, key, address string) string {
	return fmt.Sprintf("dispatch sendshortcut %s, %s, address:%s", mods, key, address)
}

func (d *classicDriver) FocusWindowCmd(address string) string {
	return "dispatch focuswindow address:" + address
}

func (d *classicDriver) MoveWindowCmd(address string, workspace int) string {
	return fmt.Sprintf("dispatch movetoworkspacesilent %d,address:%s", workspace, address)
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/hypr/ -v && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Verify the Lua strings on the live Hyprland**

`send_shortcut` and `window.move` were verified on 2026-09-29. `hl.dsp.focus({ window = … })` was not. Verify it with a throwaway kitty on workspace 10, then return focus to the window that had it:

```bash
before=$(hyprctl activewindow -j | jq -r .address)
hyprctl eval 'hl.dispatch(hl.dsp.exec_cmd("kitty --class hc-focus-probe", { workspace = "10 silent" }))'; sleep 2
addr=$(hyprctl clients -j | jq -r '.[]|select(.class=="hc-focus-probe")|.address')
hyprctl --batch "dispatch hl.dsp.focus({ window = \"address:$addr\" })"; sleep 0.3
test "$(hyprctl activewindow -j | jq -r .address)" = "$addr" && echo FOCUS_OK
hyprctl --batch "dispatch hl.dsp.focus({ window = \"address:$before\" })"
hyprctl eval "hl.dispatch(hl.dsp.window.close({ window = \"address:$addr\" }))"
```

Expected: `FOCUS_OK`. When it fails, read the Lua API names with `strings /usr/bin/Hyprland | grep 'hl.dsp.focus'`, fix `FocusWindowCmd` and its test, and repeat. This step moves the human's focus for a moment. Tell the human before running it.

- [ ] **Step 6: Commit**

```bash
git add internal/hypr
git commit -m "feat(hypr): add window focus, move and send_shortcut dispatchers"
```

---

### Task 7: Control of the human's windows

**Files:**
- Create: `internal/desktop/desktop.go`, `internal/desktop/desktop_test.go`
- Modify: `internal/record/state.go` (`lockedFn`)
- Modify: `internal/cli/fork.go`, `internal/cli/cli.go`, `internal/mcpserver/server_fork.go`, `internal/mcpserver/server_test.go`

**Interfaces:**
- Consumes: `hypr.Instance`, the Task 6 dispatchers, `screen.Errf` and the codes.
- Produces:
  - `desktop.Locked(procRoot string) bool`
  - `desktop.CheckAddress(a string) error`
  - `desktop.KeyFor(r rune) (mods, key string, err error)`
  - `desktop.ParseCombo(combo string) (mods, key string, err error)`
  - `desktop.Desktop{H *hypr.Instance; D hypr.ConfigDriver}` with `Windows() ([]hypr.Client, error)`, `Focus(addr string) error`, `Move(addr string, ws int) error`, `Type(addr, text string) error`, `Key(addr string, combos []string) error`
  - CLI: `hyprcage desktop windows|focus|move|type|key …`
  - MCP: `desktop_windows`, `desktop_focus`, `desktop_move`, `desktop_type`, `desktop_key`

- [ ] **Step 1: Write the failing test**

`internal/desktop/desktop_test.go`:

```go
package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocked(t *testing.T) {
	root := t.TempDir()
	mk := func(pid, comm string) {
		_ = os.MkdirAll(filepath.Join(root, pid), 0o755)
		_ = os.WriteFile(filepath.Join(root, pid, "comm"), []byte(comm+"\n"), 0o644)
	}
	mk("10", "kitty")
	if Locked(root) {
		t.Fatal("no hyprlock: want unlocked")
	}
	mk("11", "hyprlock")
	if !Locked(root) {
		t.Fatal("hyprlock runs: want locked")
	}
}

func TestCheckAddress(t *testing.T) {
	for _, ok := range []string{"0x55ebac116320", "0xabc"} {
		if err := CheckAddress(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "55eb", "0x", "0xZZ", `0x1" }) hl.exec_cmd("rm -rf ~`, "0x1 ,x", "address:0x1"} {
		if CheckAddress(bad) == nil {
			t.Errorf("%q: want invalid_address", bad)
		}
	}
}

func TestKeyFor(t *testing.T) {
	cases := map[rune][2]string{
		'a': {"", "a"}, 'Z': {"SHIFT", "z"}, '7': {"", "7"}, ' ': {"", "space"},
		'\n': {"", "Return"}, '\t': {"", "Tab"}, '!': {"SHIFT", "1"}, '_': {"SHIFT", "minus"},
		'.': {"", "period"}, '?': {"SHIFT", "slash"}, '"': {"SHIFT", "apostrophe"},
	}
	for r, want := range cases {
		mods, key, err := KeyFor(r)
		if err != nil || mods != want[0] || key != want[1] {
			t.Errorf("KeyFor(%q) = %q %q %v, want %v", r, mods, key, err, want)
		}
	}
	for _, r := range []rune{'é', '😀', '€'} {
		if _, _, err := KeyFor(r); err == nil {
			t.Errorf("KeyFor(%q): want unsupported_input", r)
		}
	}
}

func TestParseCombo(t *testing.T) {
	cases := map[string][2]string{
		"Return": {"", "Return"}, "ctrl+l": {"CTRL", "l"}, "ctrl+shift+t": {"CTRL SHIFT", "t"},
		"super+alt+F4": {"SUPER ALT", "F4"},
	}
	for in, want := range cases {
		mods, key, err := ParseCombo(in)
		if err != nil || mods != want[0] || key != want[1] {
			t.Errorf("ParseCombo(%q) = %q %q %v", in, mods, key, err)
		}
	}
	for _, bad := range []string{"", "ctrl+", "hyper+x", `x"y`} {
		if _, _, err := ParseCombo(bad); err == nil {
			t.Errorf("ParseCombo(%q): want an error", bad)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/desktop/ -v`
Expected: FAIL, the package does not exist.

- [ ] **Step 3: Implement**

`internal/desktop/desktop.go`:

```go
// Package desktop acts on the human's own windows through Hyprland IPC. It
// is not silent: send_shortcut moves keyboard focus to the target and back
// for every key. The human accepted that on 2026-09-29.
package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hexadecimil/hyprcage/internal/hypr"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

// Locked reports whether hyprlock runs, by the comm of every process under
// procRoot ("/proc" outside tests).
func Locked(procRoot string) bool {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return false
	}
	for _, e := range entries {
		comm, err := os.ReadFile(filepath.Join(procRoot, e.Name(), "comm"))
		if err == nil && string(bytes.TrimSpace(comm)) == "hyprlock" {
			return true
		}
	}
	return false
}

var addrRe = regexp.MustCompile(`^0x[0-9a-f]+$`)

// CheckAddress accepts only a Hyprland window address such as 0x55ebac116320.
// Addresses go into Lua and IPC strings, so anything else is refused.
func CheckAddress(a string) error {
	if !addrRe.MatchString(a) {
		return screen.Errf(screen.CodeAddress, "take the address from desktop_windows", "%q is not a window address", a)
	}
	return nil
}

// ponytail: US layout only. A rune outside this map is refused, not guessed;
// add a layout table when a second keyboard layout is in use.
var usShifted = map[rune]string{
	'!': "1", '@': "2", '#': "3", '$': "4", '%': "5", '^': "6", '&': "7", '*': "8", '(': "9", ')': "0",
	'_': "minus", '+': "equal", '{': "bracketleft", '}': "bracketright", '|': "backslash",
	':': "semicolon", '"': "apostrophe", '<': "comma", '>': "period", '?': "slash", '~': "grave",
}

var usPlain = map[rune]string{
	' ': "space", '\n': "Return", '\t': "Tab", '-': "minus", '=': "equal", '[': "bracketleft",
	']': "bracketright", '\\': "backslash", ';': "semicolon", '\'': "apostrophe", ',': "comma",
	'.': "period", '/': "slash", '`': "grave",
}

// KeyFor maps a rune to the send_shortcut modifiers and key name.
func KeyFor(r rune) (string, string, error) {
	switch {
	case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return "", string(r), nil
	case r >= 'A' && r <= 'Z':
		return "SHIFT", string(r + 'a' - 'A'), nil
	}
	if k, ok := usPlain[r]; ok {
		return "", k, nil
	}
	if k, ok := usShifted[r]; ok {
		return "SHIFT", k, nil
	}
	return "", "", screen.Errf(screen.CodeUnsupported, "type it on an agent screen, or use desktop_key", "cannot type %q on the desktop (US layout only)", r)
}

var keyRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
var modNames = map[string]string{"ctrl": "CTRL", "shift": "SHIFT", "alt": "ALT", "super": "SUPER"}

// ParseCombo turns "ctrl+shift+t" into ("CTRL SHIFT", "t").
func ParseCombo(combo string) (string, string, error) {
	parts := strings.Split(combo, "+")
	key := parts[len(parts)-1]
	if !keyRe.MatchString(key) {
		return "", "", screen.Errf(screen.CodeUnsupported, "", "bad key in %q", combo)
	}
	var mods []string
	for _, p := range parts[:len(parts)-1] {
		m, ok := modNames[strings.ToLower(p)]
		if !ok {
			return "", "", screen.Errf(screen.CodeUnsupported, "ctrl, shift, alt or super", "bad modifier %q in %q", p, combo)
		}
		mods = append(mods, m)
	}
	return strings.Join(mods, " "), key, nil
}

// Desktop acts on the human's Hyprland session.
type Desktop struct {
	H *hypr.Instance
	D hypr.ConfigDriver
}

func (d Desktop) ready(addr string) error {
	if d.H == nil {
		return screen.Errf(screen.CodeHyprland, "", "no Hyprland session")
	}
	if Locked("/proc") {
		return screen.Errf(screen.CodeLocked, "wait until the human unlocks", "the desktop is locked")
	}
	if addr == "" {
		return nil
	}
	return CheckAddress(addr)
}

// Windows lists the human's windows, hyprcage's own mirrors left out.
func (d Desktop) Windows() ([]hypr.Client, error) {
	if err := d.ready(""); err != nil {
		return nil, err
	}
	all, err := d.H.Clients()
	if err != nil {
		return nil, err
	}
	out := []hypr.Client{}
	for _, c := range all {
		if c.Class != "hyprcage-mirror" {
			out = append(out, c)
		}
	}
	return out, nil
}

// Focus gives keyboard focus to a window. This one moves the human's focus
// on purpose.
func (d Desktop) Focus(addr string) error {
	if err := d.ready(addr); err != nil {
		return err
	}
	return d.H.Command(d.D.FocusWindowCmd(addr))
}

// Move sends a window to a workspace without following it.
func (d Desktop) Move(addr string, ws int) error {
	if err := d.ready(addr); err != nil {
		return err
	}
	return d.H.Command(d.D.MoveWindowCmd(addr, ws))
}

// Type sends text to a window key by key. Every rune is mapped first, so a
// rune that cannot be typed sends nothing at all.
func (d Desktop) Type(addr, text string) error {
	if err := d.ready(addr); err != nil {
		return err
	}
	type k struct{ mods, key string }
	var keys []k
	for _, r := range text {
		mods, key, err := KeyFor(r)
		if err != nil {
			return err
		}
		keys = append(keys, k{mods, key})
	}
	for _, x := range keys {
		if err := d.H.Command(d.D.SendShortcutCmd(x.mods, x.key, addr)); err != nil {
			return err
		}
	}
	return nil
}

// Key sends key combinations such as ctrl+l to a window. Every combination
// is parsed first.
func (d Desktop) Key(addr string, combos []string) error {
	if err := d.ready(addr); err != nil {
		return err
	}
	type k struct{ mods, key string }
	var keys []k
	for _, c := range combos {
		mods, key, err := ParseCombo(c)
		if err != nil {
			return err
		}
		keys = append(keys, k{mods, key})
	}
	for _, x := range keys {
		if err := d.H.Command(d.D.SendShortcutCmd(x.mods, x.key, addr)); err != nil {
			return err
		}
	}
	return nil
}
```

In `internal/record/state.go`, replace the `lockedFn` line with:

```go
var lockedFn = func() bool { return desktop.Locked("/proc") }
```

and import `github.com/hexadecimil/hyprcage/internal/desktop`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/desktop/ ./internal/record/ -v && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Add the CLI twin**

Append to `internal/cli/fork.go` (import `strconv`, `strings` and `github.com/hexadecimil/hyprcage/internal/desktop`):

```go
func openDesktop() (desktop.Desktop, error) {
	inst, err := hypr.Discover()
	if err != nil {
		return desktop.Desktop{}, err
	}
	return desktop.Desktop{H: inst, D: inst.Driver()}, nil
}

func runDesktop(e *Env) int {
	fs := e.flags("desktop")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	usage := "usage: hyprcage desktop windows | focus <addr> | move <addr> <ws> | type <addr> <text> | key <addr> <combo>..."
	if fs.NArg() < 1 {
		return e.errorf(usage)
	}
	d, err := openDesktop()
	if err != nil {
		return e.fail(err)
	}
	a := fs.Args()
	switch {
	case a[0] == "windows" && len(a) == 1:
		wins, err := d.Windows()
		if err != nil {
			return e.fail(err)
		}
		return e.printJSON(wins)
	case a[0] == "focus" && len(a) == 2:
		err = d.Focus(a[1])
	case a[0] == "move" && len(a) == 3:
		ws, convErr := strconv.Atoi(a[2])
		if convErr != nil {
			return e.errorf("workspace: expected an integer, got %q", a[2])
		}
		err = d.Move(a[1], ws)
	case a[0] == "type" && len(a) >= 3:
		err = d.Type(a[1], strings.Join(a[2:], " "))
	case a[0] == "key" && len(a) >= 3:
		err = d.Key(a[1], a[2:])
	default:
		return e.errorf(usage)
	}
	if err != nil {
		return e.fail(err)
	}
	return ExitOK
}
```

Add to `commands`: `{"desktop", "act on the human's own windows (not silent)", runDesktop, false},`

In `internal/cli/util.go`, add `screen.CodeLocked` to the `ExitDependency` case, and `screen.CodeAddress, screen.CodeUnsupported` to a new `return ExitUsage` case.

- [ ] **Step 6: Add the MCP tools**

In `server_fork.go`, add the inputs:

```go
type desktopIn struct {
	Address   string   `json:"address,omitempty" jsonschema:"window address from desktop_windows, e.g. 0x55ebac116320"`
	Workspace int      `json:"workspace,omitempty" jsonschema:"target workspace (desktop_move)"`
	Text      string   `json:"text,omitempty" jsonschema:"text to type (desktop_type); US layout characters only"`
	Keys      []string `json:"keys,omitempty" jsonschema:"combinations such as [\"ctrl+l\", \"Return\"] (desktop_key)"`
}
```

Add to `registerFork`:

```go
	tool(s, srv, "desktop_windows", "List the human's own windows (address, class, title, workspace). Use an agent screen for your own apps; these are the human's.", s.desktopWindows)
	tool(s, srv, "desktop_focus", "Give keyboard focus to one of the human's windows. This moves the human's focus: only when the task needs it.", s.desktopFocus)
	tool(s, srv, "desktop_move", "Move one of the human's windows to a workspace, without following it.", s.desktopMove)
	tool(s, srv, "desktop_type", "Type text into one of the human's windows without focusing it. Each key briefly takes the human's keyboard focus and gives it back. US layout characters only.", s.desktopType)
	tool(s, srv, "desktop_key", "Press key combinations in one of the human's windows without focusing it (same focus blip as desktop_type).", s.desktopKey)
```

Add the handlers (import `github.com/hexadecimil/hyprcage/internal/desktop`):

```go
func (s *Server) desktop() (desktop.Desktop, error) {
	c, err := s.hypr()
	if err != nil {
		return desktop.Desktop{}, err
	}
	return desktop.Desktop{H: c.Hypr, D: c.Driver}, nil
}

func (s *Server) desktopWindows(in struct{}) (*mcp.CallToolResult, error) {
	d, err := s.desktop()
	if err != nil {
		return nil, err
	}
	wins, err := d.Windows()
	if err != nil {
		return nil, err
	}
	return textResult(wins), nil
}

func (s *Server) desktopFocus(in desktopIn) (*mcp.CallToolResult, error) {
	d, err := s.desktop()
	if err != nil {
		return nil, err
	}
	if err := d.Focus(in.Address); err != nil {
		return nil, err
	}
	return textResult(map[string]string{"status": "ok"}), nil
}

func (s *Server) desktopMove(in desktopIn) (*mcp.CallToolResult, error) {
	d, err := s.desktop()
	if err != nil {
		return nil, err
	}
	if err := d.Move(in.Address, in.Workspace); err != nil {
		return nil, err
	}
	return textResult(map[string]string{"status": "ok"}), nil
}

func (s *Server) desktopType(in desktopIn) (*mcp.CallToolResult, error) {
	d, err := s.desktop()
	if err != nil {
		return nil, err
	}
	if err := d.Type(in.Address, in.Text); err != nil {
		return nil, err
	}
	return textResult(map[string]string{"status": "ok"}), nil
}

func (s *Server) desktopKey(in desktopIn) (*mcp.CallToolResult, error) {
	d, err := s.desktop()
	if err != nil {
		return nil, err
	}
	if err := d.Key(in.Address, in.Keys); err != nil {
		return nil, err
	}
	return textResult(map[string]string{"status": "ok"}), nil
}
```

Add the five tool names to `want` in `server_test.go`.

- [ ] **Step 7: Run the tests and a live check**

Run: `go test ./... && go vet ./... && make build`

Live check, with the human told first (it blips their focus):

```bash
hyprctl eval 'hl.dispatch(hl.dsp.exec_cmd("kitty --class hc-desk-probe sh -c \"stty -icanon -echo; cat > /tmp/hc-desk.txt\"", { workspace = "10 silent" }))'; sleep 2
addr=$(./hyprcage desktop windows | jq -r '.[]|select(.class=="hc-desk-probe")|.address')
./hyprcage desktop type "$addr" 'Hi! a_b?'
sleep 0.5; cat /tmp/hc-desk.txt; echo
./hyprcage desktop type "$addr" 'é' ; echo "exit=$?"
hyprctl eval "hl.dispatch(hl.dsp.window.close({ window = \"address:$addr\" }))"
```

Expected: `Hi! a_b?`, then `unsupported_input` with `exit=1`, and nothing more in the file.

- [ ] **Step 8: Commit**

```bash
git add internal/desktop internal/record internal/cli internal/mcpserver
git commit -m "feat(desktop): list, focus, move and type into the human's windows"
```

---

### Task 8: Agent Chrome with DevTools

**Files:**
- Create: `internal/browser/browser.go`, `internal/browser/browser_test.go`
- Modify: `internal/cli/fork.go`, `internal/cli/cli.go`, `internal/mcpserver/server_fork.go`, `internal/mcpserver/server_test.go`

**Interfaces:**
- Consumes: `screen.Launch`, `config.Config.BrowserCommand`, `BrowserPort`, the codes.
- Produces:
  - `browser.Find(configured string, lookPath func(string) (string, error)) (string, error)`
  - `browser.Args(bin string, port int, profile, url string) []string`
  - `browser.Ready(baseURL string, timeout time.Duration) (string, error)` returns `webSocketDebuggerUrl`
  - `browser.Open(c *screen.Ctx, rec *registry.Screen, url string) (Info, error)`
  - `browser.Info{Screen, BrowserURL, WS, Profile string; PID, Port int}`
  - CLI `hyprcage browser [screen] [url]`, MCP `browser_open {screen?, url?}`

- [ ] **Step 1: Write the failing test**

`internal/browser/browser_test.go`:

```go
package browser

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFind(t *testing.T) {
	look := func(have ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, h := range have {
				if h == n {
					return "/usr/bin/" + n, nil
				}
			}
			return "", errors.New("no")
		}
	}
	if b, err := Find("", look("chromium", "google-chrome")); err != nil || b != "/usr/bin/google-chrome" {
		t.Errorf("prefer google-chrome: %s %v", b, err)
	}
	if b, err := Find("", look("chromium")); err != nil || b != "/usr/bin/chromium" {
		t.Errorf("fall back to chromium: %s %v", b, err)
	}
	if b, err := Find("brave", look("brave")); err != nil || b != "/usr/bin/brave" {
		t.Errorf("configured wins: %s %v", b, err)
	}
	if _, err := Find("", look()); err == nil {
		t.Error("no browser: want an error")
	}
}

func TestArgs(t *testing.T) {
	a := strings.Join(Args("/usr/bin/google-chrome", 9222, "/tmp/p", "https://x.test"), " ")
	for _, want := range []string{"--ozone-platform=wayland", "--user-data-dir=/tmp/p", "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=9222", "--no-first-run", "https://x.test"} {
		if !strings.Contains(a, want) {
			t.Errorf("argv lacks %s: %s", want, a)
		}
	}
}

func TestReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/browser/abc"}`)
	}))
	defer srv.Close()
	ws, err := Ready(srv.URL, time.Second)
	if err != nil || ws != "ws://127.0.0.1:9222/devtools/browser/abc" {
		t.Fatalf("Ready: %s %v", ws, err)
	}
	if _, err := Ready("http://127.0.0.1:1", 300*time.Millisecond); err == nil {
		t.Error("nothing listens: want a timeout")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/browser/ -v`
Expected: FAIL, the package does not exist.

- [ ] **Step 3: Implement**

`internal/browser/browser.go`:

```go
// Package browser runs the agent's Chrome on a screen with the DevTools
// protocol on 127.0.0.1, for chrome-devtools-mcp --browserUrl to drive.
package browser

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

// Info describes the running agent Chrome.
type Info struct {
	Screen     string `json:"screen"`
	PID        int    `json:"pid"`
	Port       int    `json:"port"`
	BrowserURL string `json:"browser_url"`
	WS         string `json:"ws_endpoint"`
	Profile    string `json:"profile"`
}

var candidates = []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"}

// Find returns the configured browser, else the first Chrome on PATH.
func Find(configured string, lookPath func(string) (string, error)) (string, error) {
	names := candidates
	if configured != "" {
		names = []string{configured}
	}
	for _, n := range names {
		if p, err := lookPath(n); err == nil {
			return p, nil
		}
	}
	return "", screen.Errf(screen.CodeBrowserDown, "set browser.command in config.toml", "no Chrome found (%v)", names)
}

// Args is the Chrome command line: Wayland, a throwaway profile, DevTools
// on 127.0.0.1 only.
func Args(bin string, port int, profile, url string) []string {
	a := []string{bin, "--ozone-platform=wayland", "--user-data-dir=" + profile, "--no-first-run",
		"--no-default-browser-check", "--remote-debugging-address=127.0.0.1", fmt.Sprintf("--remote-debugging-port=%d", port)}
	if url != "" {
		a = append(a, url)
	}
	return a
}

// Ready polls baseURL/json/version until it answers, and returns the
// browser's WebSocket endpoint.
func Ready(baseURL string, timeout time.Duration) (string, error) {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(timeout)
	for {
		resp, err := client.Get(baseURL + "/json/version")
		if err == nil {
			var v struct {
				WS string `json:"webSocketDebuggerUrl"`
			}
			err = json.NewDecoder(resp.Body).Decode(&v)
			resp.Body.Close()
			if err == nil && v.WS != "" {
				return v.WS, nil
			}
		}
		if time.Now().After(deadline) {
			return "", screen.Errf(screen.CodeTimeout, "", "no DevTools endpoint at %s within %s", baseURL, timeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// Open starts the agent Chrome on rec. A port that already answers means a
// browser is there, the agent's or the human's: it is refused, never shared.
func Open(c *screen.Ctx, rec *registry.Screen, url string) (Info, error) {
	base := fmt.Sprintf("http://127.0.0.1:%d", c.Cfg.BrowserPort)
	if _, err := Ready(base, 300*time.Millisecond); err == nil {
		return Info{}, screen.Errf(screen.CodeBrowserBusy, "reuse it through the agent-chrome tools, or destroy its screen",
			"something already serves DevTools on %s", base)
	}
	bin, err := Find(c.Cfg.BrowserCommand, exec.LookPath)
	if err != nil {
		return Info{}, err
	}
	profile, err := os.MkdirTemp("", "hc-chrome-")
	if err != nil {
		return Info{}, err
	}
	pid, _, err := screen.Launch(c, rec, Args(bin, c.Cfg.BrowserPort, profile, url), "", nil)
	if err != nil {
		return Info{}, err
	}
	ws, err := Ready(base, 20*time.Second)
	if err != nil {
		return Info{}, err
	}
	return Info{Screen: rec.Name, PID: pid, Port: c.Cfg.BrowserPort, BrowserURL: base, WS: ws, Profile: profile}, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/browser/ -v && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Add the CLI twin and the MCP tool**

Append to `internal/cli/fork.go` (import `github.com/hexadecimil/hyprcage/internal/browser`):

```go
func runBrowser(e *Env) int {
	fs := e.flags("browser")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 2 {
		return e.errorf("usage: hyprcage browser [screen] [url]")
	}
	cfg, err := config.Load()
	if err != nil {
		return e.fail(err)
	}
	c, err := screen.Connect(cfg)
	if err != nil {
		return e.fail(err)
	}
	rec, err := screen.Resolve(c, fs.Arg(0), session.Current())
	if err != nil {
		return e.fail(err)
	}
	info, err := browser.Open(c, rec, fs.Arg(1))
	if err != nil {
		return e.fail(err)
	}
	return e.printJSON(info)
}
```

Add to `commands`: `{"browser", "open the agent's Chrome with DevTools on a screen", runBrowser, false},`
Add `screen.CodeBrowserBusy, screen.CodeBrowserDown` to the `ExitDependency` case in `util.go`.

In `server_fork.go`:

```go
type browserIn struct {
	Screen string `json:"screen,omitempty" jsonschema:"screen name; optional when the session owns exactly one screen"`
	URL    string `json:"url,omitempty" jsonschema:"page to open"`
}
```

Add to `registerFork`:

```go
	tool(s, srv, "browser_open", "Open the agent's Chrome on a screen with DevTools on 127.0.0.1:9222 and a throwaway profile. Then drive it with the agent-chrome MCP tools (DOM, console, network, JavaScript). One agent Chrome at a time; it closes with its screen.", s.browserOpen)
```

Handler (import `github.com/hexadecimil/hyprcage/internal/browser`):

```go
func (s *Server) browserOpen(in browserIn) (*mcp.CallToolResult, error) {
	rec, _, err := s.resolve(in.Screen, false)
	if err != nil {
		return nil, err
	}
	info, err := browser.Open(s.ctx, rec, in.URL)
	if err != nil {
		return nil, err
	}
	return textResult(info), nil
}
```

Add `"browser_open"` to `want` in `server_test.go`.

- [ ] **Step 6: Run the tests and a live check**

Run: `go test ./... && go vet ./... && make build`

```bash
./hyprcage create -name web -mirror
./hyprcage browser hc-web 'data:text/html,<title>hc-ok</title>'
curl -s http://127.0.0.1:9222/json | jq -r '.[]|select(.type=="page")|.title'
ss -ltnp | grep 9222
./hyprcage browser hc-web ; echo "exit=$?"
./hyprcage destroy hc-web
curl -s --max-time 1 http://127.0.0.1:9222/json/version ; echo "after=$?"
```

Expected: `hc-ok`; the listener only on `127.0.0.1:9222`; the second call fails with `browser_running`, `exit=4`; after destroy, curl fails (`after=7`).

- [ ] **Step 7: Register the Chrome MCP for Claude Code**

Run: `claude mcp add --scope user agent-chrome -- npx -y chrome-devtools-mcp@latest --browserUrl http://127.0.0.1:9222`
Then check with `claude mcp list | grep agent-chrome`. The server shows as failing while no agent Chrome runs. That is expected: it connects on its first tool call.

To verify: whether `chrome-devtools-mcp` connects lazily on the first call or only at start. If it connects only at start, note it in `contrib/README.md` (Task 9) and in the skill (Task 10): "call `browser_open` before the first agent-chrome tool, then run `/mcp` reconnect".

- [ ] **Step 8: Commit**

```bash
git add internal/browser internal/cli internal/mcpserver
git commit -m "feat(browser): open the agent's Chrome with DevTools on a screen"
```

---

### Task 9: `show` command and the waybar menu

**Files:**
- Modify: `internal/cli/fork.go`, `internal/cli/cli.go`
- Create: `contrib/waybar/hyprcage-agents`, `contrib/waybar/hyprcage-menu`, `contrib/README.md`

**Interfaces:**
- Consumes: `registry.Screen.WorkspaceMirror`, `ConfigDriver.WorkspaceCmd`, `hyprcage list --all --json`, `hyprcage record status --json`.
- Produces: `hyprcage show <screen>`, which switches the human to the screen's mirror workspace. Two stand-alone scripts.

- [ ] **Step 1: Add `show`**

Append to `internal/cli/fork.go`:

```go
// runShow switches the human to a screen's mirror workspace. It is the
// only command that moves the human on purpose: they asked for it.
func runShow(e *Env) int {
	if len(e.Args) != 1 {
		return e.errorf("usage: hyprcage show <screen>")
	}
	rec, err := registry.Load(e.Args[0])
	if err != nil {
		return e.fail(err)
	}
	if rec.WorkspaceMirror == 0 {
		return e.errorf("%s has no mirror window (%s); open one with: hyprcage mirror %s", rec.Name, rec.MirrorNote, rec.Name)
	}
	inst, err := hypr.Discover()
	if err != nil {
		return e.fail(err)
	}
	if err := inst.Command(inst.Driver().WorkspaceCmd(rec.WorkspaceMirror)); err != nil {
		return e.fail(err)
	}
	return ExitOK
}
```

Add to `commands`: `{"show", "switch to the workspace of a screen's mirror", runShow, false},`

- [ ] **Step 2: Build and check `show`**

Run: `go vet ./... && make build && ./hyprcage create -name show -mirror && ./hyprcage show hc-show; hyprctl activeworkspace -j | jq .id; ./hyprcage destroy hc-show`
Expected: the mirror workspace number (6 by default). Switch back with the usual binding afterwards.

- [ ] **Step 3: Write the waybar module**

`contrib/waybar/hyprcage-agents` (mode 755):

```bash
#!/usr/bin/env bash
# Waybar module: the number of agent screens, a dot while anything records,
# and the class recording-desktop while the human's desktop is recorded.
set -uo pipefail
screens=$(hyprcage list --all --json 2>/dev/null) || screens='[]'
recs=$(hyprcage record status --json 2>/dev/null) || recs='[]'
n=$(jq length <<<"$screens")
r=$(jq length <<<"$recs")
desk=$(jq '[.[] | select(.target == "desktop")] | length' <<<"$recs")
if [ "$n" -eq 0 ] && [ "$r" -eq 0 ]; then
  echo '{"text": "", "class": "idle"}'
  exit 0
fi
text="󰍹 $n"
class=active
[ "$r" -gt 0 ] && text="$text ●"
[ "$desk" -gt 0 ] && class=recording-desktop
tip=$(jq -r '[.[] | "\(.name)  ws \(.ws_mirror)"] | join("\n")' <<<"$screens")
jq -cn --arg t "$text" --arg c "$class" --arg tip "$tip" '{text: $t, class: $c, tooltip: $tip}'
```

The glyph `󰍹` is U+F0379 (monitor). Keep it as a literal UTF-8 character: bash `$'\u…'` takes only 4 hex digits.

`contrib/waybar/hyprcage-menu` (mode 755):

```bash
#!/usr/bin/env bash
# Click action of the waybar module: pick an agent screen, jump to its mirror.
set -euo pipefail
names=$(hyprcage list --all --json | jq -r '.[] | select(.ws_mirror > 0) | .name')
[ -n "$names" ] || exit 0
if command -v rofi >/dev/null; then pick=$(rofi -dmenu -p agents <<<"$names")
elif command -v wofi >/dev/null; then pick=$(wofi --dmenu -p agents <<<"$names")
elif command -v fuzzel >/dev/null; then pick=$(fuzzel --dmenu <<<"$names")
else echo "hyprcage-menu: no rofi, wofi or fuzzel" >&2; exit 1; fi
[ -n "$pick" ] && exec hyprcage show "$pick"
```

- [ ] **Step 4: Write `contrib/README.md`**

```markdown
# Desktop integration (mrwick1 fork)

These files are optional. Each machine links them in from its own configuration.

## Waybar

1. Link the scripts into your `PATH`:

   ```sh
   ln -s "$PWD/contrib/waybar/hyprcage-agents" ~/.local/bin/
   ln -s "$PWD/contrib/waybar/hyprcage-menu" ~/.local/bin/
   ```

2. Add the module to `~/.config/waybar/config.jsonc`, and add `"custom/agents"` to a modules list:

   ```jsonc
   "custom/agents": {
     "exec": "hyprcage-agents",
     "return-type": "json",
     "interval": 2,
     "on-click": "hyprcage-menu"
   }
   ```

3. Add the style to `~/.config/waybar/style.css`:

   ```css
   #custom-agents { padding: 0 8px; }
   #custom-agents.recording-desktop { color: #ef5f6b; }
   ```

## Chrome control

Register the Chrome DevTools MCP once per machine:

```sh
claude mcp add --scope user agent-chrome -- npx -y chrome-devtools-mcp@latest --browserUrl http://127.0.0.1:9222
```
```

- [ ] **Step 5: Wire it on `arjun-e16` and check it**

Do steps 1–3 of `contrib/README.md` on this machine. Put `"custom/agents"` just before `custom/wispr` in `modules-right`. Restart waybar the way the machine does it (check `~/.config/hypr/autostart.lua` for the command). Then create a mirrored screen and confirm that the bar shows `󰍹 1`. Take a screenshot of the bar area with `grim -g "1500,0 420x30"` and look at it. Click the module and confirm that the menu lists the screen, and that choosing it switches to workspace 6.

- [ ] **Step 6: Commit**

```bash
git add internal/cli contrib
git commit -m "feat(waybar): list agent screens and recordings in the bar"
```

---

### Task 10: Teach the agent the new tools

**Files:**
- Modify: `skills/hyprcage/SKILL.md`
- Modify: `internal/mcpserver/server.go` (`instructions`)
- Modify: `README.md` (a short "Fork additions" section)

**Interfaces:**
- Consumes: every tool name from Tasks 4, 5, 7 and 8.
- Produces: documentation only.

- [ ] **Step 1: Extend `instructions` in `server.go`**

Append two sentences to the `instructions` constant:

```go
Recording: record_start / record_stop (a screen, or target "desktop"). Browser: browser_open, then the agent-chrome MCP tools. Clipboard: clipboard_get / clipboard_set (the screen's, not the human's).
The human's own windows: desktop_windows, desktop_focus, desktop_move, desktop_type, desktop_key. They are not silent (each key briefly takes the human's focus) and refuse while the session is locked; prefer an agent screen whenever the task allows it.
```

- [ ] **Step 2: Extend the skill**

In `skills/hyprcage/SKILL.md`, add after the "How (MCP tools)" list:

```markdown
## More tools (mrwick1 fork)

- **Record**: `record_start` on a screen, or `target: "desktop"` for the human's screen, then `record_stop`. The reply gives the MP4 path in `~/Videos/agent/`. A recording stops by itself after 30 minutes or when its screen closes. Tell the human when you record their desktop.
- **Chrome by code**: `browser_open` on a screen, then the `agent-chrome` MCP tools (DOM, console, network, JavaScript). One agent Chrome at a time. `browser_running` means one is already open: reuse it.
- **Clipboard**: `clipboard_set`, then `key ctrl+v`, to paste long text into an app. `clipboard_get` reads what the app copied. It is the screen's clipboard, never the human's.
- **The human's windows**: `desktop_windows` gives the addresses. `desktop_type` and `desktop_key` send keys without focusing the window, but each key briefly takes the human's keyboard focus. `desktop_focus` moves their focus for real. Use these only for the human's own windows; your own apps go on a screen.
- **Files**: none needed. Apps on a screen run as the human and see the same file system, so give them normal paths.
```

- [ ] **Step 3: Add the README section**

Append to `README.md`:

```markdown
## Fork additions (mrwick1/hyprcage)

This fork adds recording (`record`), the agent's Chrome with DevTools (`browser`), the clipboard of a screen (`clip`), control of the human's own windows (`desktop`), `show`, a waybar module, and openSUSE support in the installer. See `contrib/README.md` and `docs/superpowers/specs/2026-09-29-hyprcage-fork-design.md`.
```

- [ ] **Step 4: Run the tests**

Run: `go test ./... && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add skills README.md internal/mcpserver/server.go
git commit -m "docs: teach the agent the fork's tools"
```

---

### Task 11: Smoke test on both machines and the browser E2E

**Files:**
- Create: `tests/smoke.sh`

**Interfaces:**
- Consumes: the CLI twins `create`, `browser`, `click`, `type`, `wait`, `record`, `clip`, `destroy`.
- Produces: a script that prints `SMOKE OK` or exits non-zero with `FAIL: <step>`.

- [ ] **Step 1: Write the smoke script**

`tests/smoke.sh` (mode 755):

```bash
#!/usr/bin/env bash
# Smoke test of the fork on a live Hyprland session: a screen, the agent
# Chrome, input, recording (also through destroy), clipboard, teardown.
set -euo pipefail
HC=${HC:-hyprcage}
S=hc-smoke
fail() { echo "FAIL: $*" >&2; exit 1; }
trap '$HC destroy $S >/dev/null 2>&1 || true' EXIT

$HC create -name smoke -no-mirror >/dev/null || fail "create"
page=$(mktemp --suffix=.html)
cat >"$page" <<'EOF'
<textarea autofocus oninput="document.title = this.value"></textarea>
EOF
$HC browser $S "file://$page" >/dev/null || fail "browser"
$HC wait $S --stable 500 --timeout 15000 >/dev/null || fail "page did not settle"
$HC click $S 90 20 >/dev/null
$HC type $S 'smoke ok' >/dev/null
sleep 1
title=$(curl -sf http://127.0.0.1:9222/json | jq -r '[.[] | select(.type == "page")][0].title')
[ "$title" = "smoke ok" ] || fail "typed text: got title '$title'"

$HC record start $S >/dev/null || fail "record start"
sleep 5
out=$($HC record stop $S) || fail "record stop"
frames=$(ffprobe -v error -count_frames -select_streams v -show_entries stream=nb_read_frames -of csv=p=0 "$out")
[ "${frames:-0}" -ge 30 ] || fail "recording has $frames frames"

$HC clip set $S 'smoke-clip' || fail "clip set"
[ "$($HC clip get $S)" = smoke-clip ] || fail "clip get"

# Review focus 3: destroying a recording screen still leaves a playable file.
$HC record start $S >/dev/null || fail "record start 2"
sleep 3
out2=$($HC record status --json | jq -r '.[] | select(.target == "'$S'") | .path')
$HC destroy $S >/dev/null || fail "destroy"
sleep 2
ffprobe -v error "$out2" || fail "recording cut by destroy is not playable"

left=$(grep -l "HYPRCAGE_SCREEN=$S" /proc/[0-9]*/environ 2>/dev/null | wc -l)
[ "$left" -eq 0 ] || fail "$left processes of $S remain"
curl -sf --max-time 1 http://127.0.0.1:9222/json/version >/dev/null && fail "Chrome outlived its screen"
rm -f "$page" "$out" "$out2"
echo "SMOKE OK"
```

- [ ] **Step 2: Run it on `arjun-e16`**

Run: `make build && HC=./hyprcage tests/smoke.sh`
Expected: `SMOKE OK`. Fix any failing step in the task that owns it, then rerun.

- [ ] **Step 3: Commit**

```bash
git add tests/smoke.sh
git commit -m "test: add the smoke test for the fork"
```

- [ ] **Step 4: Push and install on `archMachine`**

```bash
git push origin HEAD
timeout 300 ssh -o BatchMode=yes -o ConnectTimeout=8 mrwick@archmachine '
  set -e
  mkdir -p ~/coding/personal/projects && cd ~/coding/personal/projects
  [ -d hyprcage ] || git clone https://github.com/mrwick1/hyprcage
  cd hyprcage && git fetch -q origin && git checkout -q '"$(git branch --show-current)"' && git pull -q
  pacman -Si cage wl-clipboard ffmpeg >/dev/null && echo REPOS_OK
  make build'
```

Expected: `REPOS_OK` (this settles the "to verify" item about the Arch repositories), then a clean build. Ask Arjun to run `sudo pacman -S --needed cage wl-clipboard` on archMachine if they are missing. The Bash tool cannot answer a sudo prompt there.

- [ ] **Step 5: Run the smoke test on `archMachine`**

The test needs the live Hyprland session, so run it from a terminal there. Ask Arjun to run `cd ~/coding/personal/projects/hyprcage && HC=./hyprcage tests/smoke.sh`, or run it over SSH with the session's `XDG_RUNTIME_DIR=/run/user/$(id -u)` and `HYPRLAND_INSTANCE_SIGNATURE` exported.
Expected: `SMOKE OK`.

- [ ] **Step 6: Browser E2E with Arjun watching**

1. Install the binary: `install -Dm755 hyprcage ~/.local/bin/hyprcage`. Register it with `claude mcp add --scope user hyprcage -- ~/.local/bin/hyprcage mcp`, and run `hyprcage config` to write the configuration.
2. In a fresh Claude Code session, call `screen_create` with `mirror: true`. Tell Arjun to switch to workspace 6, or to use the waybar menu.
3. Call `browser_open` with a real local project page. Use the `agent-chrome` tools to read the DOM title and the console messages, and take a screenshot with `screenshot`.
4. Call `record_start`, click through the page for 10 s, then `record_stop`. Play the file with `ffprobe` and report the frame count.
5. Call `screen_destroy`. Confirm that the waybar module goes idle.

Report each step's result to Arjun with the evidence (titles, frame counts, screenshot).

- [ ] **Step 7: Open the PR**

```bash
gh auth switch --user mrwick1
gh pr create --repo mrwick1/hyprcage --base main --title "Fork additions: recording, agent Chrome, clipboard, desktop control, waybar" --body-file <(cat <<'EOF'
Adds recording, the agent Chrome with DevTools, the clipboard of a screen, control of the human's own windows, `show`, a waybar module and openSUSE support.

- Spec: docs/superpowers/specs/2026-09-29-hyprcage-fork-design.md
- Plan: docs/superpowers/plans/2026-09-29-hyprcage-fork.md
- Smoke test: `tests/smoke.sh` passes on arjun-e16 (openSUSE) and archMachine (Arch).
EOF
)
```

Link the PR with the t3-code `link_pull_request` tool.
