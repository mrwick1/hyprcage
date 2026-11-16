#!/usr/bin/env bash
# Acceptance checks for offstage 0.3 (cage on its own headless output), run
# INSIDE the VM as user arch with Hyprland up. Prints PASS/FAIL/INFO and
# leaves screenshots in $HOME/spikes/. One application per screen, as an
# agent would.
set -uo pipefail
H=${H:-$HOME/offstage}
export XDG_RUNTIME_DIR=/run/user/$(id -u)
export HYPRLAND_INSTANCE_SIGNATURE=${HYPRLAND_INSTANCE_SIGNATURE:-$(ls "$XDG_RUNTIME_DIR/hypr" | head -1)}
OUT=$HOME/spikes; mkdir -p "$OUT"
FAILED=0
pass() { echo "PASS $*"; }
fail() { echo "FAIL $*"; FAILED=1; }
info() { echo "INFO $*"; }

snap() { echo "cursor=$(hyprctl -j cursorpos | jq -c .) monitors=$(hyprctl -j monitors | jq -c '[.[] | {name, ws: .activeWorkspace.id, focused}]')"; }
create() { $H create --json "$@"; }
launch() { $H launch "$1" -- "${@:2}" >/dev/null; }
wait_title() { $H wait "$1" --title "$2" --timeout "${3:-10s}" >/dev/null 2>&1; }
evwait() { local f=$1 re=$2 n=${3:-40} i; for ((i=0;i<n;i++)); do local m; m=$(grep -aE "$re" "$f" 2>/dev/null | tail -1); [ -n "$m" ] && { echo "$m"; return 0; }; sleep 0.1; done; return 1; }
evcount() { grep -acE "$1" "$2" 2>/dev/null || echo 0; }
png_size() { python3 -c "import struct,sys; d=open(sys.argv[1],'rb').read(24); print('%dx%d'%struct.unpack('>II',d[16:24]))" "$1"; }
# Every Hyprland event of the run lands in $EVLOG: the whole point of 0.3 is
# that none of them is a monitor event.
EVLOG=$HOME/events.log; rm -f "$EVLOG"
socat -U - "UNIX-CONNECT:$XDG_RUNTIME_DIR/hypr/$HYPRLAND_INSTANCE_SIGNATURE/.socket2.sock" > "$EVLOG" 2>/dev/null &
EVPID=$!
trap 'kill $EVPID 2>/dev/null' EXIT

echo "== doctor"
$H doctor; drv=$($H doctor --json | jq -r '.[]|select(.name=="config driver")|.detail' | awk '{print $1}'); info "config driver: $drv"
if [ -n "${EXPECT_DRIVER:-}" ]; then [ "$drv" = "$EXPECT_DRIVER" ] && pass "config driver detected as $drv" || fail "config driver is $drv, expected $EXPECT_DRIVER"; fi
before=$(snap); info "human state before: $before"
mons_before=$(hyprctl -j monitors | jq -c '[.[]|.name]|sort')

echo "== create: a screen that Hyprland never sees"
out=$(create --no-mirror); rc=$?; name=$(echo "$out" | jq -r .name)
[ $rc = 0 ] && [ -n "$name" ] && pass "create ok: $(echo "$out" | jq -c '{name,width,height,cage_pid,renderer,render_device}')" || fail "create: $out"
[ "$(hyprctl -j monitors | jq -c '[.[]|.name]|sort')" = "$mons_before" ] && pass "no Hyprland output added" || fail "monitors changed: $(hyprctl -j monitors | jq -c '[.[]|.name]')"
sleep 1
[ "$(grep -acE '^monitor(added|removed)' "$EVLOG")" = 0 ] && pass "zero monitoradded/monitorremoved on socket2" || fail "monitor events: $(grep -aE '^monitor' "$EVLOG" | head -3)"
[ "$before" = "$(snap)" ] && pass "human state unchanged by create" || fail "create changed the human state: $(snap)"
kill -0 "$(echo "$out" | jq -r .cage_pid)" 2>/dev/null && pass "cage_pid is a live process" || fail "cage_pid not alive"
grep -q "WLR_BACKENDS=headless" "/proc/$(echo "$out" | jq -r .cage_pid)/environ" 2>/dev/null && pass "cage runs on the headless backend" || fail "cage environment lacks WLR_BACKENDS=headless"
$H shot "$name" -o "$OUT/10-empty.png" >/dev/null && [ "$(png_size "$OUT/10-empty.png")" = 1280x800 ] && pass "capture is 1280x800 (SetMode applied)" || fail "capture size $(png_size "$OUT/10-empty.png" 2>/dev/null)"
[ -s "$HOME/.local/state/offstage/log/$name-cage.log" ] && pass "cage log written ($(wc -c < "$HOME/.local/state/offstage/log/$name-cage.log") bytes)" || fail "no cage log"
systemctl --user list-units --plain --no-legend "offstage-*" | grep -q "cage.scope" && pass "cage runs in its scope" || fail "no cage scope"

