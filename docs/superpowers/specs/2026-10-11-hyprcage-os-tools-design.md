# hyprcage OS tools: file dialogs, screen audio, system prompts

Date: 2026-10-11
Status: draft, for review
Base branch: `main`

## Goal

Make three OS-level behaviors of a caged application predictable and visible to the agent:

1. A file dialog that a caged application opens stays on the agent screen, and the agent can answer it.
2. Each screen has its own audio sink. A recording can include the audio, and the agent can read whether a screen plays sound.
3. A system prompt (polkit, keyring) that a caged application triggers reaches the human as a notification. The agent never answers it.

This is sub-project C. Sub-projects A (perception) and B (desktop control) are done.

## Non-goals

- Answer a polkit or keyring prompt. The agent can read that a prompt exists. Only the human answers it.
- Capture audio from the human's desktop or from the human's default sink.
- Capture microphone input or give a screen a virtual microphone.
- Support Flatpak or Snap sandbox portals beyond what a plain application does.
- Change a screen's behavior for applications that do not open a file dialog, play sound or trigger a prompt.

## Measured facts

Date of the measurements: 2026-10-11, on archMachine (Hyprland, PipeWire 1.6.8, cage screens). A screen named `hc-ostools` held the test applications. The screen was destroyed afterwards.

### Environment of a caged application

- `internal/screen/launch.go` builds the application environment from the environment of the hyprcage process. It overrides `WAYLAND_DISPLAY`, `DISPLAY` and the toolkit hints. It drops `HYPRLAND_INSTANCE_SIGNATURE`, `GDK_SCALE`, `GDK_DPI_SCALE` and `QT_SCALE_FACTOR`.
- It does not touch `DBUS_SESSION_BUS_ADDRESS`, `XDG_RUNTIME_DIR`, `PULSE_SERVER` or `PULSE_SINK`. A caged application therefore uses the human's session bus and the human's PipeWire.
- Check: `tr '\0' '\n' < /proc/<pid>/environ | grep -E 'HYPRCAGE_SCREEN|DBUS'` for a `paplay` launched with `app_launch`. Result: `DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus` and `HYPRCAGE_SCREEN=hc-ostools`.
- The human's session runs `xdg-desktop-portal`, `xdg-desktop-portal-gtk` and `xdg-desktop-portal-hyprland`. `/usr/share/xdg-desktop-portal/hyprland-portals.conf` sets `default=hyprland;gtk`.

### Area 1: file dialogs

| Case | Launch | Result |
| --- | --- | --- |
| Chrome, `<input type=file>`, default environment | `browser_open` on a `data:` URL, then `act click` on "Choose File" | The dialog opened on the human's desktop. `hyprctl clients -j` listed a new window of class `xdg-desktop-portal-gtk`, title "Open File", on workspace 6. The cage window list did not show it. |
| Same dialog, read from the host | `snapshot screen:"desktop" window:<address>` | AT-SPI returned 13 nodes: "Select", "Cancel", "Up Path", the place and file options and "Open files read-only". The agent can read a host dialog. |
| Same dialog, cleanup | `app_close` of the Chrome window | The host dialog closed within 3 seconds. The portal request ends with its caller. |
| GTK3 `Gtk.FileChooserDialog` (python3-gi script) | `app_launch` | The dialog is a second window of the application, inside the cage. It is not on the desktop. `windows` lists it ("DlgOpen"). `snapshot` returns the AT-SPI tree: "Open" (disabled), "Cancel", the place options and the file options. The place rows have empty names. |
| GTK3 `Gtk.FileChooserNative`, default environment | `app_launch` | No window appeared in the cage or on the desktop within 4 seconds. Cause unknown. To verify: whether the request went to the portal and waited, or failed. |
| Chrome with `DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent` | `app_launch` with `env` and `debug:true` | The dialog opened inside the cage ("Open File", app id `google-chrome`). AT-SPI returned 0 nodes, because the application also lost the accessibility bus. `snapshot source:"ocr"` returned the visible rows (folders and file names). `key Escape` closed the dialog. |

