#!/usr/bin/env bash
# Smoke test of the fork on a live Hyprland session: a screen, the agent
# Chrome, input, recording (also through destroy), clipboard, teardown.
set -euo pipefail
HC=${HC:-hyprcage}
S=hc-smoke
page="" out="" out2=""
fail() { echo "FAIL: $*" >&2; exit 1; }
cleanup() {
  $HC destroy $S >/dev/null 2>&1 || true
  rm -f "$page" "$out" "$out2"
}
trap cleanup EXIT

$HC create -name smoke -no-mirror >/dev/null || fail "create"
page=$(mktemp --suffix=.html)
cat >"$page" <<'EOF'
<textarea autofocus oninput="document.title = this.value"></textarea>
EOF
$HC browser $S "file://$page" >/dev/null || fail "browser"
$HC wait $S --stable 500ms --timeout 15s >/dev/null || fail "page did not settle"
$HC click $S 90 20 >/dev/null
$HC type $S 'smoke ok' >/dev/null
sleep 1
title=$(curl -sf http://127.0.0.1:9222/json | jq -r '[.[] | select(.type == "page")][0].title')
[ "$title" = "smoke ok" ] || fail "typed text: got title '$title'"

$HC record start $S >/dev/null || fail "record start"
sleep 5
out=$($HC record stop $S) || fail "record stop"
frames=$(ffprobe -v error -count_frames -select_streams v -show_entries stream=nb_read_frames -of csv=p=0 "$out")
[ "${frames:-0}" -ge 30 ] || fail "recording has $frames frames"

$HC clip set $S 'smoke-clip' || fail "clip set"
[ "$($HC clip get $S)" = smoke-clip ] || fail "clip get"

# Destroying a recording screen still leaves a playable file.
$HC record start $S >/dev/null || fail "record start 2"
sleep 3
out2=$($HC record status --json | jq -r '.[] | select(.target == "'$S'") | .path')
[ -n "$out2" ] || fail "record status has no entry for $S"
$HC destroy $S >/dev/null || fail "destroy"
sleep 2
ffprobe -v error "$out2" || fail "recording cut by destroy is not playable"

# Environ files are binary, so grep needs -a.
left=$(grep -la "HYPRCAGE_SCREEN=$S" /proc/[0-9]*/environ 2>/dev/null | wc -l || true)
[ "$left" -eq 0 ] || fail "$left processes of $S remain"
curl -sf --max-time 1 http://127.0.0.1:9222/json/version >/dev/null && fail "Chrome outlived its screen"
echo "SMOKE OK"
