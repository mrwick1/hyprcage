package desktop

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hexadecimil/hyprcage/internal/hypr"
	"github.com/hexadecimil/hyprcage/internal/screen"
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

// deadDesktop points at a socket that does not exist, so any command that
// reaches Hyprland fails with hyprland_unreachable.
func deadDesktop(t *testing.T, locked bool) Desktop {
	t.Setenv("HYPRCAGE_DRIVER", "lua")
	old := lockedFn
	lockedFn = func() bool { return locked }
	t.Cleanup(func() { lockedFn = old })
	h := &hypr.Instance{Signature: "none", Dir: t.TempDir()}
	return Desktop{H: h, D: h.Driver()}
}

func wantCode(t *testing.T, what string, err error, code screen.Code) {
	t.Helper()
	var se *screen.Error
	if !errors.As(err, &se) || se.Code != code {
		t.Errorf("%s: got %v, want %s", what, err, code)
	}
}

func TestNothingReachesHyprland(t *testing.T) {
	d := deadDesktop(t, false)
	for _, a := range []string{"", "0xZZ", `0x1"`} {
		wantCode(t, "focus "+a, d.Focus(a), screen.CodeAddress)
		wantCode(t, "move "+a, d.Move(a, 2), screen.CodeAddress)
		wantCode(t, "type "+a, d.Type(a, "hi"), screen.CodeAddress)
		wantCode(t, "key "+a, d.Key(a, []string{"Return"}), screen.CodeAddress)
	}
	wantCode(t, "type aé", d.Type("0xabc", "aé"), screen.CodeUnsupported)
	wantCode(t, "key bad", d.Key("0xabc", []string{"Return", "hyper+x"}), screen.CodeUnsupported)
	wantCode(t, "move ws 0", d.Move("0xabc", 0), screen.CodeUnsupported)
	wantCode(t, "move ws -1", d.Move("0xabc", -1), screen.CodeUnsupported)
	// A valid call does reach the dead socket, which proves the check above.
	wantCode(t, "focus valid", d.Focus("0xabc"), screen.CodeHyprland)
	_, err := d.Windows()
	wantCode(t, "windows", err, screen.CodeHyprland)
}

func TestLockedRefusesAll(t *testing.T) {
	d := deadDesktop(t, true)
	_, err := d.Windows()
	wantCode(t, "windows", err, screen.CodeLocked)
	wantCode(t, "focus", d.Focus("0xabc"), screen.CodeLocked)
	wantCode(t, "move", d.Move("0xabc", 2), screen.CodeLocked)
	wantCode(t, "type", d.Type("0xabc", "a"), screen.CodeLocked)
	wantCode(t, "key", d.Key("0xabc", []string{"a"}), screen.CodeLocked)
}

func TestPartialTypeCountsKeys(t *testing.T) {
	d := deadDesktop(t, false)
	err := d.Type("0xabc", "ab")
	wantCode(t, "type", err, screen.CodeHyprland)
	if err == nil || !strings.Contains(err.Error(), "0 of 2 keys sent") {
		t.Errorf("type: %v, want the sent count", err)
	}
}

// fakeInstance serves a Hyprland command socket that answers every request,
// under $XDG_RUNTIME_DIR/hypr/<sig>.
func fakeInstance(t *testing.T, sig string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "hypr", sig)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", filepath.Join(dir, ".socket.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 256)
			_, _ = c.Read(buf)
			_, _ = io.WriteString(c, "Hyprland 0.0")
			c.Close()
		}
	}()
}

func TestInstanceOverride(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	fakeInstance(t, "nested")
	fallback := &hypr.Instance{Signature: "human"}
	t.Setenv("HYPRCAGE_DESKTOP_INSTANCE", "nested")
	inst, err := Instance(fallback)
	if err != nil || inst.Signature != "nested" || inst.Dir != filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "hypr", "nested") {
		t.Fatalf("got %+v %v, want the nested instance", inst, err)
	}
	for _, bad := range []string{"../nested", "a/b", "..", "gone"} {
		t.Setenv("HYPRCAGE_DESKTOP_INSTANCE", bad)
		if inst, err := Instance(fallback); err == nil {
			t.Errorf("%q: got %+v, want an error", bad, inst)
		}
	}
}

func TestInstanceFallback(t *testing.T) {
	t.Setenv("HYPRCAGE_DESKTOP_INSTANCE", "")
	fallback := &hypr.Instance{Signature: "human"}
	if inst, err := Instance(fallback); err != nil || inst != fallback {
		t.Fatalf("got %+v %v, want the fallback", inst, err)
	}
	if !IsDesktop(Name) || IsDesktop("hc-1") || IsDesktop("") {
		t.Error("IsDesktop")
	}
}

// fakeDriver records the Exec calls and builds recognisable commands.
type fakeDriver struct {
	hypr.ConfigDriver
	cmds  []string
	rules []hypr.ExecRules
}

func (f *fakeDriver) Exec(command string, rules hypr.ExecRules) error {
	f.cmds = append(f.cmds, command)
	f.rules = append(f.rules, rules)
	return nil
}

func (f *fakeDriver) WorkspaceCmd(id int) string { return fmt.Sprintf("dispatch workspace %d", id) }

func (f *fakeDriver) MoveCursorCmd(x, y int) string {
	return fmt.Sprintf("dispatch movecursor %d %d", x, y)
}

func (f *fakeDriver) MoveWindowCmd(addr string, ws int) string {
	return fmt.Sprintf("dispatch movetoworkspacesilent %d,address:%s", ws, addr)
}

