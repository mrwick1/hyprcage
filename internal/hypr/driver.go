package hypr

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ExecRules are the window rules applied, by pid, to the windows of a
// command launched through Hyprland's exec (cahier §4.2 step 6). Rule names
// are snake_case in Hyprland ≥ 0.56 for both configuration modes.
type ExecRules struct {
	Workspace      string // e.g. "11 silent"
	Fullscreen     bool
	NoInitialFocus bool
	NoAnim         bool
}

// ConfigDriver abstracts the two configuration modes of Hyprland: classic
// (hyprland.conf, `hyprctl keyword`) and Lua (hyprland.lua, `hyprctl eval`).
// This is what keeps hyprcage generic across Arch + Hyprland setups instead
// of being tied to one distribution (cahier §2.2.6).
type ConfigDriver interface {
	Mode() string
	DeclareMonitor(name string, width, height, hz, x, y int, scale float64) error
	WorkspaceRule(workspace int, monitor string) error
	// WorkspaceGapless removes gaps, borders, rounding and decoration on a
	// workspace without pinning it to a monitor (used for the mirror).
	WorkspaceGapless(workspace int) error
	Exec(command string, rules ExecRules) error
	// FocusMonitorCmd, WorkspaceCmd and MoveCursorCmd return the raw IPC
	// request for a dispatcher in the dialect of the mode: `dispatch` takes
	// classic arguments in classic mode and a Lua expression in Lua mode
	// (verified on 0.56.2: "dispatch focusmonitor X" is a Lua syntax error
	// there). They are strings so that several can go into one Batch.
	FocusMonitorCmd(name string) string
	WorkspaceCmd(id int) string
	MoveCursorCmd(x, y int) string
	// SendShortcutCmd, FocusWindowCmd and MoveWindowCmd act on one of the
	// human's windows by address. send_shortcut moves keyboard focus to the
	// window and back for each key (verified in the 0.56.2 source), so the
	// human's window sees a leave/enter pair.
	SendShortcutCmd(mods, key, address string) string
	FocusWindowCmd(address string) string
	MoveWindowCmd(address string, workspace int) string
}

// Driver detects the configuration mode: HYPRCAGE_DRIVER=lua|classic wins;
// otherwise a Lua REPL probe decides (`repl return 1` answers "1" only when
// the Lua parser is active; verified on 0.56.2 in both modes).
func (i *Instance) Driver() ConfigDriver {
	switch strings.ToLower(os.Getenv("HYPRCAGE_DRIVER")) {
	case "lua":
		return &luaDriver{i}
	case "classic":
		return &classicDriver{i}
	}
	if out, err := i.Request("repl return 1"); err == nil && strings.TrimSpace(out) == "1" {
		return &luaDriver{i}
	}
	return &classicDriver{i}
}

// --- Lua -------------------------------------------------------------------

type luaDriver struct{ i *Instance }

func (d *luaDriver) Mode() string { return "lua" }

// eval runs Lua through `hyprctl eval`. Hyprland's reply format for eval is
// not documented; a reply that reads like a Lua error is treated as one.
func (d *luaDriver) eval(code string) error {
	out, err := d.i.Request("eval " + code)
	if err != nil {
		return err
	}
	if looksLikeLuaError(out) {
		return fmt.Errorf("hyprland eval failed: %s", out)
	}
	return nil
}

func looksLikeLuaError(out string) bool {
	l := strings.ToLower(out)
	return strings.Contains(l, "error") || strings.Contains(l, "traceback") || strings.Contains(l, "attempt to")
}

func (d *luaDriver) DeclareMonitor(name string, width, height, hz, x, y int, scale float64) error {
	return d.eval(fmt.Sprintf(`hl.monitor({ output = %s, mode = "%dx%d@%d", position = "%dx%d", scale = %s })`,
		LuaString(name), width, height, hz, x, y, formatScale(scale)))
}

// The workspace rule pins the workspace to the output and removes gaps,
// borders, rounding and decorations so that the single tiled cage window
// covers the output exactly (exec rules `fullscreen` / `fullscreen_state`
// are ignored on 0.56.2). The Lua spec uses snake_case keys (`gaps_in`,
// `border_size`, `no_rounding`); the classic spellings are rejected with
// "unknown field" (verified on 0.56.2 in Lua mode).
func (d *luaDriver) WorkspaceRule(workspace int, monitor string) error {
	return d.eval(luaWorkspaceRule(workspace, monitor))
}

func (d *luaDriver) WorkspaceGapless(workspace int) error {
	return d.eval(luaWorkspaceRule(workspace, ""))
}

