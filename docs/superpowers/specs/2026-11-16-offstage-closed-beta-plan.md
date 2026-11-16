# offstage closed beta plan: Linux and Windows

Date: 2026-11-16
Status: draft, for review
Depends on: the Windows port plan and the spike B rulings (`2026-11-16-hyprcage-windows-port-plan.md`, `2026-11-16-hyprcage-windows-spike-rulings.md`, on the local branch `docs/windows-port-plan`, not merged yet)

## Goal

A small group of known testers runs offstage on Linux (Hyprland) and on Windows through Claude Code. When an agent meets an offstage problem, the agent sends a report to a relay. The relay files it as a work item in a private Plane workspace. Each report holds a trace that `offstage replay` runs again in the test VM. A report that replays to the same failure needs no call to the tester.

## Decisions (2026-11-16)

1. Platforms: Linux and Windows run in the same beta. Linux ships first with the full tool set. Windows follows when B5 is done.
2. Isolation on Windows: run spike A (loopback RDP) first. If spike A fails, ship the Windows beta on spike B (hidden desktop) and list its limits. The Windows beta ships in both cases.
3. Testers: Joel on Linux, Aswin on Windows. More known people later, under ten in total.
4. Report target: the self-hosted Plane instance `plane.mrwick.dev` (Community edition 1.4.2). Testers get no Plane account. GitHub issues stay off.
5. Filing path: a small relay on mrwick-server. Each tester has a beta key. Only the relay holds a Plane token.
6. Reproduction: a report carries a replayable trace. A screenshot is supporting evidence only.
7. Rename: the project is now called offstage (PR #28). The rename lands before the first beta build.

## Testers

| Tester | Platform | Setup |
| --- | --- | --- |
| Joel | Linux | Same as Arjun's ThinkPad E16 (`arjun-e16`): openSUSE Tumbleweed, Hyprland 0.56.2, AMD HawkPoint1 GPU (checked 2026-11-16). The README records openSUSE as verified with Hyprland 0.56.2. |
| Aswin | Windows | Joins when the Windows build is ready (B5). |

`arjun-e16` is the reference machine for Joel's track. A problem Joel reports is replayed there first.

## Phases

| Phase | Track | Output | Gate |
| --- | --- | --- | --- |
| R0 | both | Rename merged, repository renamed, release `v0.10.0-beta.1` (Linux assets only) | Fresh plugin install on `arjun-e16` from the renamed marketplace |
| B4 | both | Relay, report pipeline and replay (this plan) | A forced failure on `arjun-e16` files a Plane item through the relay, and `replay` reproduces it |
| L1 | Linux | Linux beta to Joel | Install on openSUSE from `install.sh`; Linux VM suite and `tests/e2e-desktop.py` green on `arjun-e16` |
| B0 | Windows | Spike A result in the rulings file | Isolation model chosen |
| B1 | Windows | Port plan phases 2 and 3 (seam, platform services) | `GOOS=windows go build ./...`, Linux suite green |
| B2 | Windows | Port plan phase 4 (Windows screen backend) | Notepad e2e in the VM; human cursor and focus unchanged |
| B3 | Windows | Port plan phases 5 and 6 (UIA, browser, DevTools) | Chrome e2e through `browser_open`, `snapshot`, `act` |
| B5 | Windows | Packaging (port plan phase 7) and Windows beta release | Clean VM install from `install.ps1` to a working screen |
| B6 | both | Beta run | Exit criteria below |

Order: R0, then B4 and L1 together, then the Windows phases. The Windows work does not block the Linux beta. B1 must keep the Linux suite green, because Joel runs Linux builds from the same branch.

### L1: Linux beta

The Linux tester gets every tool, including the Hyprland-only ones:

- Agent screens: `screen_*`, `app_launch`, pointer and keyboard tools, `snapshot`, `act`, `find`, `watch`, `clipboard_*`.
- Browser: `browser_open`, `devtools_*`.
- Recording: `record_start`, `record_stop`, and the `mirror` window.
- The human's desktop: `desktop_*` and the perception tools with `screen: "desktop"`.
- Notifications: `notify_*` through `offstage-notifyd.service`.
- The waybar module in `contrib/waybar`.

Checks before the build goes to Joel:

- `install.sh` on a clean openSUSE Tumbleweed user, with zypper. The installer also installs `at-spi2-core` on openSUSE.
- `tests/e2e-desktop.py` and the Linux VM suite on `arjun-e16`.
- Known openSUSE limit from the README: `tesseract-ocr` 5.5.3-2.1 crashes on exit. offstage keeps its output. The tester guide says so.
- Reports that touch the human's desktop (`desktop_*`, `notify_*`) carry window titles and notification text. The privacy rules below apply to them.

### B0: spike A

Run the checks 1 to 8 of the port plan in `tests/windows/spike-loopback/`. Add these checks, because they decide if spike A is possible at all:

- Hypothesis: a client edition of Windows allows one active session at a time. A loopback RDP logon as a second user may disconnect the console user. To verify on Windows 11 Pro in the VM.
- Hypothesis: concurrent sessions on a client edition need a patched `termsrv.dll` (for example RDP Wrapper). That breaks the Windows licence terms and fails check 8. To verify. If it is true, spike A fails.
- The session renders at 1280x800 when no RDP client window is visible.
- WinUI menus and Windows.Graphics.Capture work in the session. These failed or were untested in spike B.

Time box: two days. If spike A fails, choose spike B, record the limits in the rulings file, and ship the beta on spike B. Do not start option C (VM per screen) for the beta.

Beta tool set for each outcome:

| Tool group | Spike A | Spike B |
| --- | --- | --- |
| `screen_*`, `app_launch`, `windows` | yes | yes |
| `screenshot` | yes | yes; per-window `PrintWindow`, slow; CDP for Chrome |
| `click`, `type`, `key`, `scroll`, `drag`, `move` | `SendInput` | window messages; some apps ignore them |
| `snapshot`, `act`, `find`, `read_page` | UIA, CDP | UIA, CDP |
| `browser_open`, `devtools_*` | yes | yes, with the three occlusion flags |
| `clipboard_*` | yes | to verify |
| `desktop_*`, `notify_*`, `record_*`, `mirror` | no | no |

Tools that Windows does not have return `unsupported_platform`. The MCP server hides them from the tool list on Windows.

## Rename

The name "hyprcage" says Hyprland. Windows users will think it is not for them. The project gets a new name before B1, so that no Windows code, installer or tester guide uses the old name.

Scope, measured on `main` at `51f7c52`: about 1065 uses of "hyprcage" in 165 files, plus more than 20 `HYPRCAGE_*` environment variables.

- Go module path, binary name, `cmd/` directory, MCP server name, plugin and marketplace names, skill name, hook names, `install.sh`, release asset names.
- Config and state paths. A one-time move copies `~/.config/hyprcage`, `~/.local/state/hyprcage` and `~/.local/share/hyprcage` to the new paths when the new paths do not exist.
- Environment variables: read the new name first, then the old name, for one release.
- GitHub repository rename. GitHub redirects the old URL. Existing Linux users update the plugin once.
- The plugin launcher finds a binary under the old name and replaces it.
- Check: `make test`, the Linux VM suite, and a fresh plugin install from the renamed marketplace.

## B4: report pipeline

### Parts

1. A CLI command `offstage report`. It builds a bundle and sends it to the relay.
2. An MCP tool `report_issue`. It calls the same code. Arguments: `title`, `expected`, `actual`, `screen` (optional), `step` (optional, default: the last failed call).
3. A CLI command `offstage replay <bundle>`. It runs the trace of a bundle on a new screen and compares the result with the recorded one.
4. Instructions in the MCP server text and in the skill that tell the agent when to call `report_issue`.
5. Crash capture: a panic handler writes a crash bundle. The next start of the MCP server tells the agent in the first tool result that a crash bundle waits.
6. The relay and the Plane setup (sections below).

### When the agent files

File a report when one of these is true:

- A tool returns an error that the arguments did not cause (for example `internal`, `capture_failed`, a timeout on a stable app).
- A tool returns success, but `verify` or a follow-up `snapshot` shows that the action did not happen.
- A tool touched the human's desktop, cursor or focus.
- The MCP server crashed or did not start.
- The install or the plugin launcher failed.

Do not file when the app under test has the bug, when the arguments were wrong, or when the guard blocked an action on purpose. File at most one report per distinct problem per session.

### Reproduction

A screenshot shows the end state. It does not show how to get there. The bundle carries what a developer needs to run the same steps again:

- Trace: every tool call on the screen since `screen_create`, in order, with arguments, result, error code and duration. The source is `actions.jsonl`. The audit log already drops typed text.
- Stable targets: for each `act`, `click` or `drag` that used a ref or a point, the trace also stores the role, the name and the ancestor path of the element under it. Replay finds the element by these, not by a ref or a pixel, so a different window position does not break it.
- Placeholders: typed text becomes `<text:N chars>`. Replay types a string of the same length. A tester can mark a field as safe, and then the real text goes in.
- State around the failure: the element tree (UIA or CDP) before and after the failing step, and the window list with titles, classes, process names and bounds.
- App identity: executable name, version, and for Chrome the version and the URL without its query and fragment.
- Environment: build version, Windows build and edition, isolation model, GPU name, VM or real hardware, display scale, screen size.
- Timing: the delay between steps, so replay can keep it.
- Screenshots: one before and one after the failing step, agent screen only. Evidence, not the repro.

Replay result:

- Same error code or same failed verification at the same step: the report gets the label `reproduced`.
- A different result: `needs-repro`. The bundle and the replay log go to the developer.
- Replay runs in the Windows test VM. A T3 scheduled task can run it for each new report. Decide after week 1.

### Privacy

Only Arjun sees Plane. Testers see only the state of their own reports through the relay. The bundle still passes these rules on the tester's machine, and the relay runs them again:

- Replace the home directory with `~`. Replace the user name and the host name with `<user>` and `<host>`.
- Drop environment variables, tokens, cookies, and URL query strings and fragments.
- Drop the text of `type`, `clipboard_set` and `devtools_eval` arguments. Keep the length.
- Screenshots and element trees come from agent screens only. Never from the human's desktop.
- Linux desktop tools: window titles and notification bodies are cut to 40 characters. Notification app names stay.
- Element tree values (text box contents, page text) are cut to 80 characters each.
- Config `report.screenshots = false` turns off screenshots.
- Config `report.confirm = true` is the default in week 1. `report_issue` then returns the bundle path and the report text first. The agent shows them to the human and files only after a yes.

Check: a unit test feeds a bundle with a home path, a host name, a token in a URL, a cookie and typed text, and asserts that none of them is in the output.

### Relay

The relay keeps Plane private. A tester's machine never holds a Plane token and never talks to Plane.

- Binary: `cmd/offstage-relay` and `internal/relay` in this repository. Go standard library only.
- Status (2026-11-16): deployed and tested end to end with Joel's key. Infra files: `compose/offstage-relay` in the infra repository.
- Runs on mrwick-server as one container in `/srv/offstage-relay`, on the `web` network. It reaches Plane at its public address, because Plane's presigned upload URLs use that address.
- Address: `https://beta.mrwick.dev`, through the server's Caddy. The wildcard DNS record already covers it. Not behind Authelia: the beta key is the login.
- Resting footprint: 1.6 MB of memory at idle (measured 2026-11-16). The container limit is 64 MB.

Endpoints:

| Method and path | Does |
| --- | --- |
| `POST /v1/reports` | Takes `meta.json` and `bundle.zip` as multipart. Checks the key, the size and the rate. Redacts again. Files or merges the Plane item. Returns a report ID such as `OFS-42`. |
| `GET /v1/reports/{id}` | Returns the state and the build that fixed it. Only for reports filed with the same key. |
| `GET /healthz` | Returns 200. Gatus watches it. |

Beta keys:

- One key per tester, `osk_` plus 32 random bytes in base32. The relay stores only the SHA-256 of each key with the tester name.
- Keys live in the vault as `offstage/beta-key-<tester>` in folder `agents`. Arjun sends each key to its tester.
- Revoke a key: delete its line from `/srv/offstage-relay/secrets/keys` and restart the container.
- Add a tester: generate a key, store it in the vault, and add `name $(offstage-relay hash <<< key)` to the keys file.
- The tester stores the key with `offstage report login`. It goes to the file `beta-key` next to the config file, readable by the user only (mode 0600). It never goes into `config.toml`. `OFFSTAGE_BETA_KEY` overrides the file. The OS credential store was dropped: a 0600 file is simpler and the key only files reports.

Limits: 5 MB per bundle (the Plane attachment limit on this instance), 10 reports per key per day, 60 requests per key per hour.

Filing, inside the relay:

1. Check the key. Reject unknown keys with 401 and no details.
2. Check `meta.json` against the schema and the bundle size.
3. Run the redaction rules again on `meta.json` and on the text files in the bundle.
4. Look up the fingerprint in the relay's own store (a JSON file in the data volume). On a match, add a comment to the existing work item and attach the new bundle.
5. Otherwise create the work item with the labels `agent-report`, the platform and the area. The description holds the summary, the environment, the repro steps and the fingerprint.
6. Upload the bundle as an attachment.
7. Return the report ID.

On the tester's machine: no key, or the relay does not answer, makes `offstage report` write the bundle to the state directory under `reports/<id>/` and return the path. The next `offstage report` call sends waiting bundles first.

Verified against Plane 1.4.2 on 2026-11-16: work items, comments, labels and the three-step attachment upload (create, presigned POST to MinIO, confirm).

### Plane setup

- A workspace `offstage-beta`, separate from Arjun's `personal` workspace.
- A bot user `offstage-bot`, a member of `offstage-beta` only. Its API token is the only Plane token the relay holds. It is in the vault as `offstage/plane-bot` and on the server as a Docker secret.
- One project `Beta`, identifier `OFS`, with states New, Reproduced, In progress, Fixed in build, Closed. Plane reserves the name Triage.
- Labels: `agent-report`, `manual`, `linux`, `windows`, `crash`, `isolation-leak`, `install`, `desktop`, `reproduced`, `needs-repro`.
- Testers do not get Plane accounts. No invitations are sent.
- A tester who wants to file by hand runs `offstage report --manual`. It asks for the title and the steps and sends them through the relay.
- The server backup (`server-backup` timer) already copies Plane's database and MinIO uploads, so attachments are backed up. The relay store is not backed up. Losing it only loses duplicate merging and status for old reports.

### Triage

- Daily: open the New items in the `offstage-beta` workspace.
- Each report gets a reply within two working days.
- A fix PR names the Plane item ID. The beta release notes list the fixed items. GitHub links stay one-way: Plane points to the PR.

## Releases

- Version: `v0.10.0-beta.N`, published as a GitHub pre-release. Linux and Windows assets come from the same tag once B5 is done.
- `release.yml` builds the Windows `.exe` next to the Linux builds. To verify: the Windows build needs no cgo.
- Linux: `install.sh` installs the pre-release, the packages through pacman or zypper, and `offstage-notifyd.service`, then runs `offstage report login`.
- Windows: `install.ps1` downloads the pre-release, checks its SHA-256, places the binary in `%LOCALAPPDATA%\offstage\bin`, writes the config with `report.confirm = true`, and runs `offstage report login`.
- Plugin: testers install from a `beta` branch of the marketplace. To verify: Claude Code accepts a branch or ref for a plugin marketplace.
- Windows launcher: `.mcp.json` must start a Windows launcher. To verify: how Claude Code on Windows runs a plugin MCP command, and if `${CLAUDE_PLUGIN_ROOT}` works there.
- Tester guides: `docs/beta-linux.md` and `docs/beta-windows.md`. Each covers requirements, install, the beta key, how reports work, what is collected and how to turn reports off.

## B6: beta run

- Week 1: Joel on Linux. `report.confirm = true`.
- When B5 is done: the Windows tester joins.
- A new beta build each week, or after each crash fix.
- Exit criteria for a public release, per platform:
  - No open `crash` or `isolation-leak` item.
  - Linux: one tester ran the desktop and notification tools for a week without an `isolation-leak`.
  - Windows: one tester on real hardware (not a VM) ran Chrome and one Win32 app for a week.
  - Each `agent-report` item is `reproduced` and fixed, or closed with a reason.

## Risks

- The relay is a public endpoint. Mitigation: hashed keys, rate limits, a size limit, server-side redaction, and no Plane data in its answers.
- mrwick-server is down: reports wait on the tester's machine and go out on the next call.
- Redaction misses something. Only Arjun sees Plane, but the bundle still leaves the tester's machine. Mitigation: the redaction test, `report.confirm = true` in week 1, and agent screens only for screenshots.
- Linux desktop tools read the human's real windows and notifications. A report about them can carry private titles. Mitigation: titles and notification bodies are cut to 40 characters and the tester confirms in week 1.
- Spike A needs a licence-breaking patch. Then Windows ships on spike B, with slow screenshots and no WinUI menus.
- Replay depends on the app version and on the window layout. Stable targets reduce this. Some reports will still need the tester.
- Agents file noise (app bugs, wrong arguments). Mitigation: the "when the agent files" rules, the fingerprint and the rate limit.
- The test VM hides GPU problems on Windows. Exit criteria require real hardware.

## Open questions for Arjun

1. `report.confirm = true` for week 1 only, or for the whole beta.