Facts that follow:

- The portal path is the reason a Chrome dialog lands on the desktop. The cage does not isolate the session bus.
- Taking the session bus away puts the dialog in the cage. It also breaks AT-SPI for that application and every other bus service (notifications, secret service).
- `org.a11y.Bus.GetAddress` on the session bus returns `unix:path=/run/user/1000/at-spi/bus_0`. To verify: that an application with `AT_SPI_BUS_ADDRESS` set to that address and an unreachable session bus still exposes its AT-SPI tree.
- The system has no GTK text editor (`gedit`, `mousepad`, `gnome-text-editor` are not installed). `thunar` is installed and has no Open dialog. A short GTK script served as the GTK test application.

### Area 2: screen audio

| Check | Command | Result |
| --- | --- | --- |
| Sinks today | `pactl list short sinks` | Two sinks: the analog output `alsa_output.pci-0000_06_00.6.analog-stereo` (default) and `avatar_mic_sink`. No sink belongs to a screen. |
| Where a caged app plays | `app_launch ["paplay", ...]`, then `pactl list sink-inputs` | The stream went to sink 58, the human's default analog output. |
| Client properties | `pw-dump` | The client and node carry `application.process.id`, `application.name`, `application.process.binary` and `media.name`. They carry no screen name. |
| Link to the screen | `/proc/<pid>/environ` | `HYPRCAGE_SCREEN` identifies the screen. The PID property in PipeWire joins the stream to the screen. |
| Sink override | `app_launch` with `env {"PULSE_SINK":"hc_ostools_test"}` | `pactl list short sink-inputs` showed the stream on the new sink (206). `env` reaches the application. |
| Null sink | `pactl load-module module-null-sink sink_name=hc_ostools_test` | The module loaded (module 536870918). The default sink stayed unchanged. `pactl unload-module` removed it. The sink list returned to two entries. |
| Monitor capture | `parec -d hc_ostools_test.monitor` into `ffmpeg -af volumedetect` | A 440 Hz test tone (max level -18.1 dB in the file) measured max -18.1 dB and mean -21.3 dB on the monitor. The monitor carries the stream. |
| Recording today | read `internal/record/record.go` and the `[record]` keys in `internal/config/config.go` | `record_start` pipes raw RGBA frames to ffmpeg on stdin. ffmpeg has no audio input. A recording has no audio track. There is no config key for audio. |

Side effect to know: PipeWire's stream-restore module stores a volume per application name. A first test with `paplay --volume=1500` stored 2 percent for `paplay`. The next stream played at 2 percent (-91 dB on the monitor). A later stream with `--volume=65536` reset the stored value to 100 percent. The earlier stored value for `paplay` is unknown.

Also relevant to the design: the recorder cuts idle video (`record.idle`, default 1s). An audio track must follow the same cuts, or the track drifts.

### Area 3: system prompts

| Check | Command | Result |
| --- | --- | --- |
| Polkit daemon | `ps -eo comm \| grep -iE 'polkit\|hyprpolkit'` | Only `polkitd` runs. No authentication agent runs in the human's session. |
| Installed agent packages | `pacman -Qq \| grep -iE 'polkit\|hyprpolkit'` | `polkit` only. `hyprpolkitagent` is not installed. |
| Hyprland autostart | `grep -riE 'polkit\|agent\|keyring' ~/.config/hypr/autostart.*` | No match. The session does not start an agent. |
| Keyring | `busctl --user list` | `gnome-keyring-daemon` owns `org.freedesktop.secrets`. `org.gnome.keyring.SystemPrompter` and `PrivatePrompter` are activatable. `gcr` is installed. |
| Keyring prompt window | not triggered | To verify: that an unlock request opens a `gcr-prompter` window on the human's desktop, and its window class. |

Facts that follow:

- A caged application shares the session bus, so a keyring prompt it triggers goes to the prompter of the human's session, not to the cage.
- No polkit agent exists today. To verify, with the human's consent: what an action that needs authorization does when no agent runs (hypothesis: the call fails or falls back to a terminal agent, and no window appears). The measurement was not run, because it could show a prompt on the desktop.

## Design

### Area 1: file dialogs stay in the cage

#### Decision

By default a caged application must not reach the portal file chooser. The cage keeps the session bus and blocks only the file chooser route.

Three options exist:

| Option | Effect | Cost |
| --- | --- | --- |
| A. Unreachable session bus plus `AT_SPI_BUS_ADDRESS` | Dialog in the cage. AT-SPI possible (to verify). | No notifications, no secret service, no portals of any kind for the application. |
| B. Filtering D-Bus proxy per screen | The application keeps the bus. The proxy denies `org.freedesktop.portal.FileChooser`. | Needs `xdg-dbus-proxy`. It is not installed. |
| C. Toolkit switches (`GTK_USE_PORTAL=0`, Chrome switches) | No bus change. | Not measured. To verify: that Chrome honors a switch. |

Proposal: measure C first, then A. Choose B only if both fail. The measurement decides in the first implementation task. The tools below do not depend on the option.

#### Config

```toml
[dialogs]
mode = "cage"   # cage: file dialogs open inside the screen
                # host: the application uses the portal, the dialog opens on the human's desktop
```

`mode = "host"` is the current behavior. A launch `env` entry overrides the mode for one application.

#### Tools

- `file_dialog {screen, action, path?, timeout_ms?}`
  - `action: "wait"`: wait until a file dialog window appears on the screen, and return its window and mode (open, save, folder).
  - `action: "choose"`: type `path` into the location field (`ctrl+l`, then text), press `Return`, and return the result window list. A relative path refuses with `invalid_path`.
  - `action: "cancel"`: press `Escape`.
  - Detection: a new window whose title matches `Open|Save|Select|Choose|Upload` (configurable) and whose parent application has another window. To verify: the match rate on GTK3, GTK4 and Chrome.
- `windows` and `snapshot` need no change. They already list and read an in-cage dialog.
- A dialog on the host: after `act click` or `key`, `act` checks `hyprctl clients -j` for a new window of class `xdg-desktop-portal-gtk` or `xdg-desktop-portal-hyprland` and adds `host_dialog: <address>` to its result. The agent then calls `app_close` or the human answers it. `act` does not touch the host window.

The tool count goes up by one.

### Area 2: per-screen audio sink

#### Lifecycle

- `screen_create` loads `module-null-sink` with `sink_name=hc_<screen>` and `sink_properties=device.description=hyprcage-<screen>`. It stores the module ID in the registry record of the screen.
- `app_launch` adds `PULSE_SINK=hc_<screen>` to the environment, unless `env` sets it.
- `screen_destroy` and the safety timer call `pactl unload-module <id>`. `hyprcage doctor` removes a sink named `hc_*` whose screen no longer exists.
- The default sink stays unchanged. Nothing plays on the human's speakers from a screen.
- Applications that ignore `PULSE_SINK` (to verify: Chrome, Electron and Firefox) play on the default sink. The tool `audio_level` reports them as `leaked` (see below), using the `application.process.id` of every sink input that has `HYPRCAGE_SCREEN` set and a sink other than the screen sink.

#### Config

```toml
[audio]
enabled = true      # create a null sink per screen
record = true       # record_start adds the screen audio when the screen has a sink
monitor = false     # true: the human can hear the screen (loopback from the screen sink to the default sink)
```

`monitor = true` loads `module-loopback`. It stays off by default, because the human works near the speakers.

#### Recording

`record_start` adds a second ffmpeg input when `audio.record` is true and the target is a screen: `-f pulse -i hc_<screen>.monitor`. The output gets `-c:a aac`. The recorder starts the audio input at the first frame time.

The idle cut moves the video clock forward, and the audio stays in real time. Two solutions exist:

1. Cut the same ranges from the audio with an `aselect` filter fed by cut intervals. This needs an interval list from `record.Loop`.
2. Skip the idle cut for a recording with audio and keep real time.

Proposal: solution 2 in the first version. The loop reports no cut when audio is on. Open question 3 asks the human.

A recording of the `desktop` target has no audio. `record_start` returns `audio: "none"` in the result for it.

#### Tools

- `audio_level {screen, window_ms?}`: return `{sink, playing, peak_db, rms_db, streams:[{app, pid, corked, sink}], leaked:[...]}`.
  - `playing` is true when at least one stream on the sink is not corked.
  - `peak_db` and `rms_db` come from a read of the monitor for `window_ms` (default 500, maximum 5000), through `parec` or the PipeWire native API, and `volumedetect` equivalent code in Go. No new dependency. To verify: that reading with `parec` costs no more than a few percent CPU.
  - The streams come from `pactl list sink-inputs` joined with `/proc/<pid>/environ` for `HYPRCAGE_SCREEN`.
- `audio_wait {screen, state: "playing"|"silent", timeout_ms}`: wait until the state holds. It polls `audio_level` every 250 ms.

The tool count goes up by two.

### Area 3: system prompts, read only

#### Behavior

hyprcage never answers a system prompt. It tells the human and lets the agent read that a prompt exists.

- `system_prompts {}`: list the prompt windows on the human's desktop and the notifications that look like prompts. Each entry has `kind` (`polkit`, `keyring`, `pinentry`, `ssh-askpass`, `portal`), `address`, `class`, `title`, `pid`, `age_ms` and `screen` (the screen name when the PID has `HYPRCAGE_SCREEN` set, else empty).
- `system_prompt_wait {kind?, timeout_ms}`: wait for a new entry.
- No tool types a password, clicks a prompt button or sends a key to a prompt window. `desktop_type`, `desktop_key`, `desktop_focus`, `act` and `click` refuse a window that `system_prompts` lists. The error code is `system_prompt` and the hint says that the human answers it.

#### Detection

Source of entries, in order:

1. `hyprctl clients -j` filtered by window class. Defaults: `polkit-gnome-authentication-agent-1`, `hyprpolkitagent`, `org.kde.polkit-kde-authentication-agent-1`, `gcr-prompter`, `pinentry-*`, `ssh-askpass`, `xdg-desktop-portal-gtk`. The list is configurable.
2. Notifications from `internal/notifyd` whose app or summary matches `[prompts] notify_match`.
3. To verify: a `BecomeMonitor` match on `org.gnome.keyring.Prompter` calls on the session bus. This gives the keyring prompt before the window maps.

A prompt triggered by an application of a screen comes from the PID of the window. The PID joins to the screen through `/proc/<pid>/environ`. A prompt from a system service has no screen.

#### Telling the human

- When a new entry appears, hyprcage sends a desktop notification through `org.freedesktop.Notifications`: "Prompt waiting: <kind>, from <screen or system>". The notification has no action button.
- The agent sees the same entry through `system_prompts`, and the result of `act` adds `system_prompt: <kind>` when a new entry appeared during the call.
- The watcher runs inside `hyprcage notifyd`, which already runs as a user unit. No new daemon.

#### Config

```toml
[prompts]
notify = true                  # tell the human when a prompt appears
classes = ["gcr-prompter", "hyprpolkitagent", "polkit-gnome-authentication-agent-1", "pinentry-*", "ssh-askpass"]
notify_match = "(?i)authenticat|password|unlock|polkit"
```

#### Today's gap

No polkit agent runs on this machine. A polkit prompt would have no window. `system_prompts` then reports nothing, and the action fails or waits (to verify). The doctor check below reports this. hyprcage does not start or install an agent. The human decides whether to install one (open question 4).

## Security

