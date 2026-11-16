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

The beta is a pre-release. The installer skips pre-releases unless you pin the
version. Set `OFFSTAGE_VERSION`:

```
curl -fsSL https://raw.githubusercontent.com/mrwick1/offstage/beta/install.sh | OFFSTAGE_VERSION=v0.11.0-beta.1 bash -s -- --agents none
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

The plugin pins the beta build. When a new beta comes out, run
`/plugin marketplace update offstage-mrwick1`. On its next start, the plugin
installs the new binary.

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

After a crash, the next tool result tells the agent that crash files wait. The
agent then files a report with the crash files.

You can also file a report yourself:

```
offstage report --title "Mirror window stays black" --expected "the screen shows" --actual "black window" --screen <screen> --area desktop
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
- `env.txt`: the offstage version, OS, kernel, Hyprland, cage and GPU, and your
  `config.toml`.
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
then the logs.

## Settings

The `[report]` table is in `~/.config/offstage/config.toml`. To write the file
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
`~/.local/state/offstage/reports`.

To stop sending reports completely, delete the key:

```
rm ~/.config/offstage/beta-key
```

Reports then stay in the queue on your machine. To send again, run
`offstage report login`.

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
