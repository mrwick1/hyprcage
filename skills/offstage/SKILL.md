---
name: offstage
description: REQUIRED before launching any graphical application FOR YOURSELF (testing a UI, taking screenshots, driving an app, computer-use) on a Hyprland desktop, or in the Bare browser on Windows. Not for apps the human asked you to open for them: those go on their normal screens. Triggers - launch/open/test a GUI app, screenshot of an app, click/type in an app, computer-use, open_application, headless output.
---

# offstage: your own screen, never the human's

Anything you launch for yourself goes on a **offstage screen**: a compositor
with an output and a seat of its own, in memory. The human's compositor never
learns it exists, so their monitors, workspaces, focus and cursor cannot move
because of you. They can watch through a mirror window on one of their spare
workspaces (6–9 by default), and are never interrupted.

## When

| Situation | offstage? |
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
installs nothing and asks them for a password for nothing. `cage_failed` means cage is installed but did not start: read the cage log named in the error, and do not call `setup`.

## Browser: Bare

Bare is required and ships with offstage as `bare-mcp` and `bare-drive`.
Use the `browser_*` tools of the offstage MCP server for all browser work.
Do not launch Chrome or use it as a fallback. Report unsupported pages with
`report_issue`, area `bare`.

1. Call `browser_open` with `url` and a unique `session`. It runs headlessly.
2. Read `browser_snapshot`. Use `browser_act` and `browser_find` by ref or locator.
3. Use `browser_read` for text and `browser_screenshot` for visual evidence.
4. Use `browser_console`, `browser_network` and `browser_diagnose` to inspect errors.
5. Use `browser_ensure_session` and `browser_save_state` for login state. State files
   contain live credentials. Never commit, print or share their contents.
6. Use `browser_run_steps` or `browser_run_suite` for scripted journeys.
7. Call `browser_close` for every session you opened before handing back.

To show the browser on Linux, first call `screen_create`. Pass `screen` and
`session` to every `browser_*` call. offstage binds that browser process to the
screen's Wayland socket. Use the screen name returned by `screen_create`; a
configured prefix can change the name you supplied. Use one browser session per screen. Close the browser,
then call `screen_destroy`. Destroying the screen also closes its browser process.
Use `record_start` with `crop: "none"` to record a visible browser screen.

Headless sessions do not need `screen_create`. On Windows, only headless Bare
sessions and `report_issue` are available. Native apps, mirrors, desktop input
and recording are unavailable there. Chrome is not required on either platform.

The browser uses its own engine and protocol. It does not implement Chrome
DevTools, Lighthouse, performance traces, heap snapshots or CDP emulation.
Do not call `devtools_*` for Bare. Use viewport `width` and `height` on browser
session creation when testing another screen size.

`offstage browser` serves Bare's native MCP protocol on stdin/stdout for a
standalone client. The former `offstage browser [screen] [url]` command is removed.
Use only offstage's `browser_*` tools. If a standalone `bare` MCP server is
also registered, do not use it: visible sessions stay isolated only through offstage.

## How (MCP tools)

1. `screen_create` → **one screen per application** (cage shows one app at a time), 1280x800 by default. Remember its name.
   The human watches a screen through a mirror window. Leave `mirror` out and their configuration decides, which is the right answer unless they said something: pass `true` when they asked to watch or when showing the result is the point, `false` for a long job they have no reason to see. The reply carries `mirror_note` when there is none, and why. The `mirror` tool opens or closes that window later without touching the screen, so a screen made without one can still be shown on request.
2. `app_launch` with the native application's command. Use a dedicated profile for single-instance applications. For Electron apps, `debug: true` enables CDP-backed text snapshots. Browser pages use the Bare workflow above.
3. `snapshot` **first**, not `screenshot`. It lists the elements of the screen as text, one per line, with a ref, a role, a name and screen coordinates. It costs a fraction of the tokens of an image.
4. `act` on a ref: `click`, `double_click`, `type` (with `text`), `key` (with `keys`), `hover` or `scroll`. The reply lists what changed on the screen, so you often need no new snapshot.
5. `find` (a regular expression on the name or value, with `timeout_ms`) to wait for an element. Controls, landmarks and headings match first. When none match, page text matches too, so `find` can check a status line or a success message. Use it instead of `wait` plus a screenshot.
6. `screenshot` only when the snapshot does not explain the screen: a canvas, an image, a layout question. Then `click` / `type` / `key` / `scroll` / `drag` take **screen pixel coordinates**. Use `wait` (stability or title) instead of sleeping. Ask for `screenshot_after` only when you need to see the result; `settle_ms` on `screenshot` waits for a toast or an animation first. Pass `path` (or `path: "auto"` for `<cache dir>/offstage/shots/<screen>-<time>.png`) to write the image to a file and get only the path back. Use it to hand evidence to another agent without the image entering your context.
7. `screen_destroy` **as soon as you are done**, before handing back to the human. If you keep a screen open between steps, say so.

