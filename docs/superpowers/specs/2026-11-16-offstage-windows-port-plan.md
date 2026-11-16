# offstage on Windows: port plan

Date: 2026-11-16
Status: draft, for review
Base branch: `main` (at `ebceb52`)

## Goal

An agent on Windows gets a screen of its own. The agent launches applications on that screen, reads the screen as text, and acts on it. The human's cursor, focus and windows stay untouched. The same plugin, `offstage@offstage-mrwick1`, serves Linux and Windows.

## Decisions (2026-11-16)

1. Isolation model: v1 needs none. A headless Chrome has no window (spike C). For native apps after v1: spike B, the hidden desktop. Spike A (loopback RDP session) failed: Windows 11 Pro refuses a second interactive session. See the rulings file.
2. Code home: this repository. Windows code goes in `*_windows.go` files behind interfaces. Linux behavior does not change.
3. v1 scope (changed 2026-11-16): a headless browser screen. A screen on Windows is one headless Chrome. `screenshot`, `click`, `type`, `key`, `scroll`, `drag`, `move` and `record_*` go through CDP. `snapshot`, `act`, `find`, `read_page` and `devtools_*` work as on Linux. Native apps, the hidden desktop, `desktop_*` and `notify_*` come after v1.
4. Test machine: a Windows 11 Pro VM under QEMU/KVM on archMachine. GPU behavior in a VM differs from real hardware. Each GPU result from the VM is marked "VM only" until a real machine confirms it.

## Non-goals for v1

- Native apps (`app_launch` of anything but Chrome), `desktop_*`, `notify_*`, `mirror`, waybar.
- Windows 10.
- ARM64 Windows.
- Isolation from the system. A Linux screen isolates the graphical session only. A Windows screen does the same.

## What exists today

Measured on `origin/main` with `grep` for Linux-only imports and calls (`x/sys/unix`, `syscall`, `godbus`, `/proc`, `systemctl`, `hyprctl`, `XDG_RUNTIME_DIR`, Wayland).

| Package | Lines | Linux-bound | Windows work |
| --- | --- | --- | --- |
| `mcpserver` | 2863 | 4 of 10 files | Keep. Hide v1-excluded tools on Windows. |
| `perceive` | 3225 | 8 of 13 files | Keep refs, act, find, verify, CDP. Add a UIA source next to the AT-SPI source. |
| `screen` | 2513 | 12 of 21 files | New Windows backend. |
| `wl` | 3846 | 5 of 10 files | Not used on Windows. |
| `cli` | 2065 | 7 of 23 files | Split per platform. |
| `devtools`, `cdp` | 1138 | 0 | Keep. |
| `browser` | 427 | 2 of 2 files | Port the Chrome launch and profile paths. |
| `config`, `audit`, `guard`, `shellq`, `version` | 823 | 0 | Keep. Add Windows default paths in `config`. |
| `registry`, `session`, `lock` | 431 | all | Port: state directory, process liveness, file lock. |
| `sysd` | 135 | all | Replace with Job Objects and a safety-timer process. |
| `mirror` | 358 | all | Replace with a Win32 window that paints frames. Optional in v1. |
| `setup` | 229 | all | Replace with Windows prerequisite checks. |
| `hypr`, `desktop`, `notifyd`, `record` | | most | Out of v1. |

Main fact: `screen` exposes `*wl.Client` to its callers (`screen.Open`, `screen.Shot`, `screen.Keys`). A Windows backend needs a seam above `wl` first.

## Isolation spikes

Each spike is a small Go program in `tests/windows/spike-<name>/`. It is not part of the binary. It runs in the VM. Each spike answers the same checks.

### Checks

1. Chrome with `--remote-debugging-port` opens a page on the agent screen. A capture shows the page, not a black frame. Run once with GPU on and once with `--disable-gpu`.
2. Notepad and one WinUI or UWP app (Settings or Calculator) render in a capture.
3. A click and typed text reach the app on the agent screen. The human's cursor position and foreground window stay the same, measured before and after with `GetCursorPos` and `GetForegroundWindow`.
4. UI Automation reads the element tree of an app on the agent screen from the offstage process.
5. Two agent screens run at the same time and stay apart.
6. Closing the agent screen ends all its processes.
7. The capture of one frame takes less than 200 ms at 1280x800.
8. Required: Windows edition, admin rights, registry changes, extra software.

