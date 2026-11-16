#!/usr/bin/env bash
# The plugin's hook shim checks, at most once a day on session-start, whether
# the beta branch pins another plugin version. When it has, the shim starts
# `claude plugin update` and tells the session. Runs with a temporary HOME
# and stub commands; it changes nothing on the machine.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/plugin/bin" "$tmp/plugin/.claude-plugin" "$tmp/stub" "$tmp/home"
cp "$here/bin/offstage-hook" "$tmp/plugin/bin/"
printf '{\n  "version": "0.11.0-beta.9"\n}\n' > "$tmp/plugin/.claude-plugin/plugin.json"
printf '#!/bin/sh\nprintf "{\\n  \\"version\\": \\"%%s\\"\\n}\\n" "$(cat %s/latest)"\n' "$tmp" > "$tmp/stub/curl"
printf '#!/bin/sh\necho "$*" >> %s/claude-ran\n' "$tmp" > "$tmp/stub/claude"
printf '#!/bin/sh\necho "$*" >> %s/offstage-ran\necho noise\n' "$tmp" > "$tmp/stub/offstage"
chmod +x "$tmp/stub/"*
run() { HOME=$tmp/home XDG_STATE_HOME=$tmp/home/state PATH=$tmp/stub:/usr/bin:/bin \
  bash "$tmp/plugin/bin/offstage-hook" session-start </dev/null; }
fail() { echo "FAIL: $*"; exit 1; }

echo 0.11.0-beta.10 > "$tmp/latest"
out=$(run); sleep 0.5
python3 -c "import json,sys; d=json.loads(sys.argv[1]); assert d[\"systemMessage\"].startswith(\"offstage 0.11.0-beta.10 is out\")" "$out" || fail "no update note: $out"
grep -q 'plugin update offstage@offstage-mrwick1' "$tmp/claude-ran" || fail "claude plugin update did not run"
grep -q session-start "$tmp/offstage-ran" || fail "the offstage hook did not run"

rm "$tmp/claude-ran"; out=$(run); sleep 0.5
[ -z "$out" ] && [ ! -e "$tmp/claude-ran" ] || fail "checked twice in one day"

rm -r "$tmp/home/state"; echo 0.11.0-beta.9 > "$tmp/latest"; out=$(run); sleep 0.5
[ -z "$out" ] && [ ! -e "$tmp/claude-ran" ] || fail "updated to the same version"

echo PASS