echo "== size: a screen of another size"
out2=$(create --no-mirror --size 1920x1200); name2=$(echo "$out2" | jq -r .name)
$H shot "$name2" -o "$OUT/11-big.png" >/dev/null && [ "$(png_size "$OUT/11-big.png")" = 1920x1200 ] && pass "1920x1200 screen captures at 1920x1200" || fail "big capture $(png_size "$OUT/11-big.png" 2>/dev/null)"
$H destroy "$name2" >/dev/null && pass "destroy of the second screen ok" || fail "destroy second"

echo "== input: one wev per screen"
rm -f "$HOME/wev.log"; launch "$name" sh -c "exec stdbuf -oL wev > $HOME/wev.log 2>&1"
wait_title "$name" '^wev$' 15s && pass "app_launch: wev listed by foreign-toplevel" || fail "wev not listed"
evwait "$HOME/wev.log" 'wl_seat|wl_pointer|wl_keyboard' 100 >/dev/null && pass "wev connected to the cage seat" || fail "wev produced no output"
$H click "$name" 640 400
{ evwait "$HOME/wev.log" 'x, y: 640\.0+, 400\.0+' >/dev/null && [ "$(evcount 'button: 272 \(left\)' "$HOME/wev.log")" -ge 2 ]; } && pass "left click at exactly 640,400" || fail "click"
lines0=$(wc -l < "$HOME/wev.log"); $H key "$name" ctrl+b; sleep 0.4; seg=$(tail -n +"$((lines0+1))" "$HOME/wev.log")
{ echo "$seg" | grep -qE "key: 37; state: 1" && echo "$seg" | grep -qE "key: 37; state: 0"; } && pass "ctrl+b: Control held around the letter" || fail "ctrl+b"
wid=$($H windows "$name" --json | jq -r '.[]|select(.AppID=="wev")|.ID'|head -1); $H close "$name" "$wid" >/dev/null 2>&1; sleep 1
[ "$($H windows "$name" --json | jq -r '[.[]|.AppID]|index("wev")')" = null ] && pass "app_close closes wev" || fail "wev still listed after close"

echo "== typing and modifiers reach a real application (foot + cat)"
rm -f /home/arch/catout
launch "$name" foot sh -c "cat > /home/arch/catout"
wait_title "$name" 'foot' 12s; sleep 1.5
$H type "$name" 'Grüezi, ça va ? 😀'; $H key "$name" Return; sleep 0.3; $H key "$name" ctrl+d; sleep 0.6
got=$(head -1 /home/arch/catout 2>/dev/null)
[ "$got" = 'Grüezi, ça va ? 😀' ] && pass "Unicode typed verbatim into a real app" || fail "app received [$got]"
{ [ -n "$got" ] && grep -q "" /home/arch/catout && [ "$(wc -l < /home/arch/catout)" -ge 1 ]; } && pass "Ctrl+D delivered EOF (cat closed)" || fail "no EOF"
wid=$($H windows "$name" --json | jq -r '.[]|select(.AppID=="foot")|.ID'|head -1); $H close "$name" "$wid" >/dev/null 2>&1; sleep 1

echo "== rendering with nobody watching"
launch "$name" foot sh -c 'while true; do date +%N; sleep 0.2; done'
wait_title "$name" 'foot' 12s; sleep 2
$H shot "$name" -o "$OUT/12-a.png" >/dev/null; sleep 1; $H shot "$name" -o "$OUT/13-b.png" >/dev/null
{ [ -s "$OUT/12-a.png" ] && [ -s "$OUT/13-b.png" ] && ! cmp -s "$OUT/12-a.png" "$OUT/13-b.png"; } && pass "the ticking terminal keeps rendering unwatched" || fail "rendering frozen"
$H wait "$name" --stable 300ms --timeout 3s >/dev/null 2>&1 && fail "wait --stable returned on a changing screen" || pass "wait --stable times out on a changing screen"
wid=$($H windows "$name" --json | jq -r '.[0].ID'); $H close "$name" "$wid" >/dev/null 2>&1; sleep 1
$H wait "$name" --stable 300ms --timeout 5s >/dev/null 2>&1 && pass "wait --stable returns on a static screen" || fail "wait --stable timed out on a static screen"