## Reading the screen as text

`snapshot` picks a source in this order: CDP (an app launched with `debug: true`), then AT-SPI (GTK and Qt apps), then OCR (anything else). The first line names the source: `source=<cdp|atspi|ocr> nodes=<n> truncated=<true|false>`. Every other line is one element: `[<ref>] <role> "<name>" (<x>,<y>)` plus its value, states and level when it has them.

- Refs are `e<n>` for CDP and AT-SPI, `o<n>` for OCR. A ref keeps naming the same element across snapshots of the same screen.
- `act` and `find` use the source of your last `snapshot`. They read the interactive elements again and keep only those as current refs. Exception: when `find` matches only page text, it keeps every element as a current ref. A ref from a `full` snapshot that points to a non-interactive element can therefore go stale after `act` or `find`.
- `act` with op `key` sends the keys to the focused element. The ref must be valid, but the keys do not go to that element. Click the element first when it does not have the focus.
- `mode` is `interactive` by default: controls, landmarks and headings. Pass `full` for every element. `interactive` leaves out the page text: paragraphs, alerts and status lines. Use `full` to read them. `max_nodes` caps the list at 300 by default and sets `truncated=true` when it cuts.
- `root` with a ref reads only that subtree. `source` forces `cdp`, `atspi` or `ocr`.
- `(action)` in place of `(<x>,<y>)` means the coordinates are not reliable. Items in an open GTK menu show it. `act` with `click` uses the element's accessibility action instead of the pointer. Other ops fail with `unsupported_input`: use `click`, or `key` to navigate the menu.
- OCR reads text only: every element has the role `text`. Clicks on OCR refs hit the centre of the text.
- `stale_ref` means the element is gone, vanished before the input, or is no longer unique. Take a new `snapshot` and use the new ref.
- `ref_offscreen` means the element stays off screen after offstage tried to scroll it into view. Scroll its container, then take a new `snapshot`.
- `ref_occluded` means another element covers the target, such as a dialog. Close the dialog, or act on the covering element.
- `unsupported_input` means the source cannot do this op on this element, for example `hover` on an `(action)` node. Use `click`, or use `key` to navigate.
- `cdp_unreachable` means the DevTools port does not answer or no longer belongs to the screen. Relaunch the app with `debug: true`, or use `source: atspi` or `source: ocr`.
- A `snapshot` can come back with `nodes=0`, and an `act` diff can list every element as removed, when an app drops off the accessibility bus. Then take one `screenshot`, or use `source: ocr`.
- An `act` that returns an error after it sent the input can still have taken effect. Take a `snapshot` before you retry it.
- `no_source` means no source can read the native app screen. Relaunch the app with `debug: true`, or call `setup` to install OCR.

## More tools (mrwick1 fork)

- **Record**: `record_start` on a screen, or `target: "desktop"` for the human's screen, then `record_stop`. The reply gives the MP4 path in `~/Videos/agent/`. A recording stops by itself after 30 minutes or when its screen closes. Tell the human when you record their desktop. For a recording that a person watches, such as a PR demo, create the screen with `size: "1920x1080"`: the 1280x800 default clips wide page layouts. For visible Bare sessions, pass `crop: "none"` to record the screen. Native apps launched with `debug: true` can use viewport cropping. The `[record] crop` key in `~/.config/offstage/config.toml` sets the default (`viewport` or `none`). A pause in which the screen does not change stays in the video for 1 second at most, and the rest of it is cut, so the time you spend between actions does not stretch the video. The `[record] idle` key sets that limit; `"0s"` keeps real time. While a screen records, the pointer glides to each target over 0.4 seconds instead of jumping, and the video shows a blue ring at each click. Outside a recording, the pointer still jumps, so tests do not slow down.
- **Clipboard**: `clipboard_set`, then `key ctrl+v`, to paste long text into an app. `clipboard_get` reads what the app copied. It is the screen's clipboard, never the human's.
- **Files**: no file tool is needed. Apps on a screen run as the human and see the same file system, so give them normal paths.

## Know whether an action worked

