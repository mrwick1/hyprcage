#!/usr/bin/env bash
# install.sh, piped into bash as curl ... | bash runs it, must run to the end
# when an agent CLI fails to remove a name that is not registered. The stub
# claude fails as the real one does. Before the fix, set -e ended the script
# there: a migration from hyprcage removed the old binary and installed no new
# one. Runs with a temporary HOME and stub commands; it changes nothing on the
# machine.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/stub"
printf '#!/bin/sh\nexit 1\n' > "$tmp/stub/claude"
printf '#!/bin/sh\nexit 0\n' > "$tmp/stub/systemctl"
chmod +x "$tmp/stub/claude" "$tmp/stub/systemctl"
out=$(env -i PATH="$tmp/stub:/usr/bin:/bin" HOME="$tmp/home" bash -s -- --uninstall < "$here/install.sh" 2>&1) || true
if grep -q "^.*done\. cage" <<<"$out"; then echo "PASS install.sh runs to the end when claude mcp remove fails"
else echo "FAIL install.sh stopped early:"; echo "$out"; exit 1; fi