### Spike A: second session over loopback RDP

A local account per agent screen (or one shared agent account) logs on through RDP to `127.0.0.2`. The session has its own desktop, cursor and focus. offstage starts a small helper inside that session. The helper captures with Windows.Graphics.Capture or DXGI desktop duplication, injects input with `SendInput`, and reads UIA. It talks to offstage over a named pipe.

To verify:

- Windows 11 Pro allows only one interactive session per user and, by default, one remote session while the console user is logged on. Find which policy or edition allows the console session and one loopback session at the same time.
- The RDP session size can be set to the screen size (1280x800) and stays the same after the client window minimizes. A minimized RDP client can stop the session's rendering.
- The session must keep running without a visible RDP client window, or the client window must be hidden from the human.
- The cost per session in memory and logon time.

### Spike B: hidden desktop

`CreateDesktop` makes a desktop in the human's window station. Apps start on it with `STARTUPINFO.lpDesktop`. Nothing on it shows on the human's screen.

To verify:

- DWM composes only the input desktop. Captures of a hidden desktop may need `PrintWindow` per window, and GPU-rendered apps (Chrome, Electron, WinUI) may draw black.
- `SendInput` reaches only the input desktop. Input may have to go through `PostMessage`, which many apps ignore or handle differently.
- UIA on a hidden desktop needs the reading thread on that desktop (`SetThreadDesktop`).
- No admin rights and no edition limits.

### How the result decides

- Spike A passes checks 1 to 6: choose A.
- Spike A fails check 3 or check 8 makes it impractical, and spike B passes checks 1 to 4 with Chrome: choose B and record what does not render.
- Both fail: consider a VM per screen (Hyper-V or Windows Sandbox) as option C, and write a new plan for it.

## Phases

Each phase is one PR. Each PR keeps `make test` and the Linux VM suite green.

### Phase 0: test VM

- Windows 11 guest under QEMU/KVM on archMachine, from the standard multi-edition ISO, not activated. The ISO comes from Microsoft through `quickget windows 11`. Pro runs spike A. Home runs spike B again, because most users have Home.
- Windows 11 Home has no Remote Desktop host. Spike A therefore cannot work on Home without third-party patches. Spike B has no edition limit.
- Guest size: 4 vCPUs, 8 GB RAM, 80 GB qcow2 disk on `/data`. virtio drivers, OpenSSH server, Go, Git, Chrome.
- A script in `tests/windows/vm.sh` starts the VM headless and forwards SSH. Agents run commands in the guest over SSH.
- Snapshot after setup, so each test run can restore a clean guest.
- Note: archMachine has qemu but no libvirt. The script uses plain `qemu-system-x86_64`.
- An activation key is not needed for tests. A guest that is not activated shows a watermark and locks personalization only.

### Phase 1: spikes A and B

Run both spikes. Write the results as a rulings file next to this plan. Choose the isolation model.

Status: phase 0 and phase 1 are done. Spike C (headless Chrome) changed the v1 scope. Phases 4 to 6 below are the v1 path. The old screen-backend phases move to "After v1".

### Phase 2: backend seam on Linux

- Add an interface in `internal/screen` for the operations the MCP tools use: create, destroy, alive, capture frame, pointer, keys, type, scroll, clipboard, record.
- The Linux implementation wraps the current cage and `wl` code. No behavior change.
- A second kind of screen, "headless browser", implements the same interface with CDP only. It can run on Linux too, which gives a test path without the VM.
- `GOOS=windows go build ./...` compiles. Tools that v1 does not have answer `unsupported_platform`.
- Check: `make test` and the Linux VM suite pass unchanged. The headless browser screen passes its own tests on Linux.

### Phase 3: platform services on Windows

- `registry`: state in `%LOCALAPPDATA%\offstage`.
- `session`: process liveness through `OpenProcess`.
- `lock`: `LockFileEx`.
- `sysd` replacement: a Job Object per screen, so closing the job ends every process. A detached safety-timer process replaces the systemd timer.
- `config`: Windows default path `%APPDATA%\offstage\config.toml`.
- Check: unit tests run on Windows in the VM (`go test ./...`).
- Deferred: the Job Object and the safety-timer process. v1 kills with `taskkill /T /F`, which walks the tree from Chrome's main pid. When Chrome's main process dies first, its children keep no link to that pid and the tree is lost. A Job Object closes this gap and stays on the list for after v1.

