# offstage

A caged desktop for AI agents on Hyprland.

offstage was called hyprcage before 0.10. To move an existing install, remove
the old plugin (`/plugin uninstall hyprcage@hyprcage-mrwick1`), install the new
one (see below), and run `install.sh` once. The installer removes the old
binary, its agent registrations and its notification unit. On its first run,
offstage moves the old config, state and browser profiles to the new name.

When an agent needs a graphical application for itself, to test a UI, take
screenshots or drive an app, it should not do it on your desktop. offstage
gives it a screen of its own: a [cage](https://github.com/cage-kiosk/cage)
compositor with its own output and its own seat, in memory. Hyprland never
learns that screen exists, so your monitors, workspaces, focus and cursor
are never touched, and the desktop scripts that watch for a monitor being
plugged in never fire. You watch the agent work through a mirror window on
a spare workspace, or not at all. It works on any Hyprland desktop, Omarchy
included, with Claude Code, Codex, Cursor, Gemini CLI, Windsurf, OpenCode or
any other MCP client.

## Browser

Bare is the bundled browser. Installation requires both `bare-mcp` and
`bare-drive`, downloaded from the pinned Bare release with checksum verification.
A missing or invalid download fails installation. Chrome is not required.

The offstage MCP server exposes Bare's native tools as `browser_*`:
`browser_open`, `browser_snapshot`, `browser_act`, `browser_find`,
`browser_run_steps`, `browser_run_suite`, and the other Bare tools.
Use a unique `session` and close it with `browser_close` when done.
Headless browsing needs no screen or desktop connection.

On Linux, create an offstage screen and pass `screen` to every browser call to
show the session there. offstage routes its Wayland connection into that screen.
The mirror and screen recording work with this visible browser. Use one browser
session per screen and `crop: "none"` for recording. Destroying the screen closes
its browser process. On Windows, browser sessions are headless.

Bare uses its own engine. Chrome DevTools, Lighthouse, CDP emulation, performance
traces and heap snapshots are not part of this browser interface. Use Bare's
console, network and diagnosis tools. Browser compatibility must be checked with
the target app.

`offstage browser` now serves Bare MCP on stdin/stdout. Its former screen/URL
arguments and Chrome profile option are removed. The `OFFSTAGE_NO_BARE` and
Windows `-NoBare` options are rejected. Existing standalone Bare registrations
are kept; new installs need only the offstage MCP registration.

## Install

Closed-beta testers on Linux: read [docs/beta-linux.md](docs/beta-linux.md).

**Claude Code**

```
/plugin marketplace add mrwick1/offstage
/plugin install offstage@offstage-mrwick1
```

The plugin fetches its binary on first start. If cage is missing, the agent
asks for it through a password dialog the first time it needs a screen.

**Terminal**

```
curl -fsSL https://raw.githubusercontent.com/mrwick1/offstage/main/install.sh | bash
```

Installs cage, the latest release, and registers offstage with every agent
found on the machine. Options come after `bash -s --`:

```
curl -fsSL https://raw.githubusercontent.com/mrwick1/offstage/main/install.sh | bash -s -- --agents codex,cursor
```

| Option | Effect |
|---|---|
| `--agents claude,codex,gemini,cursor,windsurf,opencode` | register only these agents (default: every agent found) |
| `--agents none` | register no agent |
| `--uninstall` | remove the binary, the registrations, offstage's state and its configuration |
| `OFFSTAGE_VERSION=v0.1.0` | install that release instead of the latest |
| `OFFSTAGE_FROM_SOURCE=1` | build with Go instead of downloading |

To pin the script itself to a version as well, fetch it from the tag:

```
curl -fsSL https://raw.githubusercontent.com/mrwick1/offstage/v0.1.0/install.sh | OFFSTAGE_VERSION=v0.1.0 bash
```

**From source** (Go ≥ 1.24)

```
git clone https://github.com/mrwick1/offstage && cd offstage
make build && install -Dm755 offstage ~/.local/bin/offstage
offstage setup && offstage config && offstage doctor
```

## Windows

Windows runs the bundled Bare browser headlessly. It does not open a window on
your desktop or require Chrome. Native apps, desktop tools and recording are
unavailable on Windows.

Install from PowerShell. It needs no admin rights and changes no system
setting:

```
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/mrwick1/offstage/main/install.ps1)))
```

The script puts `offstage.exe` in `%LOCALAPPDATA%\offstage\bin`, writes the
configuration to `%APPDATA%\offstage` and registers the MCP server with Claude
Code and Codex. Options go after the script block, for example `-Agents none`,
`-BinaryOnly` or `-Uninstall`. `OFFSTAGE_VERSION=v0.9.0` pins a release. The
Claude Code plugin also works: its launcher runs the same script.

- **Tools**: the `browser_*` tools and `report_issue`.
- **Needs**: the bundled `bare-mcp.exe` and `bare-drive.exe`.
- Browser sessions end when the MCP server stops. Close each session with
  `browser_close` before handing back.

## Agents

| Agent | Tools | Skill | Screens of a finished session |
|---|---|---|---|
| Claude Code | plugin | plugin | closed at once |
| Codex | `codex mcp add` | `~/.codex/skills` | closed by the safety timer |
| Cursor | `~/.cursor/mcp.json` | `~/.cursor/skills` | closed by the safety timer |
| Gemini CLI | `gemini mcp add` | | closed by the safety timer |
| Windsurf | `~/.codeium/windsurf/mcp_config.json` | `~/.codeium/windsurf/skills` | closed by the safety timer |
| OpenCode | `~/.config/opencode/opencode.json` | `~/.config/opencode/skills` | closed by the safety timer |
| Any MCP client | `offstage mcp` on stdio | `skills/offstage/SKILL.md` | closed by the safety timer |

The installer registers offstage with every agent it finds. To choose,
`install.sh --agents codex,cursor` limits it to those, and `--agents none`
skips the step. Only the Claude Code plugin carries session hooks, which
close an agent's screens the moment its session ends. Elsewhere, the safety
timer closes them within 15 minutes.

## Usage

The agent gets `screen_create`, `app_launch`, `screenshot`, `click`, `type`,
`key`, `scroll`, `drag`, `wait`, `windows`, `screen_destroy` and a few more.
Its skill tells it to use a screen for anything it launches for itself, one
application per screen, and to close it when done.

`screenshot` returns the image inline. With `path`, it writes the image to that
file and returns the path as text, so the image stays out of the agent's
context. `path: "auto"` writes `<cache dir>/offstage/shots/<screen>-<time>.png`.

The agent reads a screen as text before it looks at an image. `snapshot`
lists the elements of the screen, one per line, with a ref, a role, a name
and screen coordinates. `act` clicks, types, presses keys, hovers or scrolls
on a ref and returns what changed. `find` waits for an element by name or
value. The text comes from the first source that answers: the DevTools
protocol (CDP), then AT-SPI, then OCR with tesseract. The first line of a
snapshot names the source. For CDP-backed native app snapshots, launch an Electron app with
`app_launch` and `debug: true`: offstage opens a DevTools port on 127.0.0.1
for it. GTK and Qt apps need the AT-SPI bus, and OCR needs `offstage setup`.
`offstage doctor` checks both.

A screen normally opens a mirror window on one of your spare workspaces, 6
to 9 by default. Switch to it with your usual workspace binding to watch the
agent live. Nothing you do there reaches the agent: the mirror shows the
screen and relays nothing back, so clicking or typing in it does nothing.
`offstage mirror <screen>` opens or closes that window at any time. Every
tool has a command-line twin: `offstage create`, `launch`, `shot`, `click`,
`type`, `destroy`, `mirror`, `list`, `config`, `doctor`, `snapshot`, `act`,
`find`.

### Saved browser state

Use `browser_save_state` to save cookies, localStorage and sessionStorage under
a name. Pass `file` to persist state between server runs. Use
`browser_ensure_session` to restore it and check that the login still works.
State files contain live credentials. Never commit or share their contents.
Legacy Chrome profiles are not imported into Bare. Existing profile files stay
on disk and can still be managed with `offstage profile`.

## Configuration

A screen is a virtual monitor of `width` × `height` pixels. Screenshots have
exactly that size and clicks use those coordinates. 1280x800 is what vision
models read without downscaling. Above 2000 px a side, screenshots are scaled
and clicks lose precision, hence the ceiling. Each screen is a compositor of
its own, with an output only it can see, so Hyprland never learns of it and
your monitors, workspaces, focus and cursor are never touched. You watch a
screen through the mirror, an ordinary window on one of your workspaces.

The installer writes `~/.config/offstage/config.toml` with every key at its
default, so changing a setting is editing a line. `offstage config` writes
it again if it is missing and never touches one that exists. The file is
read by every command, so a change applies to the next screen or mirror
without restarting anything. A key offstage does not know, or a value out of
range, makes it refuse the whole file rather than run on the defaults in
silence.

```toml
[screen]
width = 1280          # a new screen is this wide, and so is every screenshot of it
height = 800          # and this tall, 1280x800 being what vision models read best
max_width = 3840      # refuse a wider screen, whichever agent asks for it
max_height = 2160     # refuse a taller one
max_per_session = 4   # screens one agent session may hold at once, 0 lifts the limit
mirror = true         # open the window you watch a screen through, unless the agent says otherwise
notify = true         # tell you, through your desktop notifications, when a screen opens or closes

[workspaces]
mirror = [6, 9]       # the mirror windows land here, never on a workspace holding something else

[mirror]
group = "session"     # session: one workspace per agent session, its mirrors tiled side by side
                      # pack: mirrors of every session share a workspace until it is full
                      # screen: one workspace per mirror, each fullscreen
per_workspace = 4     # mirrors a workspace holds before the next one is used (session and pack)
fps = 30              # most frames per second the mirror asks for, and it asks none when idle or hidden

[lifecycle]
safety_timer = "15m"  # close a screen this long after the session that opened it died

[cage]
renderer = "auto"     # auto tries the GPU first and falls back, gles or pixman forces one
render_device = ""    # DRM node cage renders on, empty to follow your compositor's GPU
```

**Several screens at once.** Each screen is its own compositor, with its own
output, seat, applications and mirror window. An agent can hold several
(four per session by default, `max_per_session`), and closing one leaves the
others untouched. Where the mirror windows go is `mirror.group`. With
`"session"` every mirror of one agent session lands on the same workspace
and Hyprland tiles them, four screens in one glance, and your usual
fullscreen key enlarges the one you want to read. Another session takes the
next workspace. With `"pack"` the mirrors gather whatever their session,
and a workspace fills up before the next one is used: two agents with two
screens each share one. `per_workspace` says when a workspace is full, four
by default, and a seat freed by a closed screen is taken again first. With
`"screen"` each mirror takes a workspace of its own, fullscreen. In every
case a workspace freed by a closed screen goes back into the pool.

**The mirror window.** `mirror` is a default, not a rule. With `mirror = true`
every screen opens its window unless the agent has a reason not to, for
instance a long batch you never asked to watch. With `mirror = false` no
screen opens one unless the agent judges that this one is worth showing you.
Agents are told to leave the choice to you unless you said something about
watching. When a screen has no mirror, `offstage list` gives the reason in
`mirror_note`, and `offstage mirror <screen>` opens one at any time (with
`-close` to take it away). A mirror opened later gets a workspace from the
same pool, and when the pool is empty the request is refused rather than
the window opened on the workspace you are looking at. The window keeps the
screen's proportions whatever its own shape, with dark bars around the image
rather than a stretched or cropped one. The mirror only asks for a frame
when its window is visible and the screen has changed, so a hidden or idle
mirror costs nothing.

## Compatibility

Arch Linux and derivatives, Hyprland ≥ 0.50 in either configuration mode
(`hyprland.conf` or `hyprland.lua`), any launcher, any desktop shell.
Verified on Hyprland 0.56 with vanilla Arch and with Omarchy, on a real GPU
and under software rendering. The aarch64 build is tested under emulation
only.

A screen creates no Hyprland output and emits no monitor event, so nothing
on your desktop reacts to it. Hyprland is only asked to place the mirror
window, and only when there is one. Everything else, including a screen
created while no compositor is running at all, needs nothing from it.

Browsers and Electron apps are single-instance: launch them in a cage with
their own profile, or with yours closed.

## Security

offstage isolates the agent from your graphical session, not from your
system: applications in a cage run as you. See [SECURITY.md](SECURITY.md).

## Development

`make build`, `make test`. Architecture and the VM test suite:
[docs/development.md](docs/development.md).

## License

MIT

## Fork additions (mrwick1/offstage)

This fork adds recording (`record`), the bundled Bare browser (`browser_*`), the clipboard of a screen (`clip`), control of the human's own desktop (`screen: "desktop"` on the perception and pointer tools, `desktop_*`, and the `notify_*` tools with the `offstage-notifyd` user unit), `show`, a waybar module, and openSUSE support in the installer. See `contrib/README.md` `docs/superpowers/specs/2026-09-29-hyprcage-fork-design.md` and `docs/superpowers/specs/2026-09-30-hyprcage-desktop-design.md`.

The fork is also verified on openSUSE Tumbleweed with Hyprland 0.56.2 (Lua configuration, one monitor at scale 1, `input:follow_mouse = 2`). On openSUSE, the fork's installer also installs `at-spi2-core`, because GTK does not pull it in there. The `tesseract-ocr` package 5.5.3-2.1 crashes when it exits. offstage keeps the output that tesseract writes before the crash. The verification record is at the end of `docs/superpowers/specs/2026-09-30-hyprcage-desktop-rulings.md`.
