# hyprcage desktop control Implementation Plan

> **For agentic workers:** Use subagent-driven-development to implement this plan task by task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an agent read and act on the human's own Hyprland desktop with the existing tools and `screen:"desktop"`, and read, act on and wait for notifications through an always-on daemon.

**Architecture:** `internal/desktop` gets a Wayland connection to the desktop instance (screencopy, toplevel export, virtual pointer) and a pointer with a cursor restore. The MCP server routes `screen:"desktop"` to it. `internal/perceive` gets a desktop target for its existing CDP, AT-SPI and OCR sources. A new `internal/notifyd` package monitors the session bus and writes a JSONL file that the notification tools read.

**Tech Stack:** Go, `github.com/modelcontextprotocol/go-sdk`, `github.com/godbus/dbus/v5`, hyprcage's own Wayland client in `internal/wl`.

**Spec:** `docs/superpowers/specs/2026-09-30-hyprcage-desktop-design.md`

**Rulings file:** `docs/superpowers/specs/2026-09-30-hyprcage-desktop-rulings.md`. The controller appends one line per ruling in the format of the sub-project A rulings file.

## Global Constraints

- `screen:"desktop"` is the target name. A new constant `desktop.Name` (`"desktop"`) holds it, and `record.Desktop` becomes an alias of it (`record` already imports `desktop`). `screen_create` refuses that name for an agent screen.
- The environment variable `HYPRCAGE_DESKTOP_INSTANCE` names the Hyprland signature of the desktop. When it is unset, the desktop is the instance that `hypr.Discover` finds. All desktop code, the existing `desktop_*` tools included, gets its instance from one function: `desktop.Instance()`.
- Every desktop tool refuses with `session_locked` while hyprlock runs. It uses the existing `lockedFn` check.
- No tool except `desktop_workspace` changes the visible workspace. No tool except the pointer path moves the cursor. The pointer path always restores the cursor position, also after an error.
- New error codes: `window_hidden`, `no_action`, `notifyd_down`.
- Coordinates on the desktop are global logical pixels of the Hyprland layout.
- The notification file is `~/.local/state/hyprcage/notifications.jsonl`, mode 0600. Its entries live for 48 hours.
- No test and no E2E step touches the human's real desktop. Live checks run on the nested Hyprland that `tests/nested-hypr.sh` starts on `special:hyprcage-e2e`.
- Do not touch `browser_open`, `internal/browser`, `internal/devtools`, `internal/cdp`, `contrib/README.md`, or the "Chrome by code" part of `skills/hyprcage/SKILL.md`.
- Existing tools keep their behavior, and their tests keep passing. Run `go test ./...` and `go vet ./...` before each commit.
- Commits use the repo-local identity `Arjun KR <arjunkrishnaraj123@gmail.com>` and carry no AI attribution.

## Review Focus

1. **The cursor restore.** A failed click, a failed drag or a panic between the press and the release must still restore the position and release the button. Task 4 adds `TestPointerRestoresOnError` and `TestDragReleasesOnError`.
2. **The visible workspace.** `app_launch`, `screenshot`, `snapshot` and `act` on a hidden window must never switch the workspace. Task 11 checks the active workspace of the human's instance before and after every E2E step.
3. **Refs across targets.** A ref from `desktop:0xA` must never resolve on `desktop:0xB` or on an agent screen. Task 5 adds `TestDesktopTablesAreSeparate`.
4. **AT-SPI coordinates of a window that is not at 0,0.** Task 1 records whether AT-SPI gives screen or window extents on Hyprland. Task 5 adds the window offset when the extents are window-relative, and `TestATSPIDesktopOffset` pins it.
5. **The notification file.** A crash during the prune must never lose the file: the prune writes a temporary file and renames it. `TestPruneKeepsFileOnError` pins it. The file mode is 0600.

---

### Task 1: Verify the spec's hypotheses (controller)

This task produces facts, a vendored protocol file and the nested-instance script. It runs in the controller, not a subagent.

**Files:**

- Create: `tests/nested-hypr.sh`, `protocols/hyprland-toplevel-export-v1.xml`
- Modify: the spec's "To verify in task 1" section, with the results