### Phase 4: headless browser screen on Windows

- `screen_create` on Windows starts one headless Chrome (`--headless=new`, a DevTools port, a profile per screen, 1280x800 through `Emulation.setDeviceMetricsOverride`). A Job Object holds its processes.
- `browser_open` navigates that Chrome. The Chrome path comes from the registry or the standard install folders.
- Input: `Input.dispatchMouseEvent`, `Input.dispatchKeyEvent` and `Input.insertText`. Coordinates are page pixels.
- Capture: `Page.captureScreenshot`.
- Native pop-ups have no window. Handle them through CDP: `Page.setInterceptFileChooserDialog`, `Browser.setDownloadBehavior`, `Browser.grantPermissions`, `Page.handleJavaScriptDialog`.
- Tools that work after this phase: `screen_create`, `screen_destroy`, `screen_list`, `browser_open`, `screenshot`, `click`, `double_click`, `type`, `key`, `scroll`, `drag`, `move`, `wait`, `clipboard_get`, `clipboard_set` (page clipboard, to verify).
- Check: a Windows e2e script fills a form, follows a link and takes a screenshot. It confirms that no window appears and the human's cursor stays the same.

### Phase 5: perception and DevTools on Windows

- `snapshot`, `act`, `find`, `read_page`, `read_selection` and `watch` use the CDP source, as on Linux.
- `devtools_*` work without change, because they use CDP over a port.
- OCR is not needed in v1: a page has a DOM and an accessibility tree.
- Check: the CDP perception tests run on Windows. `devtools_eval`, `devtools_console` and `devtools_network` work on a page.

### Phase 6: recording

- `record_start` uses `Page.startScreencast` and acknowledges each frame. A recorder repeats the last frame to keep a fixed frame rate, because frames come only on repaint.
- Encoding: ffmpeg reads the frames on a pipe. To verify: whether offstage bundles ffmpeg on Windows or asks the user to install it.
- Check: a 10 s recording of a page with motion has the expected duration and frame rate.

### Phase 7: plugin and install

- `bin/offstage-mcp` gets a Windows launcher (`offstage-mcp.cmd` or a PowerShell script). `.mcp.json` picks the launcher per platform. To verify: how Claude Code on Windows resolves a plugin MCP command.
- `install.ps1`: download the Windows release, register agents, write config.
- The release workflow builds `offstage-windows-amd64.exe`.
- `skills/offstage/SKILL.md` names the tools that Windows does not have yet.
- Session hooks close screens when a Claude Code session ends, as on Linux. To verify: hooks run on Windows with the same JSON input.

### After v1

- Native apps on a hidden desktop (spike B): `app_launch`, `PrintWindow` capture, input through window messages, a UIA source in `internal/perceive`, Windows.Media.Ocr. Open items from spike B: two desktops at once, closing single-instance packaged apps, capture time over 200 ms, Calculator and Settings.
- bare as the headless browser, when its CDP support covers the v1 tools. It used 8 to 16 MiB per page in the VM, against 540 to 690 MiB for headless Chrome.
- Mirror window (Win32 window that paints frames).
- `desktop_*` on the human's desktop through UIA and `SendInput`.
- `notify_*` through `UserNotificationListener`.
- A test on real Windows hardware.

## Risks

- Headless Chrome can differ from a windowed Chrome: some sites detect headless mode and block or change the page.
- v1 drives only web pages. An agent task that needs a native app fails on Windows until the hidden desktop lands.
- A Windows VM hides GPU problems. A real machine is needed before release.
- The name "offstage" means Hyprland. A Windows release under that name may confuse users. The name stays until v1 works.
- `wl` and `screen` are large and well tested. The seam in phase 2 must not change Linux behavior. The Linux VM suite is the guard.

## Open questions for Arjun

1. Who uses the Windows build first: only you, friends, or a public release.
2. A rename before a public Windows release: yes or no.
