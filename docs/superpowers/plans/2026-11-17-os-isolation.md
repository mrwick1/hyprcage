# OS isolation redo plan

Date: 2026-11-17
Status: In progress. PR 0 and item 1 ship together on branch `feat/osfeat`.
Replaces: branch `feat/os-tools` (5 commits, 2026-10-11, never pushed, 207 commits behind `main`).

## Outcome

An application in an offstage screen does not reach the human's desktop through a file dialog, a system prompt or the speakers.

Port only the behavior. Do not rebase the old branch. Write each change fresh against `main`, in small PRs, at existing hook points.

## Rules for every PR

- Change one hook point per feature. Do not refactor the code around it.
- Add no new MCP tool when an existing tool can do the work.
- Add no field to `registry.Screen`. Derive names from the screen name.
- Put new logic in a new file. Keep edits to shared files (`server.go`, `config.go`, `launch.go`) to a few lines.
- Add a test for each change. Run `go build ./...`, `go vet ./...` and `go test ./...`.

## Architecture: OS backends

offstage runs on Linux and Windows. macOS can follow. The OS-specific code of this plan goes behind interfaces in one new package, `internal/osfeat`. Each OS supplies its backends in its own file. Shared code calls the interfaces and never checks the OS.

```go
// internal/osfeat/osfeat.go
type Window struct{ Class, Title string }

type Prompts interface {
    IsPrompt(w Window) bool // a polkit, keyring, pinentry, UAC or credential window
}
type Dialogs interface {
    Env() map[string]string // keeps a file dialog in the screen
}
type Audio interface {
    Ensure(screen string) error
    Remove(screen string) error
    Prune(live []string) error           // remove sinks of screens that do not exist
    Env(screen string) map[string]string // routes the app's sound to the screen
    Observe(screen string) AudioState    // streams that play, and leaks
}

// Set holds the backends of this system. A nil field means unsupported.
type Set struct {
    Prompts Prompts
    Dialogs Dialogs
    Audio   Audio
}

func Detect(cfg config.Config) Set // implemented once per OS
func (s Set) LaunchEnv(screen string) map[string]string
```

Files:

| File | Build tag | Content |
| --- | --- | --- |
| `osfeat.go` | none | Interfaces, `Set`, `LaunchEnv`, `Status` for `doctor` |
| `detect_linux.go` | `linux` | `Detect` with the Linux backends |
| `detect_other.go` | `!linux` | `Detect` that returns an empty `Set`. Split it into `detect_windows.go` when Windows gets a backend. |
| `prompts_linux.go`, `dialogs_linux.go`, `audio_linux.go` | `linux` | The Linux backends |

Rules:

- Detection is dynamic. A backend is set only when its dependency exists at run time. For example, the Linux audio backend needs `pactl`. Without it, `Set.Audio` is nil and offstage works as before.
- Config can turn off a backend: `[screen] dialogs = "host"` and `[audio] enabled = false` make `Detect` leave the field nil.
- A caller handles nil in one place. A predicate on a nil backend returns `unsupported`, the same as the process predicate off Linux today.
- The server calls `Detect` once at start and keeps the `Set` on `Server` and on `screen.Ctx`.
- `doctor` prints one line for each backend: active, off by config, or missing a dependency.
- A new OS adds files with its build tag. Shared code does not change.
- Leave the existing `available(name)` gate in `platform.go` and the `_linux.go`/`_other.go` pairs as they are. Moving them to `osfeat` is a separate decision.

Check for each PR: `go build ./...`, `GOOS=windows go vet ./...` and `GOOS=darwin go build ./...` must pass.

## Priority

| # | Feature | Why first | Size |
| --- | --- | --- | --- |
| 1 | System prompts are read-only | Security. An agent can type into a polkit, keyring or pinentry prompt today. | Small |
| 2 | File dialogs stay in the screen | The dialog opens on the human's desktop. The agent cannot drive it. | Small, after a spike |
| 3 | Audio sink for each screen | Sound reaches the speakers. | Medium |
| 4 | Audio condition | The agent can check that sound plays. | Small |
| — | Audio in recordings | Deferred. It touches `internal/record`, which PR #52 changed. | — |

## Dropped from the old branch

| Old item | Reason |
| --- | --- |
| `system_prompts` tool and `wait system_prompt` | The guard refusal covers the risk. `desktop_windows` already lists prompt windows. |
| `notifyd` prompt watcher and the `[prompts]` Telegram command | Not core. Telegram is not the notification channel (see `tg-notify`). |
| `file_dialog` tool and `host_dialog` result | When the dialog opens in the screen, `snapshot` and `act` drive it. |
| `audio_level` tool and the `wait audio=` option | Replaced by a `condition_wait` predicate kind. |
| Plugin bump to 0.9.0, `.serena/` files | Stale or accidental. |

## 1. System prompts are read-only

Hook point: `Server.guard` in `internal/mcpserver/server_guard.go`. Every input tool calls it. Desktop actions pass the window address.

Changes:

1. Add `prompts_linux.go` in `osfeat` with the default prompt window classes: `hyprpolkitagent`, `polkit-gnome-authentication-agent-1`, `polkit-kde-authentication-agent-1`, `gcr-prompter`, `pinentry*`, `org.kde.ksecretd`. To verify: the class of each agent on archMachine with `hyprctl clients`.
2. In `Server.guard`, before the `Enabled` check, call `Set.Prompts.IsPrompt` when it is not nil, and refuse an action on the desktop when the window class is a prompt class. Use the error code `system_prompt`. Do not allow `ack` to override it. Do not let `guard.enabled = off` turn it off.
3. Add the config key `[guard] prompt_classes` to extend the list. Do not add a new section.

To verify: whether a desktop click at coordinates can omit `window`. If it can, find the window that contains the point with `desktop.Windows()` and apply the same check.

Test: a table test in `server_guard_test.go` with a fake prompt window. Click, type and key must return `system_prompt`. A normal window must pass.

## 2. File dialogs stay in the screen

Hook point: the environment map in `screen.Launch` (`internal/screen/launch.go`). All three callers go through it: `app_launch`, `offstage launch` and the browser launch.

Conflict: the old branch blocked the session bus (`DBUS_SESSION_BUS_ADDRESS` to a dead path). That also stops desktop notifications from the app. `notify_list` and `notify_wait` depend on them. The old fix breaks a shipped feature.

Spike first (half a day, no PR):

1. Test portal opt-outs that keep the session bus: `GTK_USE_PORTAL=0` (GTK 3), `GDK_DEBUG=no-portals` (GTK 4), no `xdgdesktopportal` Qt platform theme, and the Chromium or Electron portal switch.
2. Test with one GTK 3, one GTK 4, one Qt and one Electron app. For each, record where the file dialog opens and whether `notify_list` still sees the app's notification.
3. Write the result in this file.

Then:

1. Add `dialogs_linux.go` in `osfeat`. Its `Env` returns the variables that the spike proved.
2. In `screen.Launch`, merge `Set.LaunchEnv(screen)` before `extraEnv`, so a caller can still override it. That adds 3 lines. Items 2 and 3 share this one hook.
3. Add `[screen] dialogs = "cage" | "host"` with default `cage`.
4. Fallback only if no opt-out works: the blocked bus as a non-default `dialogs = "nobus"` mode. Document that it disables notifications.

Test: a unit test that `Launch` sets the variables and that `extraEnv` wins. A live test in the VM (`tests/vm/vm.sh`) that opens a dialog in a GTK app and finds it with `snapshot`.

## 3. Audio sink for each screen

Hook points: `screen.Create`, `screen.Destroy`, `screen.GC` and `screen.Launch`.

Changes:

1. Add `audio_linux.go` in `osfeat`. Name the sink `offstage_<screen>`. Functions: `Ensure(name)` loads a `module-null-sink` with `pactl` and `Remove(name)` unloads the module whose `sink_name` matches. No module ID is stored, so no registry change is needed.
2. `Create`: call `Set.Audio.Ensure` when the backend is not nil. On error, log once and continue. Audio must not block screen creation.
3. `Env` returns `PULSE_SINK=offstage_<screen>`. `LaunchEnv` from item 2 applies it. PipeWire honors it through `pipewire-pulse`. To verify on archMachine.
4. `Destroy` calls `Remove`. `GC` calls `Prune` with the live screen names.
5. `doctor`: the `osfeat` status line covers it.
6. Add `[audio] enabled = true`.

Test: `audio_linux_test.go` with a fake `pactl` on `PATH`. Check the commands for create, destroy and orphan cleanup.

## 4. Audio condition

Hook point: the predicate kinds in `internal/mcpserver/server_expect.go` (`element`, `request`, `process`).

Changes:

1. Add the kind `audio` with `playing: true|false`.
2. Call `Set.Audio.Observe`. Return `unsupported` when the backend is nil. The Linux backend reads `pactl list sink-inputs`: count the streams on `offstage_<screen>` that are not corked. Return the stream count and app names as evidence.
3. Also report streams of the screen's processes that play on another sink. That is a leak. Put it in the evidence. Do not fail the wait for it.

Test: a predicate test with a fake `pactl` output.

## Later

- Audio in `record_start`: rebase on the PR #52 ffmpeg changes. Plan it after item 4 ships.
- A prompt notice to the human through `tg-notify`, if testers ask for it.
- Windows backends: prompts (`consent.exe`, `CredentialUIBroker.exe`) when desktop tools reach Windows, and audio with a virtual endpoint if a tester needs it. To verify: what Windows offers without a driver install.

## Delivery

| PR | Content | Depends on |
| --- | --- | --- |
| 0+A | `internal/osfeat` with the `Prompts` interface only, `Detect` for each OS, wiring into `Server`, and item 1. Each later PR adds its own interface, so no interface ships without a user. | — |
| B | Spike result in this file, then item 2 | 0 |
| C | Item 3 | 0 |
| D | Item 4 | C |

Each PR starts from `origin/main` in `~/coding/worktrees/hyprcage/<branch>`. After all PRs merge, delete the four `feat/os-*` worktrees and branches.
