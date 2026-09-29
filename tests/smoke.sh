#!/usr/bin/env bash
# Smoke test of the fork on a live Hyprland session: a screen, the agent
# Chrome, input, recording (also through destroy), clipboard, teardown.
# Assumes the default screen size (1280x800): the click aims at its center.
# It reads the DevTools port from `hyprcage browser`.
set -euo pipefail
HC=${HC:-hyprcage}
S=hc-smoke
page="" out="" out2="" port=""
fail() { echo "FAIL: $*" >&2; exit 1; }
cleanup() {
  $HC destroy $S >/dev/null 2>&1 || true
  rm -f "$page" "$out" "$out2"
}
trap cleanup EXIT

$HC create -name smoke -no-mirror >/dev/null || fail "create"
page=$(mktemp --suffix=.html)
cat >"$page" <<'EOF'
<textarea style="position:fixed;inset:0" oninput="document.title = this.value"></textarea>
EOF
port=$($HC browser $S "file://$page" | jq -r .port) || fail "browser"
[ -n "$port" ] && [ "$port" != null ] || fail "browser reported no port"
$HC wait $S --stable 500ms --timeout 15s >/dev/null || fail "page did not settle"
# The textarea fills the page, so the center of the screen is below the Chrome toolbar.
$HC click $S 640 400 >/dev/null || fail "click"
$HC type $S 'smoke ok' >/dev/null || fail "type"
sleep 1
title=$(curl -sf http://127.0.0.1:$port/json | jq -r '[.[] | select(.type == "page")][0].title') || fail "read page title"
[ "$title" = "smoke ok" ] || fail "typed text: got title '$title'"

$HC record start $S >/dev/null || fail "record start"
sleep 5
out=$($HC record stop $S) || fail "record stop"
frames=$(ffprobe -v error -count_frames -select_streams v -show_entries stream=nb_read_frames -of csv=p=0 "$out") || fail "ffprobe recording"
[ "${frames:-0}" -ge 30 ] || fail "recording has $frames frames"

$HC clip set $S 'smoke-clip' || fail "clip set"
[ "$($HC clip get $S)" = smoke-clip ] || fail "clip get"

# Destroying a recording screen still leaves a playable file.
$HC record start $S >/dev/null || fail "record start 2"
sleep 3
out2=$($HC record status --json | jq -r --arg s "$S" '.[] | select(.target == $s) | .path') || fail "record status"
[ -n "$out2" ] || fail "record status has no entry for $S"
$HC destroy $S >/dev/null || fail "destroy"
sleep 2
frames2=$(ffprobe -v error -count_frames -select_streams v -show_entries stream=nb_read_frames -of csv=p=0 "$out2") ||
  fail "recording cut by destroy is not playable"
[ "${frames2:-0}" -ge 1 ] || fail "recording cut by destroy has $frames2 frames"

# Environ files are binary, so grep needs -a.
left=$(grep -la "HYPRCAGE_SCREEN=$S" /proc/[0-9]*/environ 2>/dev/null | wc -l || true)
[ "$left" -eq 0 ] || fail "$left processes of $S remain"
curl -sf --max-time 1 http://127.0.0.1:$port/json/version >/dev/null && fail "Chrome outlived its screen"
echo "SMOKE OK"
