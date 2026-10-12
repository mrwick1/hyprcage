# Development

## How it works

A screen is cage on wlroots' headless backend: a compositor that owns its
output and renders it in memory, with a seat of its own. Nothing about it
reaches the human's compositor, so there is no output to declare, no
workspace to pin, no monitor event for a desktop shell to react to. cage is
started directly, in a transient systemd scope, with an environment built
rather than inherited so that wlroots reads exactly what hyprcage chose.
hyprcage then connects to cage as a Wayland client and drives it with the
virtual-pointer, virtual-keyboard, output-management and screencopy
protocols, in pure Go.

The human watches through `hyprcage _mirror`, a second Wayland client that
holds two connections at once: it asks cage for a frame and shows that same
memory, unmodified, in an ordinary window on the human's compositor.

Every screen runs in its own transient systemd slice, with a record in
`$XDG_RUNTIME_DIR/hyprcage`. Screens close when the agent asks, when its
session ends (hooks, stdin EOF) and, at the latest, on a per-screen safety
timer once the session is dead.

```
cmd/hyprcage          the binary
internal/hypr         Hyprland IPC, events, classic + Lua config drivers
internal/mirror       the human's mirror window
internal/screen       create / launch / destroy / gc, capture, input, selection.go (read_selection)
internal/wl           the Wayland client (pointer, keyboard, screencopy)
internal/registry     one record per screen
internal/session      who owns a screen, is that session alive
internal/sysd         systemd slices, scopes, timers
internal/config       config.toml
internal/audit        the action log (actions.jsonl), one line per tool call
internal/guard        the [guard] policy: blocked keys, destructive labels, app allowlist
internal/perceive     snapshots, refs, find, act, and the frame tools:
                        diff.go (FrameDiff), verify.go (verdicts), watch.go,
                        readpage.go (read_page)
internal/setup        installing cage with the human's authorisation
internal/mcpserver    the MCP server
                        server_perceive.go (snapshot, act, find, read_page),
                        server_guard.go (guard in front of the actions),
                        verify.go (verify around the actions),
                        server_watch.go (watch), server_clip.go (read_selection)
internal/cli          the command line
bin/, hooks/, skills/, .mcp.json, .claude-plugin/   the plugin
install.sh, .github/  the installer and the release workflow
tests/vm/             the VM test suite
```

## Configuration modes

Hyprland 0.56 ships two parsers. The driver is chosen at runtime
(`hyprctl repl "return 1"` answers `1` under Lua, and `HYPRCAGE_DRIVER`
overrides it). Since 0.3 the only thing either driver does is launch the
mirror window with placement rules.

| | classic | Lua |
|---|---|---|
| launch with rules | `dispatch exec [workspace 6 silent; no_initial_focus] …` | `hl.exec_cmd(cmd, {…})` |

## The screen

cage is started with `WLR_BACKENDS=headless`, which makes wlroots give it an
output of its own, 1280x720 by default and in memory. `SetMode` then resizes
that output to the screen's size through `wlr-output-management`, the same
protocol `wlr-randr` speaks: the refresh rate goes in millihertz, so 60 Hz is
`60000`, and a plain `60` would leave cage rendering one frame every sixteen
seconds.

The renderer is named explicitly, `WLR_RENDERER=gles2` and then `pixman`,
never `auto`: left to itself wlroots falls back to software rendering without
a word, and the capture probe would bless a screen no application can use a
GPU on. Which one actually runs is read back from cage's globals
(`zwp_linux_dmabuf_v1` is there under GLES, absent under pixman) and recorded.

`RenderDevice` picks the DRM render node: `cage.render_device`, then the
environment a compositor was pinned with (`WLR_RENDER_DRM_DEVICE`,
`AQ_DRM_DEVICES`, `WLR_DRM_DEVICES`), then the `main_device` the human's
compositor tells every client through the linux-dmabuf feedback, then a node
of a device with a connected output. Each candidate is resolved through
sysfs by its device number, so a udev symlink and a card node both land on
the right `renderD*`. On a laptop with two GPUs this keeps cage on the one
the human's compositor already uses.

## Input devices

The virtual pointer and keyboard belong to the Wayland client that created
them, and every hyprcage command is a client that comes and goes. If the
devices came and went with it, the seat's capabilities would appear and
vanish under the applications, which re-bind their input on each change and
lose the keys sent in between. `_holder`, which already lives exactly as
long as the screen, therefore creates one pointer and one keyboard and holds
them: the seat is steady, and a command's own devices only ever add to it.

Typing goes through a generated xkb keymap, one key per distinct keysym,
which covers all of Unicode whatever the human's layout. Which physical key
carries a keysym is not free, though. Chromium and Electron derive the DOM
`keyCode` of anything but an ASCII letter or digit from the physical key,
and a character whose key says Escape, BackSpace, Tab or Enter is acted on
as that key and never inserted. The first version put the first keysym of
every `type` on keycode 9, the Escape key, so a leading `/` vanished in
Claude Desktop while `abc/def` typed fine. Now a keysym takes the key that
carries it in the US layout when it has one (`/` on Slash, so `code` and
`keyCode` even match a real keyboard) and otherwise a key that is printable
there; the pool is 64 keys, so a text with more distinct characters goes in
several keymaps. `keys.html` in the VM suite reads back what Chromium saw.

## The mirror