- **Verdict**: each action tool returns a `verify:` line, for example `verify: CONFIRMED (changed 4.2%, bbox ...)`. The server compares a frame before the action with a frame after it. `CONFIRMED` means the changed area contains the target. `PARTIAL` means the change is near the target, or covers only some points of a drag. `DIVERGED` means the change is far from the target. `NO_OP` means the screen did not change. Pass `verify: false` to skip the check. The `[verify] enabled` key in `config.toml` sets the default. The check adds about 120 ms to an action.
- **Sense**: the result also has a `sense` object with `changed`, `opened`, `modal` and `no_op`. `opened` is true when the action opened a window. `modal` is true only when AT-SPI names the new window a dialog. `modal: false` can mean unknown.
- **Spacing**: wait 0.5 seconds or more between two clicks at the same spot. Applications can read two quick clicks as a double-click and select a word. The selection then shows as a change, and a later click on a dead area returns `DIVERGED` instead of `NO_OP`.
- **Animated pages**: a video or a spinner can make a verdict `CONFIRMED` when the action did nothing. Use `watch` for these pages.
- **`watch`**: `watch` with `screen`, `seconds` (default 5, maximum 30) and `fps` (default 1, maximum 4) samples the screen. The verdict is `stable`, `jitter`, or `frozen`. `frozen` needs `expect_change: true` and a screen that never changes. A change in fewer than half of the frames returns `stable`. Pass `path` (a directory) to write each frame as a PNG. Without `path`, the reply has three thumbnails. It works on agent screens only.
- **`read_selection`**: copies the selection of the focused window and returns the exact text, with no OCR. It restores the earlier clipboard of the screen. Do not use it in a terminal, because ctrl+c interrupts the program.
- **`read_page`**: scrolls a view from the top to the bottom and returns every node once. `max_steps` (default 30) and `max_nodes` (default 1000) limit it. It leaves the page scrolled to the bottom. It works on agent screens only, and not with `source: ocr`.
- **Audit log**: every tool call adds one JSON line to `~/.local/state/offstage/actions.jsonl`. The line holds the tool, the screen, the arguments, the error code, the verdict and `ack`. For a pointer action or `act`, it also holds the role and name of the element aimed at, so `offstage replay` can find it again. The log never holds typed text (only its length), script text, environment values, URL queries, or printable keys. Command arguments stay after redaction of tokens, home paths and emails.

## Check a run end to end

After every action, read the result. A click that returns no error does not prove that the page did the right thing.

1. Take a `snapshot` with `mode: full`. In `interactive` mode, a loading page and an empty page both looked like a ready page, because the status text is missing.
2. Use the `act` diff only as a hint. It lists controls, so it misses an error banner on screen.
3. For Bare, read `browser_console` after each step. A page with failing trackers returned about 1,000 entries, and 994 were one repeated line. Collapse the repeats before you read.
4. Read `notify_list` or `notify_wait` when the app under test sends desktop notifications. They read the human's tray, so filter with `app` or `match`.
5. Name the page state before you act: ready, loading, error page, login wall, captcha, empty, or a validation message. Stop and report on an error page or a captcha.

Where the kit is installed, `~/coding/personal/kit/bin/offstage-jev` covers steps 3 to 5 and plans form fills (`state`, `verify`, `check`, `fill`, `console`, `notify`, `find`). It sends the text to an outside model. Use it on your own test pages, not on client data or on a real tray.

For a scripted end-to-end journey, use `browser_run_steps` or `browser_run_suite`. Report a Bare problem with `report_issue` and area `bare`. Do not fall back to Chrome.

## Review a whole app

Use `browser_links` to inspect navigation. Use `browser_run_steps` to check
read-only journeys. Do not follow links or submit forms that change data unless
the task authorizes the action. Check console and network failures after each
journey. The older external `offstage-map` helper uses Chrome; do not use it.

## The human's desktop

Use the human's desktop only when the job needs their own apps or their session. Your own apps still go on a screen.