- A screen reaches the human's session bus today. A caged application can already call any session service. Option B of area 1 narrows this for the file chooser only. It is not a sandbox.
- The tool surface adds no write path to a system prompt. A test checks that every desktop input tool refuses a window that `system_prompts` lists.
- `file_dialog choose` runs inside the cage. The path it types is visible to the application only. The tool does not read files.
- `audio_level` reads the monitor of the screen sink, and no other source. It never reads the human's default sink or a microphone.
- A recording with audio holds what the screen played. Today's recordings in `~/Videos/agent` have mode 644 (measured with `stat -c %a`). A recording with audio gets the same mode. Open question 7 asks whether to tighten it.
- `system_prompts` shows window titles. A title can name a user or an action. The tool output goes to the agent, as with every desktop tool. The privacy ruling of sub-project B applies: the agent reads everything.

## Errors

| Code | Meaning |
| --- | --- |
| `no_dialog` | `file_dialog` action `choose` or `cancel` with no dialog on the screen. |
| `invalid_path` | `file_dialog choose` with a relative path or an empty path. |
| `audio_off` | The screen has no sink (`audio.enabled` false, or a screen from before the upgrade). The hint says to create a new screen. |
| `system_prompt` | An input tool targeted a window that `system_prompts` lists. |

## Dependencies

- No new Go module. `godbus/dbus/v5` covers the bus watch.
- `pactl` (the package `libpulse`) for the sink module and the stream list. Already present with `pipewire-pulse`. `hyprcage doctor` checks it.
- `ffmpeg` with the `pulse` input. To verify: `ffmpeg -formats | grep pulse` on arjun-e16 (openSUSE).
- `xdg-dbus-proxy` only if area 1 needs option B.

## Testing

- **Task 1: measurement.** Run option C and option A of area 1, the `AT_SPI_BUS_ADDRESS` check, Chrome and Electron with `PULSE_SINK`, and the `FileChooserNative` case. Write the results into this spec.
- **Unit tests (Go)**, with fakes for `pactl` output and `hyprctl clients`:
  - the sink name and module ID in the registry record, and the unload on destroy;
  - the join of a sink input to a screen through the PID;
  - the audio level math on a generated PCM buffer;
  - the prompt class match, the screen join and the `system_prompt` refusal.
- **Smoke script `tests/smoke-os-tools.sh`** on a throwaway screen:
  - create a screen, check that `hc_<screen>` exists, destroy it, check that the sink is gone;
  - play a tone with `PULSE_SINK` and check `audio_level playing:true`, then check `playing:false` after the end;
  - record 3 seconds with a tone and check with `ffprobe` that the file has a video and an audio stream.
- **E2E over MCP** on an agent screen only:
  - Chrome with a file input: `file_dialog wait`, `choose` with a test file path, and a check that the page shows the file name;
  - the same with a GTK3 dialog;
  - a check that `hyprctl clients -j` shows no new window on the human's desktop during both runs.
- The E2E never triggers a polkit prompt on the human's desktop. The prompt tests use a fake window source and a nested Hyprland, as sub-project B does.

## Decisions

Arjun decided these points on 2026-10-11.

1. Area 1: option A is acceptable. Caged applications can lose the session bus services, including notifications and the secret service, when the cage must use a private bus.
2. Area 2: keep `monitor = true`. The human can listen to a screen.
3. Area 2: a recording with audio keeps real time and has no idle cut.
4. Area 3: install `hyprpolkitagent`, so that a polkit prompt shows on the human's desktop.
5. Area 3: every polkit and keyring prompt also goes to Telegram, through `tg-notify`.
6. Tool count: fold `audio_wait` and `system_prompt_wait` into the existing `wait` tool. The count goes from 39 to 42 (`file_dialog`, `audio_level`, `system_prompts`).
7. Security: new recordings get mode 0600.

## Build flow

Worktree `../hyprcage-os-tools` on branch `feat/os-tools` off `main`, in three PRs, one per area. The order is audio, file dialogs, then prompts. Each PR needs a browser E2E on an agent screen and a plugin version bump.
