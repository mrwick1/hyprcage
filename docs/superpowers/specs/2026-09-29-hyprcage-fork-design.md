# hyprcage fork: full computer use for agents on Hyprland

Date: 2026-09-29
Status: design approved in conversation, waiting for spec review

## Goal

Give an AI agent full computer use on a Hyprland desktop. The agent must never disrupt the human at the keyboard.

The agent works in two places:

- **Agent screens.** These are hidden screens that the agent owns. Input and capture on them are fully silent.
- **The human's desktop.** The agent can also control the human's own windows. Input there causes a short keyboard-focus blip, which the human accepts.

The fork must run on two machines without code changes: `arjun-e16` (openSUSE Tumbleweed) and `archMachine` (Arch Linux).

## Success criteria

1. The agent creates a screen, launches an application on it, clicks, types and takes screenshots. The human's focus, cursor, monitors and workspaces do not change.
2. The agent drives Chrome on an agent screen through the Chrome DevTools Protocol.
3. The agent records an agent screen or the human's desktop to an MP4 file.
4. The agent reads and writes the clipboard of an agent screen, and moves files in and out.
5. The agent lists, moves, focuses and types into the human's own windows.
6. A waybar menu lists the agent screens. A click on an entry shows that screen's mirror.
7. One smoke script passes on both machines.

## Decisions

| Decision | Choice | Reason |
|---|---|---|
| Base | Fork `hexadecimil/hyprcage` (MIT) | A spike on 2026-09-29 confirmed that it works on `arjun-e16`. |
| Input on the human's desktop | Use `send_shortcut` without asking | The human chose this. The blip is accepted. |
| In-session Hyprland plugin | Not built | Every agent action would cause a blip. That breaks the "never disrupt" goal. |
| Chrome control | Reuse `chrome-devtools-mcp --browserUrl` | It already gives the DOM, console, network and JavaScript tools. |
| Agent Chrome instances | One at a time, port `9222`, bound to `127.0.0.1` | This is the simplest option that works. Add more ports when a real task needs two browsers. |
| Repository | `mrwick1/hyprcage`, checked out at `~/coding/personal/projects/hyprcage` | This is a personal project. |

## Evidence from the spike (verified)

- hyprcage `v0.3.0-1-g30a5d16` built with Go 1.27.1. `hyprcage doctor` passed every check on Hyprland 0.56.2 with the Lua configuration.
- A screen ran Google Chrome with the Wayland backend. A click on a textarea followed by `type` put the text `typed in textarea` into the page. A screenshot confirmed the text.
- The human's active window stayed the same during the whole test. The Hyprland event socket showed no monitor event and no workspace event.
- Screenshots of an agent screen work while the human's session is locked by hyprlock.
- One screen with its `cage` compositor used about 77 MB RSS.
- The mirror window opened on workspace 6 and did not take focus.

## Evidence about the human's desktop (verified)

- `hl.dsp.send_shortcut({ mods, key, window })` delivered keys to kitty and Chrome windows that did not have focus.
- A source read of Hyprland `v0.56.2` (`src/config/shared/actions/ConfigActions.cpp`, `Actions::pass`) shows how it works. It moves seat keyboard focus to the target, sends the key, and moves focus back. The human's window gets `wl_keyboard.leave` and then `enter`.
- A mouse button sent by `send_shortcut` always lands at surface position (1,1). It cannot click at a chosen point.
- `google-chrome --class=<name>` does not set the Wayland app ID. Window rules must match on the process, not on the class.

## Architecture

The system has three parts.

### 1. The hyprcage fork (Go)

The existing code already provides screens, the mirror, and the `screen_create`, `app_launch`, `screenshot`, `click`, `type`, `key`, `scroll`, `drag`, `wait`, `windows` and `screen_destroy` tools. It also provides session hooks and the safety timer.

The fork adds these tools:

| Tool | Implementation |
|---|---|
| `record_start`, `record_stop` for an agent screen or the desktop | Run `hyprcage _record`, which sends frames from hyprcage's wlr-screencopy capture to `ffmpeg`. |
| `clipboard_get`, `clipboard_set` | Run `wl-paste` and `wl-copy` with `WAYLAND_DISPLAY` set to the screen's inner display. |
| `browser_open` | Launch Chrome on a screen with `--remote-debugging-port=9222`, `--remote-debugging-address=127.0.0.1` and a temporary profile. |
| `desktop_windows`, `desktop_focus`, `desktop_move` | Call `hyprctl clients -j` and Hyprland dispatchers. |
| `desktop_type`, `desktop_key` | Call `hl.dsp.send_shortcut` for each key, aimed at the window address. |

### 2. Chrome control