`tests/nested-hypr.sh start` writes a minimal `hyprland.conf` (one 1280x800 monitor, no logo, no splash) to a temporary directory. It starts Hyprland through `hl.exec_cmd` (Lua config) or `dispatch exec` (classic config) with `workspace = "special:hyprcage-e2e silent"` and `no_initial_focus = true`. It prints the new signature. `tests/nested-hypr.sh stop <signature>` sends `dispatch exit` to that instance.

- [ ] **Step 1:** Write `tests/nested-hypr.sh`. Run `start`, and check that `hyprctl activeworkspace` of the human's instance does not change.
- [ ] **Step 2:** Vendor `hyprland-toplevel-export-v1.xml` from `github.com/hyprwm/hyprland-protocols` (BSD-3-Clause; keep the license header).
- [ ] **Step 3:** On the nested instance, check each item with a throwaway probe in `/tmp`:
  1. Toplevel export captures a window on a hidden workspace of the nested instance.
  2. The `wl` virtual pointer moves the nested cursor, and `movecursor` restores the position. Record how `motion_absolute` maps to logical coordinates when the extent is the output size.
  3. `EditableText.InsertText` works in a GTK entry (`zenity --entry`) and in a Qt line edit.
  4. AT-SPI extents of a window at a non-zero position: screen or window coordinates.
  5. A D-Bus `BecomeMonitor` connection sees `Notify` calls, their replies, `NotificationClosed` and `ActionInvoked`.
  6. swaync `LatestInvokeAction` invokes the action, and `notify-send --action` prints the action name.
- [ ] **Step 4:** Record each result in the spec. A failed item amends the spec and this plan, and the rulings file records it.
- [ ] **Step 5:** Stop the nested instance. Delete the probes. Commit: `desktop: verify the spec's hypotheses and add the nested Hyprland script`.

---

### Task 2: The desktop target and its instance

**Files:**

- Modify: `internal/desktop/desktop.go`, `internal/screen/errors.go`, `internal/mcpserver/server_desktop.go`, `internal/mcpserver/server.go`
- Test: `internal/desktop/desktop_test.go`, `internal/mcpserver/server_test.go`

**Interfaces:**

```go
package desktop

// Instance returns the desktop's Hyprland instance: HYPRCAGE_DESKTOP_INSTANCE
// when set, otherwise fallback. It refuses a signature with a path separator.
func Instance(fallback *hypr.Instance) (*hypr.Instance, error)

// IsDesktop reports whether a screen argument names the desktop.
func IsDesktop(name string) bool
```

```go
package screen

const (
	CodeWindowHidden Code = "window_hidden"
	CodeNoAction     Code = "no_action"
	CodeNotifydDown  Code = "notifyd_down"
)
```

`Server.desktop()` uses `desktop.Instance` and builds the driver from that instance. The tools `type`, `key`, `wait`, `batch`, `windows`, `app_close`, `clipboard_get`, `clipboard_set` and `mirror` refuse `screen:"desktop"` with `unsupported_input`. The hint names the desktop equivalent.

- [ ] **Step 1: Write the failing tests:**
  - `TestInstanceOverride`: with the variable set to a live fake instance directory, `Instance` returns it. With a value that has `/`, it returns an error.
  - `TestInstanceFallback`: without the variable, `Instance` returns the fallback.
  - `TestDesktopRefusedByScreenTools`: `key` with `screen:"desktop"` returns `unsupported_input` and a hint with `desktop_key`.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3:** Implement. Keep the existing `desktop_*` tools working through `Server.desktop()`.
- [ ] **Step 4:** Run `go test ./...`. Expected: PASS.
- [ ] **Step 5:** Commit: `desktop: add the desktop target and the instance override`.

---

### Task 3: Toplevel export and the desktop screenshot

Depends on: task 2.

**Files:**

- Modify: `internal/wl/gen/main.go` (add the XML), `internal/wl/protocols_gen.go` (regenerated), `internal/wl/wl.go`, `internal/wl/capture.go`
- Create: `internal/desktop/conn.go`
- Modify: `internal/mcpserver/server.go` (`screenshot`)
- Test: `internal/wl/wl_test.go`, `internal/desktop/conn_test.go`