- **Target**: pass `screen: "desktop"` to `screenshot`, `snapshot`, `find`, `act`, `click`, `double_click`, `move`, `scroll`, `drag` and `app_launch`. Add `window` (an address from `desktop_windows`) to work on one window. Coordinates are global logical pixels.
- **Read first**: `snapshot` and `find` work on any window, also on a hidden workspace. `screenshot` with `window` captures one window on any workspace. Without `window`, it captures the visible output.
- **Act without the cursor**: `act` tries AT-SPI first (no cursor move, no focus change), then CDP, then the real pointer or keys. The first line of the reply is `path=atspi|cdp|pointer|shortcut`.
- **The pointer**: `click`, `scroll`, `drag` and a pointer `act` move the human's real cursor and then put it back. Focus follows the click. On a window that is not on a visible workspace, they refuse with `window_hidden`.
- **Workspaces**: no tool changes the human's view, except `desktop_workspace`. Call it only when a pointer action needs a hidden window.
- **Launch**: `app_launch` with `screen: "desktop"` opens the app on the given `workspace` (default: the current one) without a view change. The reply gives the window address.
- **Keys**: `desktop_type` and `desktop_key` send keys to a window without focusing it. Each key briefly takes the human's keyboard focus. `desktop_focus` moves their focus for real, and `desktop_move` moves a window to a workspace.
- **Notifications**: `notify_list` shows the notifications of the last 48 hours. `notify_wait` waits for a new one. `notify_act` closes one, or invokes an action of the latest one only. `notifyd_down` means the daemon does not run: tell the human.
- Every desktop tool refuses with `session_locked` while the screen is locked.
- **Guard**: a desktop action that closes a window or hits a destructive control returns `ack_required` with the reason. The blocked keys are `Ctrl+W`, `Ctrl+Q` and `Alt+F4`. The blocked clicks are on controls with a label such as delete, remove, erase, format, uninstall, close all, quit or shut down. Repeat the call with `ack: "<reason>"` only when the task needs the action. The server logs the reason. The guard reads the label from your last `snapshot`. With no snapshot, only the keys are blocked. The `[guard]` table in `config.toml` sets `enabled` (`desktop`, `all` or `off`), `apps`, `keywords` and `combos`.
- **Audio**: each screen has its own silent audio output, `offstage_<screen>`. Apps on the screen play into it, not into the human's speakers. `[audio] enabled = false` turns it off. Without `pactl`, apps play into the speakers. To check sound, call `condition_wait` with `{"kind": "audio", "playing": true}` or `false`. Playing means a stream is open and not paused; the level can still be silent.
- **File dialogs**: GTK and Qt apps on a screen open their file dialog in the screen. Drive it with `snapshot` and `act`. An Electron app still opens its dialog on the human's desktop. Do not open a file dialog in an Electron app. `[screen] dialogs = "host"` turns the feature off.
- **System prompts**: a desktop action returns `system_prompt` when a polkit, keyring or pinentry prompt has the focus, is the target window, or lies under the click. `ack` does not lift it, and `enabled = "off"` does not turn it off. Ask the human to answer the prompt, then retry. `prompt_classes` in `[guard]` adds app ids.

## Report offstage problems (beta)

Call `report_issue` when offstage itself fails (type `bug`):

- A tool returns an error that your arguments did not cause.
- A tool returns success, but `verify` or a new `snapshot` shows the action did not happen.
- A tool touched the human's desktop, cursor or focus (`area: "isolation-leak"`).
- offstage crashed or did not start (`area: "crash"`). A tool that panics returns the error code `internal`. After a crash, the first result of the new server says that crash files wait. A report with `area: "crash"` carries those files.

Pass `type` to say what you file. `bug` is the default and covers the list above. The other three need no `expected` or `actual`, and `screen` is optional:

- `doubt`: you were unsure how to use offstage. Example: `type: "doubt", title: "Unclear whether act or click fits a canvas button"`.
- `suggestion`: a tool or option would have helped. Example: `type: "suggestion", title: "snapshot could filter by role"`.
- `feedback`: what worked well or badly. File one at the end of a task that used offstage heavily. Example: `type: "feedback", title: "find with timeout_ms replaced every wait"`.

Never file any type about the app under test, wrong arguments, or an action the guard blocked. Send one report per distinct item. Do not repeat one you already filed this session. For a bug, pass `screen`, so the report carries its tool calls, logs, element tree and screenshot. When the reply is a preview, show it to the human and send it again with `confirmed: true` only after they agree. Without a beta key the report waits on the machine. The human runs `offstage report login`.

## Never

- `app &` from a shell, `open_application` from computer-use, or any launch outside `app_launch`: it lands on the human's screen.
- Raw `hyprctl dispatch` calls (`focus*`, `workspace`, `movecursor` and the like): never use them. The `desktop_*` tools and `screen: "desktop"` are the only way to act on the human's desktop, and only when the task needs it.
- Destroying a screen you did not create (`not_owner`).
- Keeping every screenshot in context: each one costs ~1 300 tokens.
- Taking a screenshot to read text or to find a button: `snapshot` and `find` give it to you as text.
- Launching a browser or Electron app (Discord, Chromium, Firefox…) with the human's profile while theirs is open: single-instance apps hand the launch to the running instance, and the window opens on the human's screen. Always a dedicated profile (`--user-data-dir`), or their instance closed.

## CLI twin

Everything exists as `offstage <command>` for a terminal: `create`, `launch`,
`shot`, `click`, `type`, `key`, `destroy`, `mirror`, `list`, `gc`, `doctor`,
`record`, `clip`, `desktop`, `browser`, `show`, `snapshot`, `act`, `find`.
The desktop target and the `notify_*` tools exist only over MCP. The CLI stores the refs of each screen between two commands, so `offstage act`
uses the refs of the last `offstage snapshot` or `offstage find`.
