# hyprcage on Windows: spike rulings

Date: 2026-11-16
Plan: `2026-11-16-hyprcage-windows-port-plan.md`

## Spike B: hidden desktop

Setup: Windows 11 Pro build 26300 in the test VM (QEMU/KVM, `-vga std`, no GPU passthrough). Chrome 157. The spike `tests/windows/spike-hiddendesk` runs in the interactive session through a scheduled task with `/it`. A process started from SSH runs in session 0 and cannot reach `WinSta0`.

The spike creates the desktop `hc-spike-b` with `CreateDesktopW`. It starts Chrome, Character Map (Win32) and Notepad (packaged, WinUI) with `STARTUPINFO.lpDesktop = WinSta0\hc-spike-b`. It captures each window with `PrintWindow(PW_RENDERFULLCONTENT)`. It sends text with `PostMessage(WM_CHAR)`. A PowerShell helper on the hidden desktop dumps the UI Automation tree.

### Results

| Check | Result | Note |
| --- | --- | --- |
| 1. Chrome renders | Pass with flags | Without flags, only the browser frame renders and the page is white or stays "Loading...". With `--disable-features=CalculateNativeWinOcclusion --disable-backgrounding-occluded-windows --disable-renderer-backgrounding`, the page renders, GPU on. VM only: no real GPU. |
| 2. Win32 and WinUI apps render | Partial | Character Map renders fully. Notepad renders its frame and text, but not its XAML menu bar and toolbar. Calculator and Settings: not tested. |
| 3. Input arrives, human untouched | Pass | `WM_CHAR` reached the Character Map edit box, the Notepad editor and the Chrome page (with the occlusion flags). CDP `Input.dispatchKeyEvent` also reached the page. The cursor and the foreground window of the input desktop did not change in three runs. The first run measured on the wrong desktop and does not count. `SendInput` was not used: it only reaches the input desktop. |
| 4. UI Automation | Pass | A helper started on the hidden desktop reads the full UIA tree of Notepad and Chrome's top window. |
| 5. Two screens at once | Not tested | |
| 6. Close ends all processes | Partial | Chrome and Character Map end with `taskkill /T`. Packaged Notepad is single-instance: the launched pid exits, and a broker process owns the window. Closing must enumerate the desktop's windows and end their owner processes. |
| 7. Capture time | Fail at 200 ms | `PrintWindow` of one 1280x800 window takes 190 to 270 ms. Chrome through CDP `Page.captureScreenshot` is the fast path for browsers. |
| 8. Requirements | Pass | No admin rights, no registry change, any edition. The spike must run in the interactive session. |

### Consequences for the design

- A screen is a set of windows, not one framebuffer. A screenshot composes the windows from `PrintWindow` at their positions.
- Input goes to a window, not to a seat. `click` maps a point to the window under it and its child, then posts mouse messages with client coordinates. Apps that read raw input or check the foreground window may ignore posted messages. This needs a test per app class.
- hyprcage starts every Chrome with the three occlusion flags.
- Browsers use CDP for capture and input, as `browser_open` already does on Linux.
- WinUI and XAML content does not capture with `PrintWindow`. Hypothesis: Windows.Graphics.Capture of a window on a hidden desktop also fails, because DWM does not compose that desktop. To verify.

## Spike A: loopback RDP session

Setup: the same VM, Windows 11 Pro 26H2. The console user `Quickemu` is signed in (session 1). The spike enabled Remote Desktop, created a local user `hcagent` in "Remote Desktop Users", stored its credential with `cmdkey /generic:TERMSRV/127.0.0.2`, and started `mstsc` with an `.rdp` file for `127.0.0.2` (1280x800) from session 1 through a scheduled task with `/it`.

### Results

- The connection reaches the server: `qwinsta` shows `rdp-tcp#0` in state `Conn`.
- The logon is refused: "The number of connections to this computer is limited and all connections are in use right now."
- The same refusal comes with the policy values `MaxInstanceCount = 2` and `fSingleSessionPerUser = 0` (under `HKLM\SOFTWARE\Policies\Microsoft\Windows NT\Terminal Services`), after `gpupdate /force` and a restart of `TermService`.
- Before the logon, Windows shows three prompts: the self-signed certificate warning (removed by `authentication level:i:0`), a one-time consent for unsigned `.rdp` files per account, and an "Unknown remote connection" warning on every launch of an unsigned `.rdp` file. To verify: whether `rdpsign` with a trusted certificate removes the last one.
- Not tested: an RDP logon from another machine while the console user is signed in.
- All changes were undone after the test: user, credential, task, policy values, firewall rule and `fDenyTSConnections`.

### Ruling

Spike A fails check 8. A Windows 11 client edition allows one interactive session at a time. A second session needs a patched `termsrv.dll` (for example RDP Wrapper). That breaks the license terms and breaks on Windows updates, so hyprcage does not use it. Checks 1 to 7 were not run, because no second session started.

## Decision

The plan says: if spike A fails check 8 and spike B passes checks 1 to 4 with Chrome, choose B. Spike B passes checks 1, 3 and 4 with Chrome. Check 2 is partial: XAML content does not capture. The isolation model is B, the hidden desktop. Its open items: two screens at once (check 5), closing single-instance packaged apps (check 6), capture time for non-browser windows (check 7), and Calculator and Settings (check 2).
