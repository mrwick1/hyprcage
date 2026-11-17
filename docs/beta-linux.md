# offstage beta guide: Linux

This guide is for the Linux testers of the closed offstage beta. Thank you for
testing. Please read it once before you install.

## Requirements

- A Hyprland desktop. Verified: openSUSE Tumbleweed with Hyprland 0.56.2.
- Hyprland 0.50 or later.
- An agent that speaks MCP. Claude Code is the main target.
- `sudo`. The installer asks for your password to install packages.
- A beta key from Arjun (see "Beta key").

The installer installs `cage`, `ffmpeg`, `wl-clipboard`, `tesseract` and
`at-spi2-core` through `zypper` (or `pacman` on Arch).

## Install (beta build)

The Bare browser migration is unreleased. The commands below still pin the
previous beta. Use the migration release version after it is published.

The beta is a pre-release. The installer skips pre-releases unless you pin the
version. Set `OFFSTAGE_VERSION`:

```
curl -fsSL https://raw.githubusercontent.com/mrwick1/offstage/beta/install.sh | OFFSTAGE_VERSION=v0.11.0-beta.13 bash -s -- --agents none
```

The installer installs the packages, puts the binary in `~/.local/bin`, and
starts `offstage-notifyd.service` for the notification tools. `--agents none`
keeps it from registering offstage with Claude Code: the plugin below does
that, and two registrations would give the agent two offstage servers. To use
offstage with another agent as well (Codex, Cursor, Gemini CLI, Windsurf,
OpenCode), name it, for example `--agents codex`.

For Claude Code, install the plugin from the `beta` branch as well. Run this
in a terminal:

```
claude plugin marketplace add mrwick1/offstage#beta
```

Then, in Claude Code:

```
/plugin install offstage@offstage-mrwick1
```

The plugin pins the beta build.

Turn on auto-update once, so that new betas arrive by themselves. In Claude
Code, run `/plugin`, open the **Marketplaces** tab, select `offstage-mrwick1`
and select **Enable auto-update**. Claude Code then checks the marketplace
in each session, a few minutes after your first message. When it finds a new
beta, it shows `Plugin updated: offstage`. The next start of Claude Code loads
the new plugin, and the plugin installs the matching offstage binary.

To update at once instead, run `claude plugin update offstage@offstage-mrwick1`
in a terminal, then restart Claude Code.

offstage also checks the `beta` branch when a session starts, at most once
an hour. When a newer beta exists, it starts the update and shows a message.
The new version loads after `/reload-plugins` or a restart of Claude Code.
Once a day, when a beta key is stored, offstage also tells the relay that you
use offstage. It sends the plugin version and the platform, nothing else. To
turn the checks off, set `OFFSTAGE_NO_UPDATE_CHECK=1`.

What changed in each beta, and what to retest:
[docs/beta-changes.md](beta-changes.md).

Check the install:

```
offstage doctor
```

If you had hyprcage before, remove its plugin first:
`/plugin uninstall hyprcage@hyprcage-mrwick1`. The installer removes the old
binary and moves the old config, state and browser profiles to the new name.

## Beta key

Arjun sends your key privately. It starts with `osk_`. Never post it anywhere.

Store it once:

```
offstage report login
```

Paste the key and press Enter. offstage saves it in
`~/.config/offstage/beta-key`. Only your user can read that file.

You can use the `OFFSTAGE_BETA_KEY` environment variable instead. It takes
priority over the file.

The installer prints a hint for this step. Without a key, reports wait on your
machine. They are not lost.

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
offstage report --title "Mirror window stays black" --expected "the screen shows" --actual "black window" --screen <screen> --area desktop
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
- `env.txt`: the offstage version, OS, kernel, Hyprland, cage, cage renderer,
  GPU, agent client name, Bare browser binary and your `config.toml`.
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

Tools that touch your desktop (`desktop_*`, `notify_*`) can put window titles
and notification text in a trace. Check a report with a dry run if that worries
you.

The bundle stays under 4 MB. If it is larger, offstage drops the screenshot,
then the logs, then the session trace, then the block log.

## Settings

The `[report]` table is in `~/.config/offstage/config.toml`. To write the file
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
`~/.local/state/offstage/reports`.

To stop sending reports completely, delete the key:

```
rm ~/.config/offstage/beta-key
```

Reports then stay in the queue on your machine. To send again, run
`offstage report login`.

## Bare: the bundled browser

Bare is required. The installer verifies and installs `bare-mcp` and
`bare-drive` in `~/.local/bin`. It also installs `libxkbcommon`.
A missing download or invalid checksum fails installation.

Use the `browser_*` tools in the offstage MCP server. Headless sessions need
no screen. For a visible session, create an offstage screen and pass its name
as `screen` to each browser call. Close the session with `browser_close`.
Destroying the screen also closes its browser process.

Existing standalone Bare registrations stay unchanged. New installations
register only offstage. `OFFSTAGE_NO_BARE=1` is no longer supported.
`offstage doctor` fails when either Bare binary is missing.
Report a Bare problem with `report_issue`, area `bare`.

## Known limits

- On openSUSE, the `tesseract-ocr` package 5.5.3-2.1 crashes when it exits.
  offstage keeps the output that tesseract wrote before the crash. OCR
  snapshots still work.
- `offstage replay` runs the trace of a report again on new screens. It does not
  replay the desktop screen, `desktop_*`, `notify_*` or `setup`. For a report about
  those, Arjun may ask you to repeat the steps.
- Windows is not part of this beta.

## Contact

Message Arjun directly. Do not post your beta key or a report bundle in public.