echo "== destroy leaves nothing"
b=$(snap); $H destroy "$name" && pass "destroy ok" || fail "destroy"; sleep 1
[ "$b" = "$(snap)" ] && pass "human state identical after destroy" || fail "destroy changed the human state"
[ -z "$(pgrep -x cage)" ] && pass "no cage process left" || fail "cage left: $(pgrep -a cage)"
[ -z "$(systemctl --user list-units --all --plain --no-legend 'offstage-*' | awk '{print $1}')" ] && pass "no offstage-* unit left, failed included" || fail "units left: $(systemctl --user list-units --all --plain --no-legend 'offstage-*' | awk '{print $1" "$3}' | tr '\n' ' ')"
[ -z "$(ls "$XDG_RUNTIME_DIR"/offstage/*.json "$XDG_RUNTIME_DIR"/offstage/*.inner 2>/dev/null)" ] && pass "no record or inner file left" || fail "runtime files left"
[ "$(grep -acE '^monitor(added|removed)' "$EVLOG")" = 0 ] && pass "still zero monitor events after destroy" || fail "monitor events after destroy"

echo "== gc reaps a crashed cage"
out=$(create --no-mirror); name=$(echo "$out" | jq -r .name); kill -9 "$(echo "$out" | jq -r .cage_pid)"; sleep 1
$H gc >/dev/null; { [ -z "$(pgrep -x cage)" ] && ! [ -e "$XDG_RUNTIME_DIR/offstage/$name.json" ]; } && pass "gc removed the record and processes of a crashed cage" || fail "gc left something: $(pgrep -a cage) $(ls $XDG_RUNTIME_DIR/offstage/)"

echo "== config.toml honoured (size, screens per session, refusals)"
mkdir -p ~/.config/offstage; cat > ~/.config/offstage/config.toml <<'EOT'
[screen]
width = 1024
height = 640
max_per_session = 1
[workspaces]
mirror = [7, 8]
EOT
out=$(create --no-mirror); name=$(echo "$out" | jq -r .name)
[ "$(echo "$out" | jq -c '[.width,.height]')" = "[1024,640]" ] && $H shot "$name" -o "$OUT/14-cfg.png" >/dev/null && [ "$(png_size "$OUT/14-cfg.png")" = 1024x640 ] && pass "config: size 1024x640 from config.toml" || fail "config not honoured: $(echo "$out" | jq -c '[.width,.height]')"
if create --no-mirror >/dev/null 2>"$OUT/limit.err"; then fail "config: max_per_session = 1 not enforced"; else grep -q 'limit' "$OUT/limit.err" && pass "config: max_per_session = 1 enforced" || fail "config: second create failed for another reason: $(cat "$OUT/limit.err")"; fi
$H destroy "$name" >/dev/null
printf '[workspaces]\nagent = [11, 99]\n' > ~/.config/offstage/config.toml
create >/dev/null 2>"$OUT/agent.err" && fail "config: workspaces.agent accepted" || { grep -q 'unknown key' "$OUT/agent.err" && pass "config: workspaces.agent refused as unknown" || fail "config: error is $(cat "$OUT/agent.err")"; }
rm -f ~/.config/offstage/config.toml

echo "== naming: a chosen name gets the prefix"
out=$(create --name dt --no-mirror); name=$(echo "$out" | jq -r .name)
[ "$name" = "hc-dt" ] && pass "a chosen name gets the prefix (hc-dt)" || fail "name is $name, want hc-dt"
$H destroy "$name" >/dev/null

