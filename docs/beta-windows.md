# offstage beta guide: Windows

This guide is for the Windows tester of the closed offstage beta. Thank you
for testing. Please read it once before you install.

On Windows, the bundled Bare browser runs headlessly. It opens no desktop window.
The agent can do browser work only. Linux has more tools. See "What works on
Windows".

All results so far come from a Windows 11 VM. Nobody has verified this build on
real Windows hardware. Anything about GPU or hardware behavior is not verified.

## Requirements

- Windows 11, x64.
- An agent that speaks MCP. Claude Code is the main target.
- A beta key from Arjun (see "Beta key").

You need no admin rights. The installer changes no system setting. It installs
Bare and its driver. Chrome is not required. Recording is unavailable.

The Claude Code plugin hooks run through Git Bash. Without Git Bash, Claude
Code runs them in PowerShell, where they fail. The session goes on, but screens
then close only when the MCP server stops. Install Git for Windows to avoid
this.

## Install (beta build)

The Bare browser migration is unreleased. The commands below still pin the
previous beta. Use the migration release version after it is published.

The beta is a pre-release. The installer skips pre-releases unless you pin the
version. Set `OFFSTAGE_VERSION`. Run this in PowerShell:

```
$env:OFFSTAGE_VERSION = 'v0.11.0-beta.11'
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/mrwick1/offstage/beta/install.ps1))) -Agents none
```

The installer puts `offstage.exe` in `%LOCALAPPDATA%\offstage\bin`. It checks
the download against the published SHA256SUMS. It writes the configuration with
the defaults. `-Agents none` keeps it from registering offstage with Claude
Code: the plugin below does that, and two registrations would give the agent
two offstage servers. To use offstage with Codex as well, name it, for example
`-Agents codex`.

The installer does not put `offstage.exe` on your PATH. The commands in this
guide need it there. Run this once in PowerShell, then open a new terminal:

```
[Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path', 'User') + ";$env:LOCALAPPDATA\offstage\bin", 'User')
```

Check the install:

```
offstage doctor
```

The Bare check must show `ok`. A missing browser binary fails the check.

For Claude Code, install the plugin from the `beta` branch as well. Run this
in a terminal:

```
claude plugin marketplace add mrwick1/offstage#beta
```

Then, in Claude Code:

```
/plugin install offstage@offstage-mrwick1
```

The plugin pins the beta build. Its launcher (`bin\offstage-mcp.cmd`) runs
`install.ps1 -BinaryOnly` when the installed binary is not the pinned version.

Turn on auto-update once, so that new betas arrive by themselves. In Claude
Code, run `/plugin`, open the **Marketplaces** tab, select `offstage-mrwick1`
and select **Enable auto-update**. Claude Code then checks the marketplace
in each session, a few minutes after your first message. When it finds a new
beta, it shows `Plugin updated: offstage`. The next start of Claude Code loads
the new plugin, and the plugin installs the matching offstage binary.

To update at once instead, run `claude plugin update offstage@offstage-mrwick1`
in a terminal, then restart Claude Code.

offstage also checks the `beta` branch once a day, when a session starts.
When a newer beta exists, it starts the update and shows a message. The new
version loads after `/reload-plugins` or a restart of Claude Code. When a
beta key is stored, the same check also tells the relay that you use
offstage. It sends the plugin version and the platform, nothing else. To turn
the check off, set `OFFSTAGE_NO_UPDATE_CHECK=1`.

What changed in each beta, and what to retest:
[docs/beta-changes.md](beta-changes.md).

## Beta key

Arjun sends your key privately. It starts with `osk_`. Never post it anywhere.

Store it once:

```
offstage report login
```

Paste the key and press Enter. offstage saves it in
`%APPDATA%\offstage\beta-key`. The file sits in your user profile.

You can use the `OFFSTAGE_BETA_KEY` environment variable instead. It takes
priority over the file.

Without a key, reports wait on your machine. They are not lost.

## What works on Windows

The agent has these tools:

- Screens: `screen_create`, `screen_destroy`, `screen_list`, `browser_open`.
- Input: `click`, `double_click`, `type`, `key`, `scroll`, `drag`, `move`,
  `wait`.
- Reading: `screenshot`, `snapshot`, `act`, `find`, `read_page`, `watch`.
- DevTools: `devtools_eval`, `devtools_console`, `devtools_network`,
  `devtools_trace`, `devtools_heap`, `devtools_audit`, `devtools_emulate`.
- Recording: `record_start`, `record_stop`. They need `ffmpeg`.
- Reports: `report_issue`.

The server does not list these tools on Windows. They are planned after v1:

- Native apps (`app_launch`, `app_close`, `windows`).
- `desktop_*`.
- `notify_*`.
- `clipboard_*`.
- `mirror`.

Coordinates are page pixels.

## What the agent reports and when

The agent calls `report_issue` when offstage itself fails:

- A tool returns an error that the arguments did not cause.
- A tool returns success, but the action did not happen.
- A tool touched your desktop, cursor or focus.
- offstage crashed or did not start.

The agent does not report bugs in the app under test, wrong arguments, or an
action that the guard blocked on purpose. It files one report per problem.

