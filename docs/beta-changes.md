# offstage beta changes

This file lists what changed in each beta build, newest first. Testers and
their agents read it after an update. "Retest" names what to try again.

Each build is a GitHub pre-release with the same name. The plugin from the
`beta` branch pins the newest build.

## v0.11.0-beta.13 (2026-11-17)

- New: the offstage MCP server updates itself while it runs. About once an
  hour it checks the `beta` branch, downloads a newer build, checks it
  against `SHA256SUMS`, and replaces the binary and Bare. Then it restarts
  when it is idle: no tool call running, and no screen, recording, task or
  browser session open. Claude Code starts the new build on the next tool
  call. With Codex, the agent is told once to restart the MCP server.
  `[update] auto = false` or `OFFSTAGE_NO_AUTO_UPDATE=1` turns it off.
- Fix (Linux, shipped in beta.11, not listed there): a beta binary
  updates to the final release. The launcher compared versions with `sort -V`, which ranks `v0.11.0` below
  `v0.11.0-beta.8`.
- Bare v0.1.3 (out since beta.11) arrives with the daily Bare update: serif font families draw
  with Noto Serif; SVG text has `dominant-baseline`,
  `getComputedTextLength`, `getSubStringLength` and rotated or skewed text;
  an `<svg>` with a border or padding draws in its content box.
- Retest: nothing by hand. Leave a session open; later builds now arrive
  in it within about an hour.

## v0.11.0-beta.12 (2026-11-17)

- Change: offstage checks the `beta` branch for a new build at most once an
  hour, when a session starts. It was once a day. The usage ping and the
  Bare update stay daily. A session that stays open does not check again.
- Retest: nothing by hand. This build arrives with your next daily check;
  later builds arrive within an hour of a session start.

## v0.11.0-beta.11 (2026-11-17)

- Change: `report_issue` returns at once. The report is queued and sent in
  the background, with retries after 30 s, 2 min and 10 min. A report that
  an ended session left in the queue goes with the next session.
- New: offstage files some reports by itself, with the title prefix
  `[auto] `: an internal error, a capture or transport failure, the same
  tool failing 3 times in a row on one screen, or a desktop call after a
  failure on the agent's screen. At most 20 for each session. Nothing is
  filed with `[report] confirm = true`.
- New: each failed tool call, `browser_*` included, adds one redacted line
  to `blocks.jsonl` in the state directory. The log stays on the machine.
  Reports carry the lines of the session.
- New: reports carry the tool calls of the whole session in
  `session-trace.jsonl`, the agent client, and on Linux the cage renderer.
- New: the installers and the session start add a short reporting rule to
  `~/.claude/CLAUDE.md` and `~/.codex/AGENTS.md`, between offstage markers.
  Text outside the markers never changes. A symlinked file is skipped.
  `offstage agent-rule remove` removes it; `[report] agent_rule = false`
  keeps it out.
- Retest: nothing by hand. Use offstage as usual; the reports arrive by
  themselves. Check that your instruction file has the offstage block, or
  run `offstage doctor`.

## v0.11.0-beta.10 (2026-11-17)

- Change: offstage no longer pins a Bare release. The installers take the
  newest release of `mrwick1/bare`, and the daily update check brings Bare
  up to date in the background. `OFFSTAGE_BARE_VERSION=vX.Y.Z` still pins
  one.
- Fix (Bare v0.1.2, OFS-7): a d3 tree no longer draws stacked in the
  top-left corner. Bare now paints the SVG `transform` attribute and SVG
  `<text>`, has `transform.baseVal` (d3 transitions of `transform` run), and
  canvas `measureText` uses the size of `ctx.font`.
- Limits: SVG text is never turned or skewed. Glyphs are Noto Sans for every
  font family. `dominant-baseline` and `getComputedTextLength` are not built.
- New (Linux): desktop input is refused with `system_prompt` while a system
  prompt (polkit, pinentry, gcr-prompter, KDE wallet, ssh-askpass) has the
  focus, is the target or lies under the click. `[guard] prompt_classes`
  adds app ids.
- New (Linux): GTK and Qt file dialogs of an app in a screen stay in that
  screen. Electron dialogs still use the portal. `[screen] dialogs = "host"`
  turns it off.
