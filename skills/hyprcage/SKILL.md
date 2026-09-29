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
   Chromium and Electron apps (VS Code, Cursor and the like): pass `debug: true`. hyprcage then opens a DevTools port on 127.0.0.1 that `snapshot` reads. If the app refuses the port, `app_launch` returns `cdp_unreachable` and the app keeps running; `snapshot` then falls back to AT-SPI or OCR.
3. `snapshot` **first**, not `screenshot`. It lists the elements of the screen as text, one per line, with a ref, a role, a name and screen coordinates. It costs a fraction of the tokens of an image.
4. `act` on a ref: `click`, `double_click`, `type` (with `text`), `key` (with `keys`), `hover` or `scroll`. The reply lists what changed on the screen, so you often need no new snapshot.
5. `find` (a regular expression on the name or value, with `timeout_ms`) to wait for an element. Use it instead of `wait` plus a screenshot.
6. `screenshot` only when the snapshot does not explain the screen: a canvas, an image, a layout question. Then `click` / `type` / `key` / `scroll` / `drag` take **screen pixel coordinates**. Use `wait` (stability or title) instead of sleeping. Ask for `screenshot_after` only when you need to see the result; `settle_ms` on `screenshot` waits for a toast or an animation first.
7. `screen_destroy` **as soon as you are done**, before handing back to the human. If you keep a screen open between steps, say so.

## Reading the screen as text

`snapshot` picks a source in this order: CDP (an app launched with `debug: true`), then AT-SPI (GTK and Qt apps), then OCR (anything else). The first line names the source: `source=<cdp|atspi|ocr> nodes=<n> truncated=<true|false>`. Every other line is one element: `[<ref>] <role> "<name>" (<x>,<y>)` plus its value, states and level when it has them.

- Refs are `e<n>` for CDP and AT-SPI, `o<n>` for OCR. A ref keeps naming the same element across snapshots of the same screen.
- `act` and `find` use the source of your last `snapshot`. They read the interactive elements again and keep only those as current refs. A ref from a `full` snapshot that points to a non-interactive element can therefore go stale after `act` or `find`.
- `act` with op `key` sends the keys to the focused element. The ref must be valid, but the keys do not go to that element. Click the element first when it does not have the focus.
- `mode` is `interactive` by default: controls, landmarks and headings. Pass `full` for every element. `max_nodes` caps the list at 300 by default and sets `truncated=true` when it cuts.
- `root` with a ref reads only that subtree. `source` forces `cdp`, `atspi` or `ocr`.
- `(action)` in place of `(<x>,<y>)` means the coordinates are not reliable. Items in an open GTK menu show it. `act` with `click` uses the element's accessibility action instead of the pointer. Other ops fail with `unsupported_input`: use `click`, or `key` to navigate the menu.
- OCR reads text only: every element has the role `text`. Clicks on OCR refs hit the centre of the text.
- `stale_ref` means the element is gone or no longer unique. Take a new `snapshot` and use the new ref.
- `ref_occluded` means another element covers the target, such as a dialog. Close the dialog, or act on the covering element.
- `no_source` means no source can read the screen. Relaunch the app with `debug: true`, or call `setup` to install OCR.

## More tools (mrwick1 fork)

- **Record**: `record_start` on a screen, or `target: "desktop"` for the human's screen, then `record_stop`. The reply gives the MP4 path in `~/Videos/agent/`. A recording stops by itself after 30 minutes or when its screen closes. Tell the human when you record their desktop.
- **Chrome by code**: `browser_open` on a screen, then the `agent-chrome` MCP tools (DOM, console, network, JavaScript). Use the `agent-chrome` tools only after `browser_open` succeeded in this session: before that, they can reach a different browser. One agent Chrome at a time. `browser_running` means the port is taken: if you opened the agent Chrome earlier in this session, reuse it; otherwise tell the human.
- **Clipboard**: `clipboard_set`, then `key ctrl+v`, to paste long text into an app. `clipboard_get` reads what the app copied. It is the screen's clipboard, never the human's.
- **The human's windows**: `desktop_windows` gives the addresses. `desktop_type` and `desktop_key` send keys without focusing the window, but each key briefly takes the human's keyboard focus. `desktop_focus` moves their focus for real. Use these only for the human's own windows. Your own apps go on a screen.
- **Files**: no file tool is needed. Apps on a screen run as the human and see the same file system, so give them normal paths.

## Never

- `app &` from a shell, `open_application` from computer-use, or any launch outside `app_launch`: it lands on the human's screen.
- Raw `hyprctl dispatch` calls (`focus*`, `workspace`, `movecursor` and the like): never use them. The `desktop_*` tools are the only way to act on the human's windows, and only when the task needs it.
- Destroying a screen you did not create (`not_owner`).
- Keeping every screenshot in context: each one costs ~1 300 tokens.
- Taking a screenshot to read text or to find a button: `snapshot` and `find` give it to you as text.
- Launching a browser or Electron app (Discord, Chromium, Firefox…) with the human's profile while theirs is open: single-instance apps hand the launch to the running instance, and the window opens on the human's screen. Always a dedicated profile (`--user-data-dir`), or their instance closed.

## CLI twin

Everything exists as `hyprcage <command>` for a terminal: `create`, `launch`,
`shot`, `click`, `type`, `key`, `destroy`, `mirror`, `list`, `gc`, `doctor`,
`record`, `clip`, `desktop`, `browser`, `show`, `snapshot`, `act`, `find`.
The CLI stores the refs of each screen between two commands, so `hyprcage act`
uses the refs of the last `hyprcage snapshot` or `hyprcage find`.