The installer and each Claude Code session start add a short reporting rule to
`~/.claude/CLAUDE.md` and to `~/.codex/AGENTS.md`, when those directories
exist. The rule tells the agent to file at once and to continue its task. It
sits between `<!-- offstage:beta-reporting` markers. offstage never changes
text outside the markers. A symlinked file is never edited: `offstage doctor`
warns, and `offstage agent-rule print` shows the block to add by hand.
`offstage agent-rule remove` takes the block out, and the uninstaller does too.

After a crash, the next tool result tells the agent that crash files wait. The
agent then files a report with the crash files.

You can also file a report yourself:

```
offstage report --title "Screenshot is black" --expected "the page shows" --actual "black image" --screen <screen> --area install
```

`--area` is one of `crash`, `isolation-leak`, `install`, `desktop` or `bare` (a problem with the bare browser server,
`bare-mcp`). Only
`--title` is required.

Reports go to a relay at `https://beta.mrwick.dev`. The relay files them in a
private tracker. You get no tracker account. Each report gets an ID such as
`OFS-12`.

## What a report holds and what is removed

A report is a zip bundle. It holds:

- `trace.jsonl`: the tool calls on the screen, in order. The calls carry
  arguments, results and error codes.
- `env.txt`: the offstage version, OS, agent client name, Bare browser binary
  and your `config.toml`.
- `session-trace.jsonl`: the tool calls of the whole agent session, up to 500.
- `blocks.jsonl`: the failed tool calls of the session, up to 200.
- `logs/`: the last 200 lines of up to 6 logs of the screen.
- `snapshot.txt` and `screenshot.png`: the element tree and a screenshot of the
  agent screen. Never of your own desktop.
- `crashes/`: crash files, only for a crash report.

Before the bundle leaves your machine, offstage removes:

- Your home directory. It becomes `~`.
- Your user name and host name. They become `<user>` and `<host>`.
- Tokens, keys, passwords and cookies.
- URL query strings and fragments.
- Email addresses.
- Typed text. The trace keeps its length only.

The relay removes the same data again.

The screenshot and the element tree show the page that the agent opened. Check
a report with a dry run if that worries you.

The bundle stays under 4 MB. If it is larger, offstage drops the screenshot,
then the logs, then the session trace, then the block log.

## Settings

The `[report]` table is in `%APPDATA%\offstage\config.toml`. To write the file
with the defaults, run `offstage config`.

```
[report]
url = "https://beta.mrwick.dev"
confirm = false
screenshots = true
agent_rule = true
```

| Key | Default | Effect |
|---|---|---|
| `url` | `https://beta.mrwick.dev` | The relay. Do not change it. |
| `confirm` | `false` | `true`: the agent shows you the report and sends it only after you agree. |
| `screenshots` | `true` | `false`: reports carry no screenshot and no element tree. |
| `agent_rule` | `true` | `false`: the next session start removes the reporting rule from the instruction files. |

Build a report and see it without sending it:

```
offstage report --title "test" --dry-run
```

The command prints the report text and the file list. It also prints the folder
of the bundle. A dry run is never sent.

Send the reports that waited (no key, or the relay was down):

```
offstage report flush
```

Show the state of a report you filed:

```
offstage report status OFS-12
```

## Turning reports off

Two settings reduce what a report carries:

- `screenshots = false` removes the screenshot and the element tree.
- `confirm = true` makes the agent show each report to you first.

Without a beta key, nothing is sent. Reports only queue in
`%LOCALAPPDATA%\offstage\reports`.

To stop sending reports completely, delete the key:

```
Remove-Item $env:APPDATA\offstage\beta-key
```

Reports then stay in the queue on your machine. To send again, run
`offstage report login`.

## Bare: the bundled browser

Bare is required. The installer verifies and installs `bare-mcp.exe` and
`bare-drive.exe` in `%LOCALAPPDATA%\offstage\bin`. A missing download or invalid
checksum fails installation. `-NoBare` and `OFFSTAGE_NO_BARE=1` are rejected.

Use offstage's `browser_open`, `browser_snapshot`, `browser_act` and other
`browser_*` tools. No `screen_create` is needed. Close each session with
`browser_close`. The server closes its browser process when it stops.
Existing standalone Bare registrations stay unchanged.
`offstage doctor` fails when either browser binary is missing.
Report a Bare problem with `report_issue`, area `bare`.

## Uninstall

Run the installer again with `-Uninstall`:

```
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/mrwick1/offstage/beta/install.ps1))) -Uninstall
```

It removes the offstage registration from Claude Code and Codex, the binary and
the state of offstage. It also removes bare, if the installer put it there. Chrome and ffmpeg stay. For the plugin, also run
`/plugin uninstall offstage@offstage-mrwick1` in Claude Code.

## Known limits

- Browser sessions are headless. Native apps, desktop tools, mirrors and
  recording are unavailable on Windows.
- Bare uses its own engine. Check compatibility with the target app.
- Chrome DevTools, Lighthouse, performance traces and heap snapshots are
  unavailable. Use browser console, network and diagnosis tools.
- The Bare migration has not been tested on Windows hardware or in the VM.
