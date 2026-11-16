# hyprcage on Windows: port plan

Date: 2026-11-16
Status: draft, for review
Base branch: `main` (at `ebceb52`)

## Goal

An agent on Windows gets a screen of its own. The agent launches applications on that screen, reads the screen as text, and acts on it. The human's cursor, focus and windows stay untouched. The same plugin, `hyprcage@hyprcage-mrwick1`, serves Linux and Windows.

## Decisions (2026-11-16)

1. Isolation model: not chosen yet. Spike A (loopback RDP session) and spike B (hidden desktop) run first. The results decide.
2. Code home: this repository. Windows code goes in `*_windows.go` files behind interfaces. Linux behavior does not change.
3. v1 scope: agent screens only. Desktop control (`desktop_*`), notifications (`notify_*`) and recording come after v1.
4. Test machine: a Windows 11 Pro VM under QEMU/KVM on archMachine. GPU behavior in a VM differs from real hardware. Each GPU result from the VM is marked "VM only" until a real machine confirms it.

## Non-goals for v1

- `desktop_*`, `notify_*`, `record_*`, `mirror` placement rules, waybar.
- Windows 10. Windows Home, unless the chosen isolation model works there.
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
4. UI Automation reads the element tree of an app on the agent screen from the hyprcage process.
5. Two agent screens run at the same time and stay apart.
6. Closing the agent screen ends all its processes.
7. The capture of one frame takes less than 200 ms at 1280x800.
8. Required: Windows edition, admin rights, registry changes, extra software.

### Spike A: second session over loopback RDP

A local account per agent screen (or one shared agent account) logs on through RDP to `127.0.0.2`. The session has its own desktop, cursor and focus. hyprcage starts a small helper inside that session. The helper captures with Windows.Graphics.Capture or DXGI desktop duplication, injects input with `SendInput`, and reads UIA. It talks to hyprcage over a named pipe.

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

### Phase 2: backend seam on Linux

- Add an interface in `internal/screen` for the operations the MCP tools use: create, launch, destroy, alive, capture frame, pointer, keys, type, clipboard, list windows, close window, wait title.
- The Linux implementation wraps the current cage and `wl` code. No behavior change.
- `GOOS=windows go build ./...` compiles, with stubs that answer `unsupported_platform`.
- Check: `make test` and the Linux VM suite pass unchanged.

### Phase 3: platform services on Windows

- `registry`: state in `%LOCALAPPDATA%\hyprcage`.
- `session`: process liveness through `OpenProcess`.
- `lock`: `LockFileEx`.
- `sysd` replacement: a Job Object per screen, so closing the job ends every process. A detached safety-timer process replaces the systemd timer.
- `config`: Windows default path `%APPDATA%\hyprcage\config.toml`.
- Check: unit tests run on Windows in the VM (`go test ./...`).

### Phase 4: Windows screen backend

- Implement the seam with the chosen isolation model.
- Capture: Windows.Graphics.Capture or DXGI duplication for spike A, `PrintWindow` for spike B.
- Input: `SendInput` (A) or window messages (B). Unicode text through `KEYEVENTF_UNICODE`.
- Tools that work after this phase: `screen_create`, `screen_destroy`, `screen_list`, `app_launch`, `screenshot`, `click`, `double_click`, `type`, `key`, `scroll`, `drag`, `move`, `wait`, `windows`, `clipboard_get`, `clipboard_set`.
- Check: a Windows e2e script drives Notepad on an agent screen. It also confirms that the human's cursor and foreground window stayed the same.

### Phase 5: perception on Windows

- A UIA source in `internal/perceive` that implements `Source`, next to the AT-SPI source.
- CDP perception for Chrome works as on Linux.
- OCR: Windows.Media.Ocr first. tesseract is the fallback if Windows OCR gives worse results on the existing test images.
- Tools: `snapshot`, `act`, `find`, `read_page`, `read_selection`, `watch`.
- Check: the Linux perception tests that do not depend on AT-SPI run on Windows. A UIA test reads Notepad and Calculator.

### Phase 6: browser and DevTools

- `browser_open` with a Windows Chrome path and a profile per screen.
- `devtools_*` work without change, because they use CDP over a port.
- Check: `devtools_eval`, `devtools_console` and `devtools_network` on a page on an agent screen.

### Phase 7: plugin and install

- `bin/hyprcage-mcp` gets a Windows launcher (`hyprcage-mcp.cmd` or a PowerShell script). `.mcp.json` picks the launcher per platform. To verify: how Claude Code on Windows resolves a plugin MCP command.
- `install.ps1`: download the Windows release, register agents, write config.
- The release workflow builds `hyprcage-windows-amd64.exe`.
- `skills/hyprcage/SKILL.md` names the tools that Windows does not have yet.
- Session hooks close screens when a Claude Code session ends, as on Linux. To verify: hooks run on Windows with the same JSON input.

### After v1

- Mirror window (Win32 window that paints frames).
- Recording with ffmpeg `ddagrab` or `gdigrab`, or by encoding captured frames.
- `desktop_*` on the human's desktop through UIA and `SendInput`.
- `notify_*` through `UserNotificationListener`.
- A test on real Windows hardware.

## Risks

- Spike A may need edition or policy changes that most users cannot make. This limits who can install it.
- Spike B may not render Chrome. Chrome is the main app agents drive.
- A Windows VM hides GPU problems. A real machine is needed before release.
- The name "hyprcage" means Hyprland. A Windows release under that name may confuse users. The name stays until v1 works.
- `wl` and `screen` are large and well tested. The seam in phase 2 must not change Linux behavior. The Linux VM suite is the guard.

## Open questions for Arjun

1. Who uses the Windows build first: only you, friends, or a public release.
2. A rename before a public Windows release: yes or no.
