# offstage beta changes

This file lists what changed in each beta build, newest first. Testers and
their agents read it after an update. "Retest" names what to try again.

Each build is a GitHub pre-release with the same name. The plugin from the
`beta` branch pins the newest build.

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