// luaWorkspaceRule builds the hl.workspace_rule call; an empty monitor
// leaves the workspace unpinned (the mirror workspace).
func luaWorkspaceRule(workspace int, monitor string) string {
	pin := ""
	if monitor != "" {
		pin = "monitor = " + LuaString(monitor) + ", default = true, "
	}
	return fmt.Sprintf(`hl.workspace_rule({ workspace = "%d", %sgaps_in = 0, gaps_out = 0, border_size = 0, no_rounding = true, decorate = false })`, workspace, pin)
}

func (d *luaDriver) Exec(command string, rules ExecRules) error {
	return d.eval(luaExecCode(command, rules))
}

func (d *luaDriver) FocusMonitorCmd(name string) string {
	return "dispatch hl.dsp.focus({ monitor = " + LuaString(name) + " })"
}

func (d *luaDriver) WorkspaceCmd(id int) string {
	return fmt.Sprintf("dispatch hl.dsp.focus({ workspace = %d })", id)
}

func (d *luaDriver) MoveCursorCmd(x, y int) string {
	return fmt.Sprintf("dispatch hl.dsp.cursor.move({ x = %d, y = %d })", x, y)
}

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

// luaExecCode builds the hl.exec_cmd call for a command and its rules.
func luaExecCode(command string, rules ExecRules) string {
	var opts []string
	if rules.Workspace != "" {
		opts = append(opts, "workspace = "+LuaString(rules.Workspace))
	}
	if rules.Fullscreen {
		opts = append(opts, "fullscreen = true")
	}
	if rules.NoInitialFocus {
		opts = append(opts, "no_initial_focus = true")
	}
	if rules.NoAnim {
		opts = append(opts, "no_anim = true")
	}
	return fmt.Sprintf("hl.exec_cmd(%s, { %s })", LuaString(command), strings.Join(opts, ", "))
}

// LuaString quotes s as a Lua double-quoted string literal.
func LuaString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`)
	return `"` + r.Replace(s) + `"`
}

// --- classic ---------------------------------------------------------------

type classicDriver struct{ i *Instance }

func (d *classicDriver) Mode() string { return "classic" }

func (d *classicDriver) DeclareMonitor(name string, width, height, hz, x, y int, scale float64) error {
	return d.i.Command(fmt.Sprintf("keyword monitor %s,%dx%d@%d,%dx%d,%s", name, width, height, hz, x, y, formatScale(scale)))
}

func (d *classicDriver) WorkspaceRule(workspace int, monitor string) error {
	return d.i.Command(classicWorkspaceRule(workspace, monitor))
}

// classicWorkspaceRule builds the keyword request; see luaDriver.WorkspaceRule.
func classicWorkspaceRule(workspace int, monitor string) string {
	return fmt.Sprintf("keyword workspace %d,monitor:%s,default:true,gapsin:0,gapsout:0,bordersize:0,rounding:false,decorate:false", workspace, monitor)
}

func (d *classicDriver) WorkspaceGapless(workspace int) error {
	return d.i.Command(fmt.Sprintf("keyword workspace %d,gapsin:0,gapsout:0,bordersize:0,rounding:false,decorate:false", workspace))
}

func (d *classicDriver) Exec(command string, rules ExecRules) error {
	return d.i.Command(classicExecCommand(command, rules))
}

func (d *classicDriver) FocusMonitorCmd(name string) string { return "dispatch focusmonitor " + name }

func (d *classicDriver) WorkspaceCmd(id int) string { return fmt.Sprintf("dispatch workspace %d", id) }

func (d *classicDriver) MoveCursorCmd(x, y int) string {
	return fmt.Sprintf("dispatch movecursor %d %d", x, y)
}

func (d *classicDriver) SendShortcutCmd(mods, key, address string) string {
	return fmt.Sprintf("dispatch sendshortcut %s, %s, address:%s", mods, key, address)
}

func (d *classicDriver) FocusWindowCmd(address string) string {
	return "dispatch focuswindow address:" + address
}

func (d *classicDriver) MoveWindowCmd(address string, workspace int) string {
	return fmt.Sprintf("dispatch movetoworkspacesilent %d,address:%s", workspace, address)
}

// classicExecCommand builds the `dispatch exec [rules] cmd` request.
func classicExecCommand(command string, rules ExecRules) string {
	var r []string
	if rules.Workspace != "" {
		r = append(r, "workspace "+rules.Workspace)
	}
	if rules.Fullscreen {
		r = append(r, "fullscreen")
	}
	if rules.NoInitialFocus {
		r = append(r, "no_initial_focus")
	}
	if rules.NoAnim {
		r = append(r, "no_anim")
	}
	prefix := ""
	if len(r) > 0 {
		prefix = "[" + strings.Join(r, "; ") + "] "
	}
	return "dispatch exec " + prefix + command
}

func formatScale(scale float64) string {
	return strconv.FormatFloat(scale, 'f', -1, 64)
}