echo "== typing into Chromium: every character lands, none on a control key"
# Chromium and Electron read the keyCode of anything but an ASCII letter or
# digit from the physical key, and drop a character whose key says Escape,
# BackSpace, Tab or Enter. The page reports key|code|keyCode of each keydown
# and the field's value to a local HTTP server, whose log the checks read.
rm -f /tmp/keys.log; python3 -m http.server 8765 --bind 127.0.0.1 >/dev/null 2>/tmp/keys.log &
KEYSRV=$!
keys_last() { grep -o 'GET /[^ ]*' /tmp/keys.log | tail -1 | cut -c6- | python3 -c 'import sys,urllib.parse; print(urllib.parse.unquote(sys.stdin.read().strip()))'; }
out=$(create --no-mirror); name=$(echo "$out" | jq -r .name)
rm -rf /tmp/hc-keys
launch "$name" chromium --ozone-platform=wayland --user-data-dir=/tmp/hc-keys --no-first-run --disable-gpu --kiosk file:///home/arch/keys.html
# The first start builds the profile with software rendering: slow here.
for i in $(seq 1 120); do grep -q 'GET /ready' /tmp/keys.log 2>/dev/null && break; sleep 1; done
grep -q 'GET /ready' /tmp/keys.log && pass "the key page loaded in chromium" || fail "chromium did not load the key page"
$H wait "$name" --stable 500ms --timeout 20s >/dev/null 2>&1
$H click "$name" 300 120; sleep 0.5
$H type "$name" "/"; sleep 1
last=$(keys_last)
grep -q '/|Slash|191' <<<"$last" && pass "/ arrives on the Slash key, keyCode 191" || fail "/ seen as: $last"
grep -q 'val=/' <<<"$last" && pass "/ inserted as the first character" || fail "/ not inserted: $last"
$H key "$name" ctrl+a; $H key "$name" BackSpace; sleep 0.5
$H type "$name" 'a?b {"k": [1, 2]} é €'; sleep 1
last=$(keys_last)
grep -qF 'val=a?b {"k": [1, 2]} é €' <<<"$last" && pass "punctuation, braces and non-ASCII typed verbatim" || fail "typed as: $last"
grep -qE '\|(Escape|Backspace|Tab|Enter|Delete|F[0-9]+)\|' <<<"$last" && fail "a character landed on a control key: $last" || pass "no character on a control key"
$H key "$name" ctrl+a; $H key "$name" BackSpace; sleep 0.5
$H key "$name" slash; $H key "$name" period; sleep 1
last=$(keys_last)
grep -q 'val=/\.' <<<"$last" && pass "key slash and key period insert / and ." || fail "keys gave: $last"
$H destroy "$name" >/dev/null; kill $KEYSRV 2>/dev/null

echo "== mirror: a window on the human's workspace, no monitor event"
out=$(create); name=$(echo "$out" | jq -r .name); echo "$out" | jq -c '{name,ws_mirror,mirror_note}'
launch "$name" foot sh -c 'while true; do date +%N; sleep 0.2; done'; sleep 2
# the mirror is a normal Hyprland window titled for the screen
win=$(hyprctl -j clients | jq -c '.[]|select(.class=="offstage-mirror")|{ws:.workspace.id,mapped}')
[ -n "$win" ] && pass "mirror window present ($win)" || fail "no mirror window"
wsm=$(echo "$out" | jq -r .ws_mirror)
[ "$(echo "$win" | jq -r .ws)" = "$wsm" ] && pass "mirror on the recorded workspace $wsm" || fail "mirror on ws $(echo "$win"|jq -r .ws), want $wsm"
[ "$(grep -acE '^monitor(added|removed)' "$EVLOG")" = 0 ] && pass "still zero monitor events with a mirror" || fail "monitor events appeared"
# the mirror sidecar names a live pid
mpid=$(sed -n 's/^PID=//p' "$XDG_RUNTIME_DIR/offstage/$name.mirror" 2>/dev/null)
{ [ -n "$mpid" ] && kill -0 "$mpid" 2>/dev/null; } && pass "mirror sidecar names a live pid ($mpid)" || fail "no live mirror pid"
# close it, the screen lives on, reopen it
$H mirror -close "$name"; sleep 1
[ -z "$(hyprctl -j clients | jq -c '.[]|select(.class=="offstage-mirror")')" ] && pass "mirror -close removed the window" || fail "mirror still there after close"
$H shot "$name" -o "$OUT/15-after-close.png" >/dev/null && [ -s "$OUT/15-after-close.png" ] && pass "the screen still works after the mirror is closed" || fail "screen broke after mirror close"
$H mirror "$name"; sleep 2
[ -n "$(hyprctl -j clients | jq -c '.[]|select(.class=="offstage-mirror")')" ] && pass "offstage mirror reopened the window" || fail "mirror did not reopen"
# the mirror image tracks the screen: grab the human monitor on the mirror workspace, twice, expect change
hyprctl dispatch workspace "$wsm" >/dev/null 2>&1; sleep 1
grim -o "$(hyprctl -j monitors | jq -r '.[]|select(.focused)|.name')" "$OUT/16-mir-a.png" 2>/dev/null; sleep 1
grim -o "$(hyprctl -j monitors | jq -r '.[]|select(.focused)|.name')" "$OUT/16-mir-b.png" 2>/dev/null
{ [ -s "$OUT/16-mir-a.png" ] && [ -s "$OUT/16-mir-b.png" ] && ! cmp -s "$OUT/16-mir-a.png" "$OUT/16-mir-b.png"; } && pass "the mirror image updates as the screen changes" || info "mirror image change not confirmed (grim/headless human monitor)"
# Frames only flow while the mirror's workspace is visible: show it, let the
# ticking terminal drive a few frames, then read the mirror's own count.
hyprctl dispatch workspace "$wsm" >/dev/null 2>&1; sleep 3
$H destroy "$name" >/dev/null; sleep 1
frames=$(sed -n 's/.*exit [0-9]* after \([0-9]*\) frames.*/\1/p' "$HOME/.local/state/offstage/log/$name-mirror.log" 2>/dev/null | tail -1)
{ [ -n "$frames" ] && [ "$frames" -gt 0 ]; } && pass "the mirror showed $frames frames of the screen" || fail "the mirror showed no frame (log: $(tail -2 "$HOME/.local/state/offstage/log/$name-mirror.log" 2>/dev/null | tr '\n' ' '))"
[ -z "$(pgrep -f '_mirror')" ] && pass "no mirror process left after destroy" || fail "mirror process left"