- New (Linux): each screen gets its own audio output (a null sink, through
  `pactl`), so apps in a screen no longer play on the human's speakers.
  `condition_wait` takes an `audio` predicate (`playing: true` or `false`).
  `[audio] enabled = false` turns it off.
- Retest: pages with SVG charts or trees in `browser_screenshot`. On Linux,
  apps that play sound or open a file dialog.

## v0.11.0-beta.9 (2026-11-17)

- Fix (Windows, OFS-6): the plugin's launcher now updates a beta binary.
  Before, `install.ps1 -BinaryOnly` took every `-beta.N` version for a build
  from source and kept the first binary installed. The fix arrives with the
  plugin update, so no manual step is needed.
- Fix (Windows, OFS-6): an update no longer fails with "Cannot create a file
  when that file already exists" while other sessions run offstage. Each old
  binary gets a unique `.old-<ticks>` name. The next update removes the old
  copies that no session runs.
- Fix: `screen_create` adds the `hc-` prefix, and later calls with the name
  as given now find the screen.
- Fix: `app_launch` with `debug=true` gives a Chromium browser its own
  profile. Before, the page could open in the human's browser.
- Fix: a cage that fails to start is reported as `cage_failed`.
- Fix: a click on a node's own label no longer counts as covered.
- Fix: the audit keeps the error code of a failed Bare call.
- Fix: recording uses far less memory (about 54 MB at 1280x800) and
  `record_stop` no longer times out after a slow ffmpeg. Files are up to 2.5
  times larger.
- Retest: on Windows, check that `offstage version` prints v0.11.0-beta.9
  after the plugin updates. Long recordings.

## v0.11.0-beta.8 (2026-11-17)

- New: when a beta key is stored, the daily update check also pings the
  relay with the plugin version and the platform. Arjun then sees who uses
  offstage. `OFFSTAGE_NO_UPDATE_CHECK=1` turns the ping off too.

## v0.11.0-beta.7 (2026-11-16)

- New: Linux task tools group owned server processes, screens, and local
  artifacts. Use `task_create`, `task_launch`, `task_inspect`, and `task_close`.
  Server launch requires a working systemd user manager. Close tasks explicitly.
- New: `task_wait` waits for process exits. `condition_wait` observes native-app element
  presence or absence and DevTools requests. Failure conditions take precedence.
  An embedded action arms request observation before it sends input.
  `condition_cursor` supports a separate action followed by a wait.
- New: `task_timeline` retains bounded local evidence from actions, requests,
  console messages, owned process output, and failure page excerpts. It reports
  source gaps. Evidence remains readable after task closure.
- Limits: timeline collection occurs at tool boundaries and queries. It does
  not collect screenshots. Pattern redaction cannot remove every possible
  secret. Keep secrets out of test output. Bare reports are retained as partial text
  excerpts only after explicit console/network reads. Use Bare step assertions for browser postconditions; standalone
  request-start cursors are not available from Bare.
- Fix: failed Bare step assertions are tool errors. Timeline reads do not
  advance Bare console or network cursors.
- Retest: create a task, launch a local test server, and create its screen with
  the returned `task_id`. Trigger a controlled failed request. Read
  `browser_console` and `browser_network`, then `task_timeline`. Close the task, and confirm that only its resources stop.

- Bare is required. Missing or invalid browser downloads fail installation.
- The offstage MCP server exposes Bare through `browser_*` tools. New installs
  need no separate Bare MCP registration.
- Headless sessions need no screen. Visible Linux sessions run inside an owned
  offstage screen. Destroying that screen closes its browser process.
- Chrome browser tools and their DevTools tools are no longer exposed.
- Windows supports headless Bare sessions and reports. Recording is unavailable.
- `offstage browser` now serves Bare MCP on stdin/stdout. Its former URL,
  screen and profile arguments are removed. Existing Chrome profiles stay on disk.
- `OFFSTAGE_NO_BARE` and Windows `-NoBare` are rejected.

## v0.11.0-beta.6 (2026-11-16)