// fakeIPC serves a command socket that answers the n-th j/clients with
// clients[n] (the last one repeats, none: no window) and anything else with
// "ok", and records every request.
func fakeIPC(t *testing.T, clients ...string) (*hypr.Instance, *[]string) {
	t.Helper()
	return fakeIPCFunc(t, func(req string) string {
		if req != "j/clients" {
			return "ok"
		}
		reply := "[]"
		if len(clients) > 0 {
			reply = clients[0]
			if len(clients) > 1 {
				clients = clients[1:]
			}
		}
		return reply
	})
}

// fakeIPCFunc serves a command socket that answers each request with
// reply(request) and records every request.
func fakeIPCFunc(t *testing.T, reply func(string) string) (*hypr.Instance, *[]string) {
	t.Helper()
	dir := t.TempDir()
	l, err := net.Listen("unix", filepath.Join(dir, ".socket.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	var mu sync.Mutex
	reqs := []string{}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 4096)
			n, _ := c.Read(buf)
			req := string(buf[:n])
			mu.Lock()
			reqs = append(reqs, req)
			out := reply(req)
			mu.Unlock()
			_, _ = io.WriteString(c, out)
			c.Close()
		}
	}()
	return &hypr.Instance{Signature: "fake", Dir: dir}, &reqs
}

func fakeDesktop(t *testing.T, locked bool, clients ...string) (Desktop, *fakeDriver, *[]string) {
	old := lockedFn
	lockedFn = func() bool { return locked }
	t.Cleanup(func() { lockedFn = old })
	h, reqs := fakeIPC(t, clients...)
	f := &fakeDriver{}
	return Desktop{H: h, D: f}, f, reqs
}

func TestLaunchQuotesArgv(t *testing.T) {
	d, f, _ := fakeDesktop(t, false)
	pid, addr, _, err := d.Launch([]string{"echo", "a b;rm -rf x", "it's"}, map[string]string{"K": "v w"}, "/tmp/my dir", 4, 0)
	if err != nil || pid != 0 || addr != "" {
		t.Fatalf("got %d %q %v, want no window and no error", pid, addr, err)
	}
	want := `cd '/tmp/my dir' && env K='v w' echo 'a b;rm -rf x' 'it'\''s'`
	if len(f.cmds) != 1 || f.cmds[0] != want {
		t.Errorf("command %q, want %q", f.cmds, want)
	}
	_, _, _, err = d.Launch([]string{"x"}, map[string]string{"A=B;": "v"}, "", 4, 0)
	wantCode(t, "bad env key", err, screen.CodeUnsupported)
	_, _, _, err = d.Launch(nil, nil, "", 4, 0)
	wantCode(t, "empty argv", err, screen.CodeUnsupported)
	if len(f.cmds) != 1 {
		t.Errorf("a refused launch reached Exec: %q", f.cmds)
	}
}

func TestLaunchSilentWorkspace(t *testing.T) {
	d, f, _ := fakeDesktop(t, false)
	if _, _, _, err := d.Launch([]string{"thunar"}, nil, "", 4, 0); err != nil {
		t.Fatal(err)
	}
	if len(f.rules) != 1 || f.rules[0] != (hypr.ExecRules{Workspace: "4 silent", NoInitialFocus: true}) {
		t.Errorf("rules %+v", f.rules)
	}
	d, f, _ = fakeDesktop(t, true)
	_, _, _, err := d.Launch([]string{"thunar"}, nil, "", 4, 0)
	wantCode(t, "locked launch", err, screen.CodeLocked)
	if len(f.cmds) != 0 {
		t.Errorf("a locked launch reached Exec")
	}
}

func TestWorkspaceCommand(t *testing.T) {
	d, _, reqs := fakeDesktop(t, false)
	if err := d.Workspace(2); err != nil {
		t.Fatal(err)
	}
	wantCode(t, "workspace 0", d.Workspace(0), screen.CodeUnsupported)
	if len(*reqs) != 1 || (*reqs)[0] != "dispatch workspace 2" {
		t.Errorf("requests %q", *reqs)
	}
	d, _, _ = fakeDesktop(t, true)
	wantCode(t, "locked workspace", d.Workspace(2), screen.CodeLocked)
}

// A single-instance app opens its window in its running process: the exec
// rules miss it, so Launch moves it to the launch workspace.
func TestLaunchSingleInstanceMoves(t *testing.T) {
	d, _, reqs := fakeDesktop(t, false,
		`[{"address":"0xa","pid":7,"workspace":{"id":1}}]`,
		`[{"address":"0xa","pid":7,"workspace":{"id":1}},{"address":"0xb","pid":7,"workspace":{"id":1}}]`)
	pid, addr, byWindow, err := d.Launch([]string{"thunar"}, nil, "", 4, time.Second)
	if err != nil || pid != 7 || addr != "0xb" || !byWindow {
		t.Fatalf("got %d %q %v %v, want 7 0xb matched by window", pid, addr, byWindow, err)
	}
	if !slices.Contains(*reqs, "dispatch movetoworkspacesilent 4,address:0xb") {
		t.Errorf("requests %q, want the silent move", *reqs)
	}
}

// A mirror window that opens during the launch is hyprcage's, not the app's.
func TestLaunchSkipsMirror(t *testing.T) {
	d, _, _ := fakeDesktop(t, false,
		`[]`,
		`[{"address":"0xm","pid":9,"class":"hyprcage-mirror","workspace":{"id":2}},{"address":"0xb","pid":7,"workspace":{"id":4}}]`)
	pid, addr, _, err := d.Launch([]string{"thunar"}, nil, "", 4, time.Second)
	if err != nil || pid != 7 || addr != "0xb" {
		t.Fatalf("got %d %q %v, want 7 0xb", pid, addr, err)
	}
}
