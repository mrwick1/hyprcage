# offstage beta guide: Windows

This guide is for the Windows tester of the closed offstage beta. Thank you
for testing. Please read it once before you install.

On Windows, a screen is one headless Chrome. It has no window on your desktop.
The agent can do browser work only. Linux has more tools. See "What works on
Windows".

All results so far come from a Windows 11 VM. Nobody has verified this build on
real Windows hardware. Anything about GPU or hardware behavior is not verified.

## Requirements

- Windows 11, x64.
- Google Chrome.
- An agent that speaks MCP. Claude Code is the main target.
- `ffmpeg`, only for recording: `winget install ffmpeg`.
- A beta key from Arjun (see "Beta key").

You need no admin rights. The installer changes no system setting. It installs
neither Chrome nor ffmpeg. It warns when one is missing.

The Claude Code plugin hooks run through Git Bash. Without Git Bash, Claude
Code runs them in PowerShell, where they fail. The session goes on, but screens
then close only when the MCP server stops. Install Git for Windows to avoid
this.

## Install (beta build)

The beta is a pre-release. The installer skips pre-releases unless you pin the
version. Set `OFFSTAGE_VERSION`. Run this in PowerShell:

<!-- set the real tag before release -->
```
$env:OFFSTAGE_VERSION = 'v0.11.0-beta.1'
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

Each line must show `ok`. A `warn` on `ffmpeg` means only that recording does
not work.

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
When a new beta comes out, run `/plugin marketplace update offstage-mrwick1`.
On its next start, the plugin installs the new binary.

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

After a crash, the next tool result tells the agent that crash files wait. The
agent then files a report with the crash files.

You can also file a report yourself:

```
offstage report --title "Screenshot is black" --expected "the page shows" --actual "black image" --screen <screen> --area install
```

`--area` is one of `crash`, `isolation-leak`, `install` or `desktop`. Only
`--title` is required.

Reports go to a relay at `https://beta.mrwick.dev`. The relay files them in a
private tracker. You get no tracker account. Each report gets an ID such as
`OFS-12`.

## What a report holds and what is removed

A report is a zip bundle. It holds:

- `trace.jsonl`: the tool calls on the screen, in order. The calls carry
  arguments, results and error codes.
- `env.txt`: the offstage version, OS and your `config.toml`.
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
then the logs.

## Settings

The `[report]` table is in `%APPDATA%\offstage\config.toml`. To write the file
with the defaults, run `offstage config`.

```
[report]
url = "https://beta.mrwick.dev"
confirm = false
screenshots = true
```

| Key | Default | Effect |
|---|---|---|
| `url` | `https://beta.mrwick.dev` | The relay. Do not change it. |
| `confirm` | `false` | `true`: the agent shows you the report and sends it only after you agree. |
| `screenshots` | `true` | `false`: reports carry no screenshot and no element tree. |

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

## Uninstall

Run the installer again with `-Uninstall`:

```
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/mrwick1/offstage/beta/install.ps1))) -Uninstall
```

It removes the offstage registration from Claude Code and Codex, the binary and
the state of offstage. Chrome and ffmpeg stay. For the plugin, also run
`/plugin uninstall offstage@offstage-mrwick1` in Claude Code.

## Known limits

- A screen is one headless Chrome. There is no native app, no desktop tool and
  no mirror window.
- Some sites detect a headless browser. They can block it or change the page.
- offstage has no Job Object yet. It closes a screen with `taskkill /T /F` from
  Chrome's main process. If that process dies first, a child process of Chrome
  can stay. Run `offstage gc --all` to destroy every screen and clean the
  leftovers. If a Chrome process still stays, close it in Task Manager and
  tell Arjun.
- `offstage replay` runs the trace of a report again on new screens. For a
  report that it cannot replay, Arjun may ask you to repeat the steps.
- Only a Windows 11 VM verified this build. Real hardware, and the GPU in
  particular, is not verified.
- The key file is not given an access list of its own on Windows. It is in your
  user profile.
- Linux is a separate guide: `docs/beta-linux.md`.

## Contact

Message Arjun directly. Do not post your beta key or a report bundle in public.
