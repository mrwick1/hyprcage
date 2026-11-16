#!/usr/bin/env bash
# The plugin's launcher, run from Claude Code's plugin cache, must download
# the release that the plugin pins. The cache holds go.mod and cmd/ but no
# .git, so install.sh took it for a checkout and built from source when go
# was on PATH. That binary reported version "source", and the launcher never
# replaced it again. Runs with a temporary HOME, a local release and stub
# commands; it changes nothing on the machine.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
want=v$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' "$here/.claude-plugin/plugin.json" | head -1)
case $(uname -m) in x86_64) a=amd64 ;; aarch64) a=arm64 ;; *) echo "SKIP unknown arch"; exit 0 ;; esac

# The plugin cache: the repository files without .git.
mkdir -p "$tmp/plugin"
git -C "$here" ls-files -z | (cd "$here" && xargs -0 cp --parents -t "$tmp/plugin")
# The release: a stub binary that reports the pinned version.
mkdir -p "$tmp/release" "$tmp/stub"
printf '#!/bin/sh\n[ "$1" = version ] && echo %s\nexit 0\n' "$want" > "$tmp/release/offstage-linux-$a"
(cd "$tmp/release" && sha256sum "offstage-linux-$a" > SHA256SUMS)
# go must not run: it would build from source.
printf '#!/bin/sh\necho go-ran > "%s/go-ran"\nexit 1\n' "$tmp" > "$tmp/stub/go"
chmod +x "$tmp/stub/go"
# The binary in place before the update: an old release, or one built from
# the cache by the old launcher.
for old in v0.0.1 source; do
rm -f "$tmp/go-ran"
mkdir -p "$tmp/home/.local/bin"
printf '#!/bin/sh\n[ "$1" = version ] && echo %s\nexit 0\n' "$old" > "$tmp/home/.local/bin/offstage"
chmod +x "$tmp/home/.local/bin/offstage"

env -i PATH="$tmp/stub:/usr/bin:/bin" HOME="$tmp/home" OFFSTAGE_RELEASE_BASE="file://$tmp/release" \
  bash "$tmp/plugin/bin/offstage-mcp" </dev/null >/dev/null 2>"$tmp/err" || true
got=$("$tmp/home/.local/bin/offstage" version)
if [ "$got" = "$want" ] && [ ! -e "$tmp/go-ran" ]; then echo "PASS the launcher replaces $old with the pinned release $want"
else echo "FAIL from $old: installed version $got (want $want), go ran: $([ -e "$tmp/go-ran" ] && echo yes || echo no)"; cat "$tmp/err"; exit 1; fi
done
