# hyprcage desktop control: drive the human's own Hyprland desktop

Date: 2026-09-30
Status: draft, for review
Base branch: `main` (`b1bce3e`)

## Goal

Let an agent do unattended jobs on the human's real Hyprland desktop, with the same text-first tools as on an agent screen. The human can be away (a job started from Telegram or the phone) or at the keyboard. The agent must not disturb a human who works at the same time, except where this spec accepts it.

This is sub-project B of three. Sub-project A (perception on agent screens) is done. Sub-project C (OS tools) gets its own spec.

## Scope

In scope:

- Read the desktop: screenshot, `snapshot` and `find` on the human's windows.
- Act on the desktop: `act`, `click`, `scroll` and `drag` on the human's windows.
- Switch the visible workspace.
- Launch an application on the human's desktop.
- Read notifications, act on them and wait for them, through an always-on daemon.

Out of scope:

- Chrome, `browser_open`, the `devtools_*` tools, `contrib/README.md` and the "Chrome by code" part of `skills/hyprcage/SKILL.md`. The human owns them.
- OS tools (sub-project C).
- A privacy filter. The human decided that the agent reads everything.

## Success criteria

1. An agent reads and acts on a GTK, a Qt and an Electron window on the desktop without a screenshot.
2. An agent reads a window on a hidden workspace without a change of the human's visible workspace.
3. A click through the pointer puts the cursor back where it was.
4. `notify_wait` returns a notification that arrives during the wait. `notify_list` returns notifications from before the agent session started.
5. Every desktop tool refuses while hyprlock runs.
6. No E2E step shows on the human's visible screens.

## Decisions (brainstorm 2026-09-30)

1. **Use case: unattended jobs.** The human is often away. hypridle is off, so the session does not lock by itself.
2. **No privacy filter.** Screenshots, snapshots and notifications show everything.
3. **Cursor rule: act without the cursor first.** The order is AT-SPI, then CDP, then the real pointer. The pointer path restores the cursor position after each action. A brief cursor jump is accepted.
4. **Workspaces: silent by default.** Tools never change the visible workspace, with one exception: `desktop_workspace` changes it on request.
5. **Notifications: read, act and wait.** An always-on daemon keeps them for 48 hours.
6. **Tool surface: `screen:"desktop"` on the existing tools.** This follows the `record_start target:"desktop"` precedent. No parallel `desktop_*` set and no second MCP server.
7. **E2E host: a nested Hyprland on a hidden special workspace.** See "Evidence".

## Evidence from the spike on 2026-09-30 (verified)

- Monitor: one output `eDP-1`, 1920x1080 at scale 1.5 (1280x720 logical).
- Hyprland 0.56.2 announces `hyprland_toplevel_export_manager_v1`, `zwlr_screencopy_manager_v1`, `zwlr_virtual_pointer_manager_v1`, `ext_foreign_toplevel_image_capture_source_manager_v1` and `ext_image_copy_capture_manager_v1`.
- swaync 0.12.6 exposes `LatestInvokeAction` on `org.erikreider.swaync.cc`. It has no method to invoke an action by notification ID. `swaync-client -a` invokes an action on the latest notification only.
- At spike time, `org.freedesktop.Notifications` was activatable and had no owner. D-Bus starts swaync on the first `Notify` call.
- The AT-SPI bus `org.a11y.Bus` runs.
- A nested Hyprland 0.56 needs `xdg_wm_base` version 6. cage 0.3.1 (wlroots 0.20), weston 15 (headless) and sway 1.12 (headless) offer version 5 only, so Hyprland crashes in `CBackend::create()` inside each of them.
- A nested Hyprland runs as a window of the human's Hyprland. This call starts it on a hidden special workspace, and the visible workspace stays unchanged:
  `hl.exec_cmd("env -u HYPRLAND_INSTANCE_SIGNATURE Hyprland -c <conf>", { workspace = "special:hyprcage-e2e silent", no_initial_focus = true })`.
  The nested instance gets its own signature under `/run/user/1000/hypr/` and one monitor `WAYLAND-1`.
- The human's Hyprland ran with `--safe-mode` at spike time. This has no effect on the design.

## Verified in task 1 (2026-09-30)

Checks ran on the nested Hyprland that `tests/nested-hypr.sh` starts, except where a line names the human's instance.