The agent calls `browser_open`. Then it uses the tools of a `chrome-devtools-mcp` instance that has the fixed argument `--browserUrl http://127.0.0.1:9222`. The fork does not reimplement DevTools tools.

### 3. The waybar module

The script `contrib/waybar/hyprcage-agents` is a polled module: waybar runs it every 2 s (`"interval": 2`), and each run prints one JSON line and exits. It gets its data from `hyprcage list --all --json` and `hyprcage record status --json`. A click runs `hyprcage-menu`, which opens a menu of the screens. Choosing an entry switches to that screen's mirror workspace.

## Runtime and safety

- **Lifecycle.** The existing session hooks close the screens of an agent session when the session ends. The 15-minute safety timer closes the screens of a session that crashed. A recording stops when its screen closes.
- **Locked session.** The desktop tools (`desktop_*` and desktop recording) refuse while hyprlock runs. The agent-screen tools continue to work.
- **Recording indicator.** Waybar shows a red dot during every desktop recording.
- **Recording limits.** The recording folder is `~/Videos/agent/`. Each recording stops after 30 minutes.
- **Errors.** Every tool returns a named error, for example `screen_not_found`, `browser_not_running` or `session_locked`. No tool fails silently.
- **Scope of isolation.** Applications on an agent screen run as the human's user. The fork isolates the human's screen, not the human's system.

## Portability

- Look up every external tool on `PATH`. Never hard-code a path.
- Teach the installer two package managers: `zypper` on openSUSE and `pacman` on Arch. The upstream installer supports only Arch.
- Keep machine-specific values in `~/.config/hyprcage/config.toml`. Examples are the mirror workspaces, the recording folder and the Chrome binary.
- Ship the waybar module as a stand-alone file. Each machine includes it from its own configuration. The installer does not edit the existing configuration.

State of the two machines on 2026-09-29:

| Item | `arjun-e16` | `archMachine` |
|---|---|---|
| Hyprland | 0.56.2, Lua configuration | 0.56.2, Lua configuration |
| `cage` | 0.3.1 installed | missing |
| `wf-recorder` | broken on openSUSE (libavformat symbol error), not used | not used |
| `ffmpeg`, Go, Google Chrome | present | present |

To verify: `cage` and `wf-recorder` are in the Arch repositories.

## Amendments 2026-09-29

These verified facts were found while planning. They override the sections above where the two differ.

1. **Recording uses `ffmpeg` for both targets.** `wf-recorder` 0.6.0 on openSUSE fails with `symbol lookup error: /lib64/libavformat.so.62: undefined symbol: rist_peer_config_defaults_set_versioned`. hyprcage's own screencopy capture feeds `ffmpeg` instead, for agent screens and for the desktop.
2. **`file_put` and `file_get` are dropped.** Applications on an agent screen run as the human's user and see the same file system. A file picker on an agent screen already reaches every path. The tools would add nothing.
3. **No stand-alone Hyprland rules file.** hyprcage places the mirror window itself. No rule is needed.
4. **Clipboard works in cage (verified).** `wl-copy` and `wl-paste` with `WAYLAND_DISPLAY` set to the inner display round-tripped the text `clip-test-123`. No extra window appeared on the screen.
5. **`ffmpeg` encoders differ per distribution.** openSUSE's `ffmpeg` 9.0.1 offers `libopenh264` and `mpeg4`, but not `libx264`. The recorder picks the first of `libx264`, `libopenh264`, `mpeg4`.

## Testing

1. Run the upstream Go tests with `make test`, and the VM suite from `docs/development.md`.
2. Add Go tests for each new tool.
3. Add the smoke script `tests/smoke.sh`. It does these steps and checks each result:
   1. Create a screen.
   2. Open Chrome and load a test page.
   3. Click and type into the page.
   4. Record 5 seconds and check that the MP4 file has frames.
   5. Set the clipboard, then get it back.
   6. Destroy the screen and check that no process remains.
4. Run the smoke script on `arjun-e16` and on `archMachine`.
5. Do a browser E2E with Chrome DevTools MCP while the human watches the mirror.

## Out of scope

- An in-session Hyprland plugin.
- More than one agent Chrome at the same time.
- AT-SPI control of the human's GTK and Qt applications. `at-spi2-core` is not installed on `arjun-e16`. Add it when a real task needs it.
- Isolation from the human's system, for example a separate user or a sandbox.

## Open items

- Log in to `mrwick1` on `arjun-e16` with `gh auth login` before the first push.
- Fix the Wispr Flow window rule in `~/.config/hypr/rules.lua`. It matches `initial_title = "Hub"`, but Wispr 1.6.957 names that window `Wispr Flow`. This is not part of the fork.
