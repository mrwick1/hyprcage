package hypr

import "testing"

func TestLuaString(t *testing.T) {
	cases := map[string]string{
		`hc-1a2b3c`:  `"hc-1a2b3c"`,
		`say "hi"`:   `"say \"hi\""`,
		`back\slash`: `"back\\slash"`,
		"two\nlines": `"two\nlines"`,
		`it's`:       `"it's"`,
	}
	for in, want := range cases {
		if got := LuaString(in); got != want {
			t.Errorf("LuaString(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestLuaExecCode(t *testing.T) {
	got := luaExecCode("cage -d -- /x _holder hc-1", ExecRules{Workspace: "11 silent", Fullscreen: true, NoInitialFocus: true, NoAnim: true})
	want := `hl.exec_cmd("cage -d -- /x _holder hc-1", { workspace = "11 silent", fullscreen = true, no_initial_focus = true, no_anim = true })`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := luaExecCode("true", ExecRules{}); got != `hl.exec_cmd("true", {  })` {
		t.Errorf("no rules: %s", got)
	}
}

func TestClassicExecCommand(t *testing.T) {
	got := classicExecCommand("hyprcage _mirror hc-1", ExecRules{Workspace: "6 silent", Fullscreen: true, NoInitialFocus: true, NoAnim: true})
	want := "dispatch exec [workspace 6 silent; fullscreen; no_initial_focus; no_anim] hyprcage _mirror hc-1"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := classicExecCommand("true", ExecRules{}); got != "dispatch exec true" {
		t.Errorf("no rules: %s", got)
	}
}

func TestClassicWorkspaceRule(t *testing.T) {
	got := classicWorkspaceRule(11, "hc-1a2b3c")
	want := "keyword workspace 11,monitor:hc-1a2b3c,default:true,gapsin:0,gapsout:0,bordersize:0,rounding:false,decorate:false"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestFullscreenState(t *testing.T) {
	for in, want := range map[string]FullscreenState{"true": 2, "false": 0, "null": 0, "0": 0, "1": 1, "2": 2} {
		var f FullscreenState
		if err := f.UnmarshalJSON([]byte(in)); err != nil || f != want {
			t.Errorf("%s: got %d, %v; want %d", in, f, err, want)
		}
	}
	var f FullscreenState
	if err := f.UnmarshalJSON([]byte(`"x"`)); err == nil {
		t.Error("expected an error for a string")
	}
}

func TestLuaWorkspaceRule(t *testing.T) {
	got := luaWorkspaceRule(11, "hc-1a2b3c")
	want := `hl.workspace_rule({ workspace = "11", monitor = "hc-1a2b3c", default = true, gaps_in = 0, gaps_out = 0, border_size = 0, no_rounding = true, decorate = false })`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := luaWorkspaceRule(6, ""); got != `hl.workspace_rule({ workspace = "6", gaps_in = 0, gaps_out = 0, border_size = 0, no_rounding = true, decorate = false })` {
		t.Errorf("unpinned: %s", got)
	}
}

func TestDispatchCommands(t *testing.T) {
	var lua ConfigDriver = &luaDriver{}
	var classic ConfigDriver = &classicDriver{}
	cases := []struct{ got, want string }{
		{lua.FocusMonitorCmd("HDMI-A-1"), `dispatch hl.dsp.focus({ monitor = "HDMI-A-1" })`},
		{lua.WorkspaceCmd(2), `dispatch hl.dsp.focus({ workspace = 2 })`},
		{lua.MoveCursorCmd(3122, 562), `dispatch hl.dsp.cursor.move({ x = 3122, y = 562 })`},
		{classic.FocusMonitorCmd("HDMI-A-1"), "dispatch focusmonitor HDMI-A-1"},
		{classic.WorkspaceCmd(2), "dispatch workspace 2"},
		{classic.MoveCursorCmd(3122, 562), "dispatch movecursor 3122 562"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got  %s\nwant %s", c.got, c.want)
		}
	}
}

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