1. **Toplevel export of a hidden window:** not checked in task 1. The frame events of `hyprland_toplevel_export_frame_v1` use other opcodes than screencopy, so a probe needs the generated bindings. Task 3 checks it live.
2. **Virtual pointer:** the `wl` virtual pointer moves the nested cursor to the requested position, and `movecursor` restores it. On the human's instance (1920x1080 at scale 1.5), `wl.Client.OutputSize` returns 960x540, because `wl_output.scale` is the integer 2. The logical size is 1280x720. The desktop pointer therefore takes its extent from the logical layout of `hyprctl monitors`, not from `OutputSize`. To verify: that Hyprland maps `motion_absolute` over the whole layout at a fractional scale. The nested output ignores scale changes, so only the human's instance can show it.
3. **`EditableText.InsertText`:** works in the Thunar location entry (GTK3) and in a Wireshark line edit (Qt). The text appears, and no key is sent.
4. **AT-SPI extents:** GTK3 and Qt report the same values for screen and window coordinates, relative to the window. The desktop AT-SPI source adds the window's `at` position from `hyprctl clients`.
5. **Notification monitor:** `dbus-monitor` (which uses `BecomeMonitor`) sees `Notify`, its method return with the ID, `ActionInvoked` and `NotificationClosed`.
6. **swaync actions:** `LatestInvokeAction u 0` invokes the first action, and `notify-send --action=ok=OK --wait` prints `ok`. swaync also has `CloseNotification u`.

Consequences for the tests:

- The notification E2E runs in `dbus-run-session`, with swaync on the nested `WAYLAND_DISPLAY`. The human's notification daemon never shows a test notification.
- The environment variable `HYPRCAGE_NOTIFY_FILE` overrides the notification file path, so that tests never write to the human's file.
- The nested monitor takes the size of its host window (1236x589 in task 1), not the configured 1280x800.

## Tool surface

### The desktop target

`screen:"desktop"` selects the human's Hyprland session. An optional `window` field takes a window address from `desktop_windows`, for example `0x55ebac116320`. Coordinates are global logical pixels of the Hyprland layout.

The environment variable `HYPRCAGE_DESKTOP_INSTANCE` names another Hyprland instance signature. Only the E2E uses it, to point the desktop tools at the nested instance.

### Changed tools

| Tool                                              | Change with `screen:"desktop"`                                                                                                                                                                  |
| ------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `screenshot`                                      | Without `window`: screencopy of the visible outputs. With `window`: toplevel export of that window, on any workspace.                                                                           |
| `snapshot`                                        | With `window`: that window. Without `window`: every window on a visible workspace. The header lists each window address.                                                                        |
| `find`                                            | Same scope as `snapshot`.                                                                                                                                                                       |
| `act`                                             | Same ops as on an agent screen. The input order follows decision 3. See "Act on the desktop".                                                                                                   |
| `click`, `double_click`, `move`, `scroll`, `drag` | The pointer path, with a cursor restore.                                                                                                                                                        |
| `app_launch`                                      | Runs the command through `ExecRules` with `workspace` (default: the current workspace, `silent`). Returns the pid and the window address once the window maps. `debug` stays agent-screen only. |

`type`, `key`, `wait`, `batch`, `windows`, `app_close`, `clipboard_*` and `mirror` refuse `screen:"desktop"` with `unsupported_input`. The hint names the desktop equivalent (`desktop_type`, `desktop_key`, `desktop_windows`).

### New tools

- `desktop_workspace {workspace}`: switch the human's visible workspace. This is the only tool that changes the view.
- `notify_list {since?, app?, match?}`: list the stored notifications, newest first. Each entry has an ID, the app, the summary, the body, the actions, the time and a closed flag.
- `notify_act {id, action?}`: without `action`, close the notification (`CloseNotification`). With `action`, invoke it through swaync `LatestInvokeAction`. The ID must be the latest open notification, or the tool refuses with `no_action`.
- `notify_wait {app?, match?, timeout_ms}`: wait for a new notification that matches, and return it.

The tool count goes from 35 to 39.

### Existing desktop tools

`desktop_windows`, `desktop_focus`, `desktop_move`, `desktop_type` and `desktop_key` stay as they are.

## Architecture

### Desktop connection (`internal/desktop`)

- `Conn` opens hyprcage's `wl` client on the Wayland socket of the desktop instance. It binds screencopy, toplevel export and the virtual pointer.
- `Visible(addr)` reports whether the window's workspace shows on a monitor, from `hyprctl monitors` and `hyprctl clients`.
- `Pointer` reads `cursorpos`, sends the input through the virtual pointer, then restores the position with `MoveCursorCmd`. It restores the position also when the input fails. It refuses with `window_hidden` when the target window is not visible.

### Perception on the desktop (`internal/perceive`)

A desktop target gets a Source through the existing `Choose` order:

1. **CDP** when the window's PID owns a listening DevTools port. `screen.OwnsPort` gives the check.
2. **AT-SPI** filtered to the window's PID. Without `window`, the filter is the PIDs of the visible windows. Coordinates come from AT-SPI screen extents, with no cage offset.
3. **OCR** on the toplevel-export image of the window, or on the screencopy image without `window`.

The ref table key is `desktop:<address>`, or `desktop:*` without `window`.

### Act on the desktop

For each op, the first path that can do it wins:

| Op                | 1. No cursor, no focus           | 2. CDP                     | 3. Fallback                 |
| ----------------- | -------------------------------- | -------------------------- | --------------------------- |
| `click`           | AT-SPI `DoAction`                | `Input.dispatchMouseEvent` | Pointer, with restore       |
| `type`            | AT-SPI `EditableText.InsertText` | `Input.insertText`         | `send_shortcut`, key by key |
| `key`             | none                             | `Input.dispatchKeyEvent`   | `send_shortcut`             |
| `hover`, `scroll` | none                             | CDP mouse events           | Pointer, with restore       |

The act result names the path that ran, for example `path=atspi`.

### Notification daemon (`internal/notifyd`)

- `hyprcage notifyd` runs as the systemd user unit `hyprcage-notifyd.service`. `install.sh` and `hyprcage setup` install and enable it.
- It calls `BecomeMonitor` on the session bus. It matches `Notify` method calls with their replies (for the ID), plus the `NotificationClosed` and `ActionInvoked` signals.
- It appends one JSON line per event to `~/.local/state/hyprcage/notifications.jsonl`, mode 0600.
- Once an hour, it drops the entries older than 48 hours by a rewrite to a temporary file and a rename.
- `notify_list` and `notify_wait` read the file. They do not talk to the daemon. `notify_wait` polls the file size.
- `hyprcage doctor` reports the unit state and the footprint of the daemon.

The build measures the resting footprint of the daemon and reports it. Hypothesis: about 10–15 MB RSS and no CPU at idle.

## Errors

New codes, in the style of sub-project A:

| Code            | Meaning                                                                                                          |
| --------------- | ---------------------------------------------------------------------------------------------------------------- |
| `window_hidden` | Pointer input on a window that is not on a visible workspace. Hint: `desktop_workspace` first.                   |
| `no_action`     | `notify_act` on a closed notification, an unknown action, or an action on a notification that is not the latest. |
| `notifyd_down`  | The notification file is missing or the unit does not run. Hint: `systemctl --user start hyprcage-notifyd`.      |

Existing codes keep their meaning: `session_locked` for every desktop tool while hyprlock runs, `invalid_address`, `stale_ref`, `ref_occluded`, `ref_offscreen`, `no_source` and `hyprland_unreachable`.

## Dependencies

- No new Go module. `godbus/dbus/v5` already serves AT-SPI.
- New protocol bindings in `internal/wl`: `hyprland_toplevel_export_v1`, generated with the existing generator.
- No new system package.

## Testing

- **Task 1** runs the checks of "To verify in task 1" on the nested instance.
- **Unit tests** (Go), with fakes for Hyprland IPC, `wl` and the D-Bus tree:
  - the act path order and the `path=` result;
  - cursor restore after a failed input;
  - `window_hidden`, `session_locked` and `no_action`;
  - the notifyd event-to-line conversion, the 48-hour prune and the `notify_wait` match.
- **Smoke script** `tests/smoke-desktop.sh`: start the nested Hyprland on `special:hyprcage-e2e`, set `HYPRCAGE_DESKTOP_INSTANCE`, and run one step per criterion.
- **E2E over MCP stdio**, against the nested instance only:
  - Thunar: a `snapshot`, then `act click` on a toolbar item through AT-SPI.
  - A GTK entry: `act type` through `InsertText`, with the cursor and focus unchanged.
  - Wireshark: Help → About.
  - A canvas-only app: an OCR `find`, then a pointer click with the cursor restored.
  - A hidden-workspace window: a `screenshot` through toplevel export.
  - `notify-send` with an action button: `notify_wait`, `notify_list`, `notify_act`.

  The E2E counts screenshots and checks that the human's visible workspace never changes. If the nested host fails, the controller stops and asks the human before any test touches the real desktop.

## Build flow

Worktree `../hyprcage-desktop` on branch `feat/desktop-control` off `main`. Subagent-driven, with a review per task and a rulings file `docs/superpowers/specs/2026-09-30-hyprcage-desktop-rulings.md`. Then a PR to `main`, a rebuild of the binary with `install.sh --binary-only`, a `claude plugin marketplace update hyprcage-mrwick1`, and a plugin version bump.