**Interfaces:**

```go
package wl

// CaptureToplevel captures one window by its Hyprland address (the low 32
// bits are the handle of hyprland_toplevel_export_manager_v1), on any workspace.
func (c *Client) CaptureToplevel(handle uint32, overlayCursor bool) (*image.RGBA, error)
```

```go
package desktop

// Conn is a Wayland connection to the desktop instance.
type Conn struct {
	H  *hypr.Instance
	CL *wl.Client
}

func Open(h *hypr.Instance) (*Conn, error)             // wl.Connect on h.WaylandDisplay()
func (c *Conn) Window(addr string) (hypr.Client, error) // CheckAddress, then the client with that address
func (c *Conn) Visible(addr string) (bool, error)      // the window's workspace is active on a monitor
func (c *Conn) Shot(addr string, o screen.ShotOptions) (*screen.ShotResult, error)
```

`Shot` without an address uses `screen.Shot` on the connection. With an address, it uses `CaptureToplevel` and the same scaling and encoding code as `screen.Shot`. Extract that code into a shared function in `internal/screen/capture.go` rather than copying it.

- [ ] **Step 1: Write the failing tests:**
  - `TestCaptureToplevelRequest`: the generated request carries the handle and the cursor flag (wire test in the style of `wire_test.go`).
  - `TestVisible`: a fake client on workspace 3 with monitor workspaces {1} is not visible. On {3}, it is visible. A window on the monitor's special workspace is visible.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3:** Implement. Run `go generate ./internal/wl`.
- [ ] **Step 4:** Run `go test ./...`. Then run `screenshot screen:"desktop" window:<addr>` against the nested instance on a hidden window. Expected: an image of the window.
- [ ] **Step 5:** Commit: `desktop: capture the desktop and single windows through toplevel export`.

---

### Task 4: The pointer with a cursor restore

Depends on: task 3.

**Files:**

- Create: `internal/desktop/pointer.go`
- Modify: `internal/mcpserver/server.go` (`click`, `double_click`, `move`, `scroll`, `drag`)
- Test: `internal/desktop/pointer_test.go`

**Interfaces:**

```go
package desktop

// Pointer runs pointer input on the desktop and puts the cursor back.
type Pointer struct {
	C   *Conn
	D   hypr.ConfigDriver
	in  pointerInput // *wl.Client outside tests
}

// Do reads cursorpos, checks that addr (when not empty) is visible, runs fn,
// then restores the cursor with MoveCursorCmd, also when fn fails. It
// releases every pressed button before the restore.
func (p Pointer) Do(addr string, fn func(in pointerInput) error) error
```

`pointerInput` is the subset of `*wl.Client` that `screen.Click`, `Scroll` and `drag` use. `move` on the desktop moves the cursor and does not restore it, because a hover needs the cursor to stay.

- [ ] **Step 1: Write the failing tests:**
  - `TestPointerRestoresOnError`: `fn` fails. The fake IPC saw `MoveCursorCmd` with the saved position.
  - `TestDragReleasesOnError`: the move between the press and the release fails. The fake saw a release before the restore.
  - `TestPointerRefusesHidden`: a window on a hidden workspace returns `window_hidden`, and no input is sent.
  - `TestPointerLocked`: `lockedFn` returns true. The result is `session_locked`, and no input is sent.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3:** Implement. Use the coordinate mapping from task 1, item 2.
- [ ] **Step 4:** Run `go test ./...`. Then click a button of `zenity --question` on the nested instance. Expected: the dialog closes, and the nested cursor is back at its start position.
- [ ] **Step 5:** Commit: `desktop: add pointer input with a cursor restore`.

---

### Task 5: Perception on the desktop

Depends on: task 3. Runs in parallel with task 4.

**Files:**

- Modify: `internal/perceive/source.go`, `internal/perceive/atspi.go`, `internal/perceive/ocr.go`
- Create: `internal/perceive/desktop.go`
- Modify: `internal/mcpserver/server_perceive.go`
- Test: `internal/perceive/desktop_test.go`, `internal/mcpserver/server_test.go`