`hyprcage _mirror` holds two Wayland connections: cage, for capture only (it
binds no seat and relays no input, so clicking in the mirror does nothing),
and the human's compositor, where it owns one xdg toplevel. A memory file is
imported as a `wl_shm_pool` by both sides, so the buffer cage copies into is
the very buffer the human's compositor displays. Two buffers alternate on
`wl_buffer.release`.

A frame is asked for only when the compositor has answered the previous
frame callback, which it only does while the window is visible, and cage is
asked with `copy_with_damage`, which returns nothing until the screen
changes. A hidden mirror and a still screen both cost nothing. `mirror.fps`
caps the rest.

The window is two surfaces: the toplevel's own surface is a backdrop, a
single dark pixel that a viewport stretches over the whole window, and the
frame sits on a subsurface above it, scaled by its own viewport to the
largest size of the screen's proportions that fits, and centred. A window
of another shape than the screen shows bars, never a stretched or cropped
image, and a tile smaller than the screen still shows all of it.

Two things about xdg-shell are worth knowing. Hyprland sends the first
configure a tick after the initial commit rather than in the same round
trip, so `Start` waits for it. And a viewport or a subsurface attached to the
surface before that first configure keeps it from ever arriving, so the
viewport is created afterwards.

## Several screens

Nothing is shared between screens: each has its own cage, its own Wayland
socket, its own slice and its own mirror. Two things keep them apart. A
mirror's workspace is reserved against the other screens' records as well as
against the workspaces Hyprland knows, because a mirror that was just given
a workspace has not mapped its window yet and Hyprland cannot report it. The
choice is made in `mirrorWorkspace`, at `create` when the screen opens with
a mirror and in `StartMirror` when the mirror comes later: a screen created
without one has no workspace on record, and launching its window without the
placement rule would drop it on whatever the human is looking at. When the
pool is empty the mirror is refused. And every unit name carries the screen
it belongs to plus, for the roles there can be several of, a pid and a
sequence number: `orphanScreenName` has to strip those, or `gc` reads the
suffix as part of the screen name, finds no record under it and stops a unit
that is working.

`mirror.group` decides what shares a workspace, in `pickWorkspace`, a pure
function over Hyprland's workspaces and the other screens' records so that
the cases are unit tested. A workspace that already holds live mirrors is
joined when the group allows it, `"session"` for mirrors of the same owner
and `"pack"` for anyone's, and only while it holds fewer than
`mirror.per_workspace` of them; the lowest such workspace wins, so mirrors
gather rather than spread and a seat freed by a closed screen is taken
first. Failing that, the first workspace of the range that holds nothing.
`"screen"` never joins and the mirror process fullscreens its window; the
other two leave the window as Hyprland tiles it. A screen keeps its
workspace on record while it lives, mirror open or not, so a mirror closed
and reopened comes back among its neighbours. Owners are compared as
`CheckOwner` does, by session id when both have one, else by pid.

## Process inventory

Without a window in Hyprland there is no client list to ask, so a screen's
processes are found by the `HYPRCAGE_SCREEN` variable each of them carries,
and cage's pid comes from `_holder`, its own child, through the `.inner`
file. `destroy` stops the applications first, then the mirror, then cage, so
that an application sees its SIGTERM before its compositor disappears.
Scopes are transient and collected (`--collect` plus `reset-failed`), so a
process killed at the stop timeout leaves no failed unit holding its name.

## Screens from before 0.3

A record without a `version` was written when a screen was a Hyprland
output. `internal/screen/legacy.go` tears those down the old way: remove the
output, then put back the workspace, focus and cursor that Hyprland moved,
watching for two seconds because a desktop's own scripts react after
Hyprland does, and trace it in
`~/.local/state/hyprcage/log/restore.log`. `gc` also removes an output
carrying the prefix that no record claims. This is the only code that still
touches the human's monitors, and it exists so that updating the binary
under a running screen cannot leave one behind.

## Renderer

hyprcage probes one capture before handing the screen over and falls back to
pixman when it does not complete, remembering the choice in
`~/.local/state/hyprcage/renderer`. Virtualized GPUs (virgl) fail that probe
with every renderer. The probe waits 15 seconds because a loaded machine can
delay a capture that works, and a renderer that was remembered and then
failed is forgotten again, so one bad night never pins a machine to software
rendering. A probe that times out answers `capture_failed`, never
`cage_missing`, which is the code that sends an agent to `setup`.

## Debugging the protocol

`HYPRCAGE_WL_DEBUG=1` makes the Wayland client trace every request and event
on stderr. `WAYLAND_DEBUG` does nothing here: it belongs to libwayland, and
this client speaks the wire protocol itself.

## Test suite

Everything is validated in a VM, never on a desktop: a vanilla Arch guest
from the cloud image, driven over SSH.

```
tests/vm/vm.sh fetch && tests/vm/vm.sh init && tests/vm/vm.sh start && tests/vm/vm.sh provision
tests/vm/vm.sh ssh HYPR_CONFIG=classic ./hypr-start.sh          # or lua
make build && tests/vm/vm.sh scp hyprcage tests/vm/guest/spikes3.sh guest:/home/arch/
tests/vm/vm.sh ssh EXPECT_DRIVER=classic ./spikes3.sh
tests/vm/vm.sh ssh ./multi.sh                                   # several screens at once
```

`HYPRCAGE_VM_PROFILE=omarchy HYPRCAGE_VM_SSH_PORT=2223` keeps a second guest
with the `omarchy` package. `guest/install-test.sh` exercises the installer.
`make dist` builds the release assets and a tag `v*` publishes them.
