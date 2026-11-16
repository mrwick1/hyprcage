# Task lifecycle

Task tools are available on Linux. `task_launch` requires a working systemd user manager. It does not use a process-group fallback.

## Use

1. Call `task_create` with `{}`. Keep the returned `id`.
2. Call `task_launch` with `task_id`, an argv array in `command`, and optional `cwd` and `env`. Use it for a local server or test process. It does not add a shell.
3. Call `screen_create` with the same `task_id`. Use the existing screen tools to launch graphical apps and control the browser.
4. Call `task_inspect` with `task_id` to read resource state, exit results, and log paths.
5. Call `task_close` with `task_id`. It stops owned services and closes associated screen instances. Retry a failed close. A repeated successful close is safe.

Example launch input:

```json
{"task_id":"<returned id>","command":["npm","run","dev"],"cwd":"/absolute/project/path"}
```

A process gets a unique systemd service. Its children belong to that service's control group. Cleanup stops the service, not a process selected by its name or port. A screen is identified by its name and creation time. Reusing a screen name does not give an old task ownership of the new screen.

The task directory is under `$XDG_STATE_HOME/offstage/tasks`, or `~/.local/state/offstage/tasks`. It contains the manifest and redacted process logs. The manifest is written before a process launch. Existing screen startup and recovery handle a failure before screen association completes.

## Limits

- A task can register 64 resources. Each process log is limited to 1 MiB. Lines longer than 16 KiB are discarded. An `.incomplete` file marks truncated output or a write failure.
- Text logs use the existing redaction rules before persistence. Pattern matching cannot remove every possible secret. Do not print secrets from test processes.
- Closed task artifacts expire after 7 days. A later `task_create` removes expired closed task directories. Active task artifacts are not expired automatically.
- Task tools do not copy, isolate, or delete project files. Task directories are private artifacts, not a filesystem or network security boundary.
- A server restart can inspect and stop recorded systemd services. Output collection cannot reconnect after its launcher dies; inspection reports the log source as unavailable. An exit result lost during a crash can remain unknown.
- Ownership follows the agent session. A known session ID permits adoption after its parent PID changes. Without a session ID, ownership requires the original parent process identity.
- Close tasks explicitly. Abrupt agent termination does not automatically close task-owned services. Task tools do not add a background cleanup service.
- Browser replacement, condition waits, and combined failure timelines are separate work.

## Validation

Package tests cover ownership checks, independent services, child-process cleanup through systemd, retained exit results, launcher restart recovery, redaction, output limits, retention, screen identity, and cleanup retry. MCP tests cover registration and task create/inspect/close. A live graphical screen journey is not covered by these tests.