**Interfaces:**

```go
package perceive

// Target is what a desktop snapshot reads: one window, or every visible one.
type Target struct {
	Windows []hypr.Client // one entry with a window address, else the visible ones
}

// ChooseDesktop picks CDP when the window's PID owns a listening DevTools
// port, then AT-SPI filtered to the windows' PIDs, then OCR on the window
// image (screencopy without a window). want is "auto", "cdp", "atspi" or "ocr".
func ChooseDesktop(ctx context.Context, c *desktop.Conn, t Target, want string) (Source, error)
```

The AT-SPI source takes the PID list of the target and, when task 1 found window-relative extents, adds each window's `at` position. The OCR source takes an image function, so that it reads either `CaptureToplevel` plus the window offset or the screencopy image. The ref table key is `desktop:<address>`, or `desktop:*` without a window. The snapshot header adds `windows=<addr,addr>`.

- [ ] **Step 1: Write the failing tests:**
  - `TestATSPIDesktopOffset`: a node at window extents (10,20) in a window at (100,200) renders at (110,220) when extents are window-relative.
  - `TestATSPIDesktopFiltersPIDs`: an application with a PID outside the target never appears.
  - `TestDesktopTablesAreSeparate`: a ref from `desktop:0xa` is unknown on `desktop:0xb` and on an agent screen.
  - `TestChooseDesktopOrder`: with a fake port owner, CDP wins. Without one, AT-SPI wins when the PID has an AT-SPI app. Otherwise OCR.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3:** Implement. Reuse `Snapshot` and `Find` unchanged.
- [ ] **Step 4:** Run `go test ./...`. Then run `snapshot screen:"desktop" window:<thunar>` on the nested instance. Expected: `source=atspi` with the toolbar items.
- [ ] **Step 5:** Commit: `perceive: read the desktop's windows through CDP, AT-SPI and OCR`.

---

### Task 6: `act` on the desktop

Depends on: tasks 4 and 5.

**Files:**

- Modify: `internal/perceive/act.go`, `internal/perceive/atspi.go`, `internal/perceive/cdp.go` (only the Source methods that `act` calls)
- Create: `internal/perceive/desktop_act.go`
- Modify: `internal/mcpserver/server_perceive.go`
- Test: `internal/perceive/desktop_act_test.go`

**Interfaces:**

```go
package perceive

// inserter is a Source that inserts text into a node without focus or keys.
// Only AT-SPI implements it (EditableText.InsertText at the caret, or at the end).
type inserter interface {
	Insert(ctx context.Context, key, text string) error
}
```

`act` on the desktop follows the spec's "Act on the desktop" table. The `inputter` for the desktop wraps the task 4 `Pointer` for pointer ops and `send_shortcut` (the existing `desktop.Desktop.Type` and `Key`) for key ops. The result text starts with `path=<atspi|cdp|pointer|shortcut>`.

- [ ] **Step 1: Write the failing tests:**
  - `TestDesktopActClickPrefersPress`: an AT-SPI node with an action gets `Press`, and the fake pointer sees no input.
  - `TestDesktopActTypeInserts`: an AT-SPI textbox gets `Insert`, and no key is sent.
  - `TestDesktopActFallsBackToPointer`: an OCR node gets a pointer click through `Pointer.Do`.
  - `TestDesktopActPathLine`: the result starts with `path=`.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test ./...`. Then run `act type` into a `zenity --entry` field on the nested instance. Expected: `path=atspi`, the text in the field, and an unchanged nested cursor.
- [ ] **Step 5:** Commit: `perceive: act on the desktop without the cursor first`.

---

### Task 7: `app_launch` on the desktop and `desktop_workspace`

Depends on: task 2. Runs in parallel with tasks 3 to 6.

**Files:**

- Modify: `internal/mcpserver/server.go` (`appLaunch`, `launchIn`), `internal/mcpserver/server_desktop.go`, `internal/desktop/desktop.go`
- Test: `internal/desktop/desktop_test.go`, `internal/mcpserver/server_test.go`

**Interfaces:**

```go
package desktop