- New: the installers also install bare, a browser for scripted end-to-end
  journeys (`bare-mcp` and `bare-drive`, release v0.1.1 of `mrwick1/bare`).
  They check the files against the published SHA256SUMS, put them beside
  offstage, and register an MCP server named `bare` with each agent. A bare
  that is already registered stays as it is. The plugin starts bare through
  its own launcher. On Linux, the installer also installs `libxkbcommon`.
  Skip bare with `OFFSTAGE_NO_BARE=1` (Windows: `-NoBare`).
- New: `offstage doctor` warns when `bare-mcp` or `bare-drive` is missing.
- New: the agent reports a bare problem with `report_issue` and the area
  `bare`.
- Retest: run the installer again, then run `offstage doctor`. The line
  `bare` must say `ok`. Ask the agent to open a page with a bare tool.

## v0.11.0-beta.5 (2026-11-16)

- New: `report_issue` and `offstage report --type` take a type: `bug`
  (default), `doubt`, `suggestion` or `feedback`. The agent files a doubt
  when it is unsure how to use offstage, a suggestion when a tool or option
  would have helped, and feedback once at the end of a task that used
  offstage heavily. Only a bug needs `expected` and `actual`.
- New: each report holds more data for a repro: the Go and Chrome versions,
  the effective config, recent offstage logs (`log.txt`) and the duration of
  each tool call (`timeline.txt`). Private data is removed as before.
- New: when a session starts, offstage checks once a day for a newer beta.
  It starts the update and shows a message. Set `OFFSTAGE_NO_UPDATE_CHECK=1`
  to turn the check off.
- Retest: ask the agent to file a doubt, for example "file an offstage doubt
  about anything that was unclear". Check that the reply gives a report ID.

## v0.11.0-beta.4 (2026-11-16)

- Fix (Linux): the plugin launcher built offstage from source when Go was
  installed, and then never updated it. It now downloads the release that the
  plugin pins. It also replaces a binary that reports the version `source`.
- Docs: both guides explain how to turn on auto-update for the
  `offstage-mrwick1` marketplace.
- Retest (Linux): `offstage version` prints `v0.11.0-beta.4` after a restart
  of Claude Code.

## v0.11.0-beta.3 (2026-11-16)

- Fix (Windows): `install.ps1` no longer warns that ffmpeg is missing right
  after `winget install ffmpeg`. It reads the saved PATH first.
- Fix (Windows): `offstage doctor` no longer shows the Linux mirror settings.
- Retest (Windows): `offstage doctor` shows `chrome ok` and exits 0.

## v0.11.0-beta.2 (2026-11-16)

- Fix (Windows): the agent did not get the beta rule to file reports with
  `report_issue`. It now gets the same rule as on Linux.
- The offstage skill now also loads for a headless Chrome on Windows.

## v0.11.0-beta.1 (2026-11-16)

- New: Windows v1. A screen on Windows is one headless Chrome driven over
  DevTools. It has no window on the desktop. The browser tools, `snapshot`,
  `act`, `find`, `read_page`, `watch`, `devtools_*` and recording work.
  Native apps, `desktop_*`, `notify_*`, `clipboard_*` and `mirror` are not on
  Windows yet.
- New: `install.ps1` (user level, no admin), the plugin launcher
  `bin/offstage-mcp.cmd`, and `docs/beta-windows.md`.
- Fix: `offstage doctor` checks Chrome and ffmpeg on Windows.
- Fix: a dead headless screen no longer leaves its Chrome profile behind.
- Linux: no change in behaviour.

## v0.10.0-beta.2 (2026-11-16)

- Fix: the plugin named version 0.9.0, which has no release, so a new plugin
  install found no binary. The plugin now pins the beta build.
- New: `docs/beta-linux.md`, the Linux tester guide.

## v0.10.0-beta.1 (2026-11-16)

- First closed beta build. Linux only.
- hyprcage is now offstage. `install.sh` removes the old name and moves its
  settings, logs and browser profiles.
- Agents send beta reports with `report_issue`. Private data is removed first.
- `offstage report`: `login`, `flush`, `status` and `--dry-run`.
- `offstage replay` runs the trace of a report again on new screens.
- A panic in a tool returns the error code `internal` and saves a crash file
  for the next report.
