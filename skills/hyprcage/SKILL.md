---
name: hyprcage
description: REQUIRED before launching any graphical application FOR YOURSELF (testing a UI, taking screenshots, driving an app, computer-use) on a Hyprland desktop. Not for apps the human asked you to open for them: those go on their normal screens. Triggers - launch/open/test a GUI app, screenshot of an app, click/type in an app, computer-use, open_application, headless output.
---

# hyprcage: your own screen, never the human's

Anything you launch for yourself goes on a **hyprcage screen**: a compositor
with an output and a seat of its own, in memory. The human's compositor never
learns it exists, so their monitors, workspaces, focus and cursor cannot move
because of you. They can watch through a mirror window on one of their spare
workspaces (6–9 by default), and are never interrupted.

## When

| Situation | hyprcage? |
|---|---|
| You launch an app to test, measure, capture or drive it | **yes** |
| The human says "open X", "show me X" | **no**, launch it normally |
| Unsure | ask in one line |

## First use on a machine

The plugin installs its own binary on the first start. If `screen_create`
fails with `cage_missing`, the machine lacks cage, the agent's compositor:
tell the human that a password dialog is about to open, call `setup`, then
retry. Nothing else to install. `capture_failed` is a different answer: cage
is there and the screen could not be captured, often a machine too busy to
answer in time. Retry once, then tell the human. Calling `setup` for it
installs nothing and asks them for a password for nothing.

## How (MCP tools)

1. `screen_create` → **one screen per application** (cage shows one app at a time), 1280x800 by default. Remember its name.
   The human watches a screen through a mirror window. Leave `mirror` out and their configuration decides, which is the right answer unless they said something: pass `true` when they asked to watch or when showing the result is the point, `false` for a long job they have no reason to see. The reply carries `mirror_note` when there is none, and why. The `mirror` tool opens or closes that window later without touching the screen, so a screen made without one can still be shown on request.
2. `app_launch` with the command. Browsers: always a dedicated profile, and `--kiosk` with the URL when the task is a page (the whole screen is the page). Chromium: `chromium --ozone-platform=wayland --user-data-dir=/tmp/hc-profile --no-first-run --kiosk <url>`. Firefox: `mkdir -p /tmp/hc-ff && firefox --no-remote --profile /tmp/hc-ff --kiosk <url>` (the profile directory must exist). Drop `--kiosk` only when you need tabs or the address bar.
3. `screenshot`, then `click` / `type` / `key` / `scroll` / `drag` with **screen pixel coordinates**. Use `wait` (stability or title) instead of sleeping. Ask for `screenshot_after` only when you need to see the result; `settle_ms` on `screenshot` waits for a toast or an animation first.
4. `screen_destroy` **as soon as you are done**, before handing back to the human. If you keep a screen open between steps, say so.

## More tools (mrwick1 fork)

- **Record**: `record_start` on a screen, or `target: "desktop"` for the human's screen, then `record_stop`. The reply gives the MP4 path in `~/Videos/agent/`. A recording stops by itself after 30 minutes or when its screen closes. Tell the human when you record their desktop.
- **Chrome by code**: `browser_open` on a screen, then the `agent-chrome` MCP tools (DOM, console, network, JavaScript). One agent Chrome at a time. `browser_running` means the port is taken: if you opened the agent Chrome earlier in this session, reuse it; otherwise tell the human.
- **Clipboard**: `clipboard_set`, then `key ctrl+v`, to paste long text into an app. `clipboard_get` reads what the app copied. It is the screen's clipboard, never the human's.
- **The human's windows**: `desktop_windows` gives the addresses. `desktop_type` and `desktop_key` send keys without focusing the window, but each key briefly takes the human's keyboard focus. `desktop_focus` moves their focus for real. Use these only for the human's own windows; your own apps go on a screen.
- **Files**: none needed. Apps on a screen run as the human and see the same file system, so give them normal paths.

## Never

- `app &` from a shell, `open_application` from computer-use, or any launch outside `app_launch`: it lands on the human's screen.
- `hyprctl dispatch focus*`, `workspace`, `movecursor`: they touch the human.
- Destroying a screen you did not create (`not_owner`).
- Keeping every screenshot in context: each one costs ~1 300 tokens.
- Launching a browser or Electron app (Discord, Chromium, Firefox…) with the human's profile while theirs is open: single-instance apps hand the launch to the running instance, and the window opens on the human's screen. Always a dedicated profile (`--user-data-dir`), or their instance closed.

## CLI twin

Everything exists as `hyprcage <command>` for a terminal: `create`, `launch`,
`shot`, `click`, `type`, `key`, `destroy`, `mirror`, `list`, `gc`, `doctor`.