// Launch runs argv on the desktop through the driver's Exec with workspace
// "<ws> silent" (default: the active workspace) and no_initial_focus. It
// waits up to wait for a client whose PID is the launched PID or its child.
func (d Desktop) Launch(argv []string, env map[string]string, cwd string, ws int, wait time.Duration) (pid int, addr string, err error)

// Workspace switches the human's visible workspace.
func (d Desktop) Workspace(ws int) error
```

`launchIn` gets `workspace int`. `debug` with `screen:"desktop"` returns `unsupported_input`. The command goes through a `sh -c` with every argument quoted, so that no argument is parsed as shell syntax.

- [ ] **Step 1: Write the failing tests:**
  - `TestLaunchQuotesArgv`: an argument `a b;rm -rf x` reaches the Exec command as one quoted word.
  - `TestLaunchSilentWorkspace`: the Exec rules carry `workspace = "4 silent"` and `no_initial_focus = true`.
  - `TestWorkspaceCommand`: `Workspace(2)` sends `WorkspaceCmd(2)`. `Workspace(0)` is refused.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3:** Implement. Register `desktop_workspace`.
- [ ] **Step 4:** Run `go test ./...`. Then launch `thunar` on nested workspace 2 while nested workspace 1 is active. Expected: a window address, and workspace 1 stays active.
- [ ] **Step 5:** Commit: `desktop: launch apps on the desktop and switch workspaces`.

---

### Task 8: The notification daemon

Independent of tasks 2 to 7.

**Files:**

- Create: `internal/notifyd/notifyd.go`, `internal/notifyd/store.go`, `internal/cli/notifyd.go`, `contrib/hyprcage-notifyd.service`
- Modify: `internal/cli/cli.go` (register the subcommand), `internal/cli/doctor.go`, `internal/setup/*.go`, `install.sh`
- Test: `internal/notifyd/store_test.go`, `internal/notifyd/notifyd_test.go`

**Interfaces:**

```go
package notifyd

type Entry struct {
	ID      uint32            `json:"id"`
	App     string            `json:"app"`
	Summary string            `json:"summary"`
	Body    string            `json:"body"`
	Actions map[string]string `json:"actions,omitempty"` // key -> label
	Time    time.Time         `json:"time"`
	Event   string            `json:"event"`            // "notify", "closed" or "action"
	Action  string            `json:"action,omitempty"` // the invoked key, for "action"
}

func Path() string                          // $HYPRCAGE_NOTIFY_FILE, else ~/.local/state/hyprcage/notifications.jsonl
func Append(path string, e Entry) error      // O_APPEND, mode 0600
func Prune(path string, now time.Time) error // drop entries older than 48 h: temp file, then rename
func Read(path string) ([]Entry, error)      // skips a torn last line
func Run(ctx context.Context) error          // BecomeMonitor, append events, prune hourly
```

`Run` pairs each `Notify` call with its method return by serial, to get the ID. The unit file runs `%h/.local/bin/hyprcage notifyd` with `Restart=on-failure`. `install.sh` and `hyprcage setup` copy it to `~/.config/systemd/user/` and run `systemctl --user enable --now hyprcage-notifyd.service`. `hyprcage doctor` prints the unit state and the RSS of the daemon.

- [ ] **Step 1: Write the failing tests:**
  - `TestAppendMode`: a new file has mode 0600.
  - `TestPrune`: entries at now−49 h go, entries at now−47 h stay.
  - `TestPruneKeepsFileOnError`: when the temp file cannot be created, the old file is unchanged.
  - `TestReadTornLine`: a file whose last line is cut returns the complete entries only.
  - `TestPairNotifyReply`: a `Notify` call with serial 7 and a return with reply serial 7 and body `uint32(42)` give an entry with ID 42.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test ./...`. Then run `hyprcage notifyd` in the foreground, send `notify-send hi there`, and read the file. Expected: one `notify` entry with an ID. Measure the resting RSS after 60 s, and record it in the rulings file.
- [ ] **Step 5:** Commit: `notifyd: keep 48 hours of desktop notifications`.

---

### Task 9: The notification tools

Depends on: task 8.

**Files:**

- Create: `internal/mcpserver/server_notify.go`, `internal/notifyd/query.go`
- Test: `internal/notifyd/query_test.go`, `internal/mcpserver/server_test.go`

**Interfaces:**

```go
package notifyd

type Notification struct {
	Entry
	Closed bool `json:"closed"`
}

// List folds the events into notifications, newest first, filtered by since,
// app (exact) and match (regexp on the summary and body).
func List(entries []Entry, since time.Time, app string, match *regexp.Regexp) []Notification

// Latest returns the newest notification that is not closed.
func Latest(entries []Entry) (Notification, bool)
```

- `notify_list {since?, app?, match?}` returns `List` as JSON.
- `notify_act {id, action?}`: without `action`, call `CloseNotification(id)` on `org.freedesktop.Notifications`. With `action`, check that `id` is `Latest` and has that action key. Then call swaync `LatestInvokeAction` with the index of the action. Otherwise refuse with `no_action`.
- `notify_wait {app?, match?, timeout_ms}` polls the file size every 250 ms and returns the first new matching notification. The timeout is at most 10 min. It returns `timeout` when nothing matches.
- A missing file or an inactive unit returns `notifyd_down`.

- [ ] **Step 1: Write the failing tests:**
  - `TestListFoldsClosed`: a `notify` plus a `closed` with the same ID gives one notification with `closed=true`.
  - `TestListFilters`: `app` and `match` filter as specified.
  - `TestLatestSkipsClosed`.
  - `TestActRefusesNotLatest`: `notify_act` with an older ID and an action returns `no_action`.
  - `TestNotifydDown`: a missing file returns `notifyd_down`.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test ./...`. With the daemon running, send `notify-send --action=ok=OK --wait hi`, and run `notify_act` with the action `ok`. Expected: `notify-send` prints `ok`.
- [ ] **Step 5:** Commit: `mcp: add notify_list, notify_act and notify_wait`.

---

### Task 10: Docs and agent instructions

Depends on: tasks 2 to 9.

**Files:**

- Modify: `internal/mcpserver/server.go` (the `instructions` constant), `skills/hyprcage/SKILL.md` (only a new "The human's desktop" section; the "Chrome by code" section stays as it is), `README.md` (the tools list)

The text states the cursor rule, the silent-workspace rule, the `window_hidden` hint, the 48-hour notification store and `notify_act` "latest only". It follows the ASD-STE100 rules.

- [ ] **Step 1:** Update the three files.
- [ ] **Step 2:** Run `go test ./...`. The server test that pins the instructions text gets the new text.
- [ ] **Step 3:** Commit: `docs: describe desktop control and notifications`.

---

### Task 11: Smoke script and E2E (controller)

Depends on: tasks 1 to 10.

**Files:**

- Create: `tests/smoke-desktop.sh`, `tests/e2e-desktop.sh`

`smoke-desktop.sh` starts the nested instance, exports `HYPRCAGE_DESKTOP_INSTANCE`, and runs one step per success criterion through the CLI or MCP stdio. It stops the nested instance on exit, also on failure. `e2e-desktop.sh` drives the MCP server over stdio for the spec's E2E list, counts screenshots, and compares the active workspace of the human's instance before and after each step.

- [ ] **Step 1:** Write both scripts.
- [ ] **Step 2:** Build the binary from the worktree. Run `tests/smoke-desktop.sh`. Expected: all steps pass.
- [ ] **Step 3:** Run `tests/e2e-desktop.sh`. Expected: all tasks pass, screenshots only where a task asks for one, and the human's workspace unchanged throughout.
- [ ] **Step 4:** Run a final review of the whole branch through a review subagent. Fix its findings.
- [ ] **Step 5:** Commit: `tests: add the desktop smoke and E2E scripts`. Push, open the PR to `main`, and report the evidence.

## Execution waves

1. Task 1 (controller).
2. Task 2.
3. Tasks 3, 7 and 8 in parallel.
4. Tasks 4, 5 and 9 in parallel (4 and 5 after 3; 9 after 8).
5. Task 6.
6. Task 10.
7. Task 11 (controller).

Skipped: CLI twins for the desktop tools. Add them when a shell script needs desktop control without MCP.