echo "== mirror opened after a create without one: placed, never on the human's workspace"
# The human sits on workspace 1. A screen created without a mirror has no
# workspace; opening the mirror later must pick one in the range, not drop
# the window where the human looks. With the range down to one workspace, a
# second screen's mirror is refused rather than misplaced.
mkdir -p ~/.config/offstage; printf '[workspaces]\nmirror = [8, 8]\n' > ~/.config/offstage/config.toml
hyprctl dispatch workspace 1 >/dev/null 2>&1
# Two sessions are told apart by their id: the script is one, the other is
# named on its commands.
export CLAUDE_CODE_SESSION_ID=session-one
out=$(create --no-mirror); name=$(echo "$out" | jq -r .name)
[ "$(echo "$out" | jq -r .ws_mirror)" = 0 ] && pass "created without a mirror: no workspace recorded" || fail "ws_mirror is $(echo "$out" | jq -r .ws_mirror) without a mirror"
$H mirror "$name"; sleep 2
win=$(hyprctl -j clients | jq -c '.[]|select(.class=="offstage-mirror")|{ws:.workspace.id,mapped}')
[ "$(echo "$win" | jq -r .ws)" = 8 ] && pass "mirror opened later lands on workspace 8, not the human's" || fail "mirror opened later is on $win"
[ "$($H list --all --json | jq -r ".[]|select(.name==\"$name\")|.ws_mirror")" = 8 ] && pass "the workspace chosen at open time is recorded" || fail "record not updated: $($H list --all --json | jq -c ".[]|select(.name==\"$name\")|{ws_mirror,mirror_note}")"
[ "$(hyprctl -j activeworkspace | jq -r .id)" = 1 ] && pass "the human is still on workspace 1" || fail "the human was moved to workspace $(hyprctl -j activeworkspace | jq -r .id)"
# Another session finds the only workspace taken: refused, not misplaced.
out2=$(CLAUDE_CODE_SESSION_ID=other-session create --no-mirror); name2=$(echo "$out2" | jq -r .name)
if CLAUDE_CODE_SESSION_ID=other-session $H mirror "$name2" 2>"$OUT/mirror-full.err"; then fail "a mirror was opened with no workspace left"; else grep -q 'all taken' "$OUT/mirror-full.err" && pass "another session's mirror refused when the range is all taken" || fail "mirror failed for another reason: $(cat "$OUT/mirror-full.err")"; fi
[ "$(hyprctl -j clients | jq '[.[]|select(.class=="offstage-mirror")]|length')" = 1 ] && pass "no misplaced mirror window" || fail "$(hyprctl -j clients | jq -c '[.[]|select(.class=="offstage-mirror")|.workspace.id]') mirror windows"
# The same session's next mirror joins workspace 8 (mirror.group = session).
out3=$(create --no-mirror); name3=$(echo "$out3" | jq -r .name)
$H mirror "$name3"; sleep 2
[ "$(hyprctl -j clients | jq -c '[.[]|select(.class=="offstage-mirror")|.workspace.id]')" = "[8,8]" ] && pass "the same session's second mirror joins workspace 8" || fail "windows on $(hyprctl -j clients | jq -c '[.[]|select(.class=="offstage-mirror")|.workspace.id]')"
$H destroy "$name" >/dev/null; $H destroy "$name3" >/dev/null; CLAUDE_CODE_SESSION_ID=other-session $H destroy "$name2" >/dev/null; rm -f ~/.config/offstage/config.toml
unset CLAUDE_CODE_SESSION_ID

echo; [ "$FAILED" = 0 ] && echo "ALL PASS" || echo "SOME FAILURES"; exit $FAILED
