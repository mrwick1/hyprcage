#!/usr/bin/env bash
# The plugin's hook shim checks, at most once an hour on session-start, whether
# the beta branch pins another plugin version. When it has, the shim starts
# `claude plugin update` and tells the session. A beta tester's shim also
# pings the relay with the version and the platform, at most once a day. Runs with a temporary HOME
# and stub commands; it changes nothing on the machine.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/plugin/bin" "$tmp/plugin/.claude-plugin" "$tmp/stub" "$tmp/home"
cp "$here/bin/offstage-hook" "$tmp/plugin/bin/"
printf '{\n  "version": "0.11.0-beta.9"\n}\n' > "$tmp/plugin/.claude-plugin/plugin.json"
cat > "$tmp/stub/curl" <<EOF
#!/bin/sh
case "\$*" in *v1/ping*) echo "\$*" > $tmp/ping-args; cat > $tmp/ping-stdin; exit 0 ;; esac
printf '{\n  "version": "%s"\n}\n' "\$(cat $tmp/latest)"
EOF
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
[ ! -e "$tmp/ping-args" ] || fail "pinged without a beta key"

rm "$tmp/claude-ran"; out=$(run); sleep 0.5
[ -z "$out" ] && [ ! -e "$tmp/claude-ran" ] || fail "checked twice in one hour"

# An hour later it checks again.
echo $(( $(date +%s) - 3700 )) > "$tmp/home/state/offstage/update-check"
out=$(run); sleep 0.5
grep -q 'plugin update' "$tmp/claude-ran" 2>/dev/null || fail "no check after an hour"

rm -r "$tmp/home/state" "$tmp/claude-ran"; echo 0.11.0-beta.9 > "$tmp/latest"; out=$(run); sleep 0.5
[ -z "$out" ] && [ ! -e "$tmp/claude-ran" ] || fail "updated to the same version"

mkdir -p "$tmp/home/.config/offstage"; echo osk_test > "$tmp/home/.config/offstage/beta-key"
rm -r "$tmp/home/state"; out=$(run); sleep 0.5
grep -q 'https://beta.mrwick.dev/v1/ping' "$tmp/ping-args" || fail "no ping"
grep -q '"version":"0.11.0-beta.9","platform":"linux"' "$tmp/ping-args" || fail "ping body: $(cat "$tmp/ping-args")"
grep -q osk_test "$tmp/ping-args" && fail "the beta key is on the command line"
grep -qx 'Authorization: Bearer osk_test' "$tmp/ping-stdin" || fail "the key is not on stdin"

# The hourly check does not ping again on the same day.
rm "$tmp/ping-args"; echo $(( $(date +%s) - 3700 )) > "$tmp/home/state/offstage/update-check"
out=$(run); sleep 0.5
[ ! -e "$tmp/ping-args" ] || fail "pinged twice in one day"

echo PASS
