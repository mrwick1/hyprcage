# Self-update of the MCP server

`offstage mcp` updates itself while it runs. It needs no service and no timer outside the server.

## What it does

1. About one minute after start, and then about every hour (plus up to 20 % jitter), the server reads `version` in `https://raw.githubusercontent.com/mrwick1/offstage/beta/.claude-plugin/plugin.json`.
2. When that version ranks above the running one (semver order, the rule of `older()` in `bin/offstage-mcp`), it downloads `offstage-<os>-<arch>` and `SHA256SUMS` from the GitHub release.
3. It checks the SHA256 and puts the file in the directory of the binary. It never goes back to an older version.
4. It also checks the newest Bare release. When `bare-mcp` or `bare-drive` next to the binary differ from `SHA256SUMS`, it replaces them. A Bare that is not next to the binary stays alone.
5. A network or checksum error is logged on stderr and changes nothing.

A build from a checkout (`vX.Y.Z-N-gHASH`) and `dev` never update themselves.

## Linux

The new file is written next to the binary as a temp file, verified, then renamed over `offstage`. The running process keeps its old inode.

## Windows

A running `.exe` cannot be overwritten but can be renamed. The server renames the old file to `offstage.exe.old-<ticks>` and moves the new one in. If the move fails, it renames the old file back. The next start removes `*.old-*` files (the same scheme as `install.ps1`). Bare uses the same steps. Windows is amd64 only. In the Windows VM (2026-11-17), a running server replaced its own `offstage.exe`, exited with status 0, and the next start removed the `.old-*` file. `install.ps1 -BinaryOnly` kept a binary newer than the plugin pin. That check costs about 2 s at each server start, because `offstage-mcp.cmd` runs it whenever the versions differ.

## Restart

The server never kills work. It exits with status 0 only when it is idle:

- no MCP request in flight, and none ended in the last 2 s (the last reply is then on stdout);
- no screen, recording or task of this session;
- no live Bare session (`bare-mcp` `sessions` says `no live sessions`).

When the server cannot tell, it counts as busy. The exit stops Bare too, because Bare is a child of the server.

Claude Code starts the server again on the next tool call (verified 2026-11-17). The server detects Claude Code from the parent process or `CLAUDE_PID`. The launchers keep a binary that is newer than the plugin pin, so the new build stays: `older()` in `bin/offstage-mcp` and `Test-Older` in `install.ps1` only update an older binary.

When the staged update is found while a request runs, the next tool result carries one line: `offstage updated to vX; the server restarts when idle.` It appears once. An idle server exits within seconds, so most updates show no line.

## Two sessions on one machine

Two servers share one binary. A lock file next to the binary (`offstage.update.lock`) serialises download and replace. The second server first asks the binary on disk for its version and skips the download when it is already new enough.

Every server also compares the modification time and size of the binary, `bare-mcp` and `bare-drive` with the values at its start. When they differ, it restarts when idle, with no download.

## Opt out

- `[update] auto = false` in `config.toml`, or
- `OFFSTAGE_NO_AUTO_UPDATE=1` in the environment of the server.

Both turn off the check, the disk watch and the restart.

## Test settings

`OFFSTAGE_UPDATE_BASE=URL` serves `plugin.json`, the release assets with `SHA256SUMS`, and the Bare assets under `bare/`. `OFFSTAGE_UPDATE_DELAY` and `OFFSTAGE_UPDATE_INTERVAL` (Go durations) set the first check and the period.

## Codex and other clients

Codex does not start an exited server again. Verified 2026-11-17 with `codex exec` 0.166.0 and a test MCP server that exits after one call: the second call failed with `Transport closed`. The interactive Codex TUI was not tested.

So the server exits only when its client is Claude Code (or `OFFSTAGE_UPDATE_RESTART=1` is set). For any other client it installs the files and tells the agent once: `offstage updated to vX; restart the MCP server to use it.` The new build runs after the client restarts the server.
