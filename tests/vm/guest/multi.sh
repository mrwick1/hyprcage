#!/usr/bin/env bash
# Several screens at once: independent cages, apps, mirrors and workspaces.
set -uo pipefail
H=$HOME/offstage
export XDG_RUNTIME_DIR=/run/user/$(id -u)
export HYPRLAND_INSTANCE_SIGNATURE=${HYPRLAND_INSTANCE_SIGNATURE:-$(ls "$XDG_RUNTIME_DIR/hypr" | head -1)}
OUT=$HOME/spikes; mkdir -p "$OUT"
FAILED=0
pass() { echo "PASS $*"; }
fail() { echo "FAIL $*"; FAILED=1; }
png_size() { python3 -c "import struct,sys; d=open(sys.argv[1],'rb').read(24); print('%dx%d'%struct.unpack('>II',d[16:24]))" "$1"; }
snap() { hyprctl -j monitors | jq -c '[.[]|{name,ws:.activeWorkspace.id}]'; }
EVLOG=$HOME/multi-events.log; rm -f "$EVLOG"
socat -U - "UNIX-CONNECT:$XDG_RUNTIME_DIR/hypr/$HYPRLAND_INSTANCE_SIGNATURE/.socket2.sock" > "$EVLOG" 2>/dev/null &
trap 'kill %1 2>/dev/null' EXIT
before=$(snap)
# One workspace per mirror first (mirror.group = "screen"), then the default
# grouping by session at the end.
mkdir -p ~/.config/offstage; printf '[mirror]\ngroup = "screen"\n' > ~/.config/offstage/config.toml
trap 'kill %1 2>/dev/null; rm -f ~/.config/offstage/config.toml' EXIT

echo "== three screens, different sizes, each with a mirror on its own workspace"
a=$($H create --json --name one --size 1280x800); an=$(echo "$a"|jq -r .name)
b=$($H create --json --name two --size 1024x640); bn=$(echo "$b"|jq -r .name)
c=$($H create --json --name three --size 800x600); cn=$(echo "$c"|jq -r .name)
for x in "$a" "$b" "$c"; do echo "  $(echo "$x"|jq -c '{name,width,height,ws_mirror,cage_pid,inner_display}')"; done
wa=$(echo "$a"|jq -r .ws_mirror); wb=$(echo "$b"|jq -r .ws_mirror); wc_=$(echo "$c"|jq -r .ws_mirror)
wsl=$(printf '%s\n%s\n%s\n' "$wa" "$wb" "$wc_" | sort -n | tr '\n' ' ')
distinct=$(printf '%s\n%s\n%s\n' "$wa" "$wb" "$wc_" | sort -u | wc -l)
inrange=$(printf '%s\n%s\n%s\n' "$wa" "$wb" "$wc_" | awk '$1>=6 && $1<=9' | wc -l)
{ [ "$distinct" = 3 ] && [ "$inrange" = 3 ]; } && pass "three mirrors on three different workspaces ($wsl)" || fail "mirror workspaces: $wsl"
disp=$(for x in "$a" "$b" "$c"; do echo "$x"|jq -r .inner_display; done | sort -u | wc -l)
[ "$disp" = 3 ] && pass "three distinct cage sockets" || fail "$disp distinct cage sockets, want 3"
[ "$(pgrep -xc cage)" = 3 ] && pass "three cage processes" || fail "$(pgrep -xc cage) cage processes"
[ "$(pgrep -fc '_mirror')" = 3 ] && pass "three mirror processes" || fail "$(pgrep -fc '_mirror') mirror processes"

echo "== a different application in each"
$H launch "$an" -- foot sh -c 'while true; do echo AAAA; sleep 0.3; done' >/dev/null
$H launch "$bn" -- foot sh -c 'while true; do echo BBBB; sleep 0.3; done' >/dev/null
$H launch "$cn" -- foot sh -c 'while true; do echo CCCC; sleep 0.3; done' >/dev/null
for n in "$an" "$bn" "$cn"; do
  $H wait "$n" --title foot --timeout 15s >/dev/null 2>&1
  w=$($H windows "$n" --json | jq -r 'length')
  [ "$w" -ge 1 ] && pass "$n has $w window(s) of its own" || fail "$n has no window"
done

echo "== each screen captures at its own size, and they differ"
$H shot "$an" -o "$OUT/m-a.png" >/dev/null; $H shot "$bn" -o "$OUT/m-b.png" >/dev/null; $H shot "$cn" -o "$OUT/m-c.png" >/dev/null
[ "$(png_size "$OUT/m-a.png")" = 1280x800 ] && [ "$(png_size "$OUT/m-b.png")" = 1024x640 ] && [ "$(png_size "$OUT/m-c.png")" = 800x600 ] && pass "captures are 1280x800, 1024x640, 800x600" || fail "sizes: $(png_size "$OUT/m-a.png") $(png_size "$OUT/m-b.png") $(png_size "$OUT/m-c.png")"
cmp -s "$OUT/m-a.png" "$OUT/m-b.png" && fail "two screens captured the same image" || pass "the screens show different things"

echo "== input goes to the right screen only"
rm -f /home/arch/one.out /home/arch/two.out
$H launch "$an" -- foot sh -c "cat > /home/arch/one.out" >/dev/null; sleep 2
$H launch "$bn" -- foot sh -c "cat > /home/arch/two.out" >/dev/null; sleep 2
$H type "$an" "to-one"; $H key "$an" Return; $H key "$an" ctrl+d
$H type "$bn" "to-two"; $H key "$bn" Return; $H key "$bn" ctrl+d
sleep 1
{ grep -q to-one /home/arch/one.out 2>/dev/null && ! grep -q to-two /home/arch/one.out 2>/dev/null; } && pass "screen one received only its own text" || fail "one.out: [$(cat /home/arch/one.out 2>/dev/null|tr '\n' ' ')]"
{ grep -q to-two /home/arch/two.out 2>/dev/null && ! grep -q to-one /home/arch/two.out 2>/dev/null; } && pass "screen two received only its own text" || fail "two.out: [$(cat /home/arch/two.out 2>/dev/null|tr '\n' ' ')]"

echo "== three mirror windows, one per workspace"
mws=$(hyprctl -j clients | jq -c '[.[]|select(.class=="offstage-mirror")|.workspace.id]|sort')
want=$(printf '%s\n%s\n%s\n' "$wa" "$wb" "$wc_" | sort -n | jq -cRs 'split("\n")|map(select(length>0)|tonumber)')
[ "$mws" = "$want" ] && pass "mirror windows on workspaces $mws" || fail "mirror windows on $mws, want $want"
[ "$(hyprctl -j clients | jq -c '[.[]|select(.class=="offstage-mirror")|.fullscreen]|unique')" = "[2]" ] && pass "each mirror fullscreen on its workspace" || fail "fullscreen states: $(hyprctl -j clients | jq -c '[.[]|select(.class=="offstage-mirror")|.fullscreen]')"
[ "$(grep -acE '^monitor(added|removed)' "$EVLOG")" = 0 ] && pass "zero monitor events for three screens" || fail "monitor events appeared"
[ "$before" = "$(snap)" ] && pass "the human's monitors never moved" || fail "human state changed: $(snap)"

echo "== destroying one leaves the others alone"
$H destroy "$bn" >/dev/null; sleep 1
[ "$(pgrep -xc cage)" = 2 ] && pass "one cage gone, two left" || fail "$(pgrep -xc cage) cages left"
[ "$(pgrep -fc '_mirror')" = 2 ] && pass "its mirror went with it, two left" || fail "$(pgrep -fc '_mirror') mirrors left"
$H shot "$an" -o "$OUT/m-a2.png" >/dev/null && [ -s "$OUT/m-a2.png" ] && pass "the other screens still capture" || fail "a surviving screen broke"
left=$(printf '%s\n%s\n' "$wa" "$wc_" | sort -n | jq -cRs 'split("\n")|map(select(length>0)|tonumber)')
[ "$(hyprctl -j clients | jq -c '[.[]|select(.class=="offstage-mirror")|.workspace.id]|sort')" = "$left" ] && pass "the surviving mirrors kept their workspaces ($left)" || fail "mirrors moved"

echo "== a new screen reuses the freed workspace"
d=$($H create --json --name four --size 640x480); dn=$(echo "$d"|jq -r .name)
[ "$(echo "$d"|jq -r .ws_mirror)" = "$wb" ] && pass "the new screen took the freed workspace $wb" || fail "new screen got ws $(echo "$d"|jq -r .ws_mirror), want the freed $wb"

echo "== destroying the rest leaves nothing"
for n in "$an" "$cn" "$dn"; do $H destroy "$n" >/dev/null; done; sleep 1
[ -z "$(pgrep -x cage)" ] && pass "no cage left" || fail "cages left"
[ -z "$(pgrep -f '_mirror')" ] && pass "no mirror left" || fail "mirrors left"
[ -z "$(systemctl --user list-units --all --plain --no-legend 'offstage-*' | awk '{print $1}')" ] && pass "no unit left" || fail "units left"
[ "$before" = "$(snap)" ] && pass "the human's monitors are as they started" || fail "human state: $(snap)"

echo "== mirror.group = session (the default): one workspace per session, mirrors tiled"
rm -f ~/.config/offstage/config.toml
export CLAUDE_CODE_SESSION_ID=session-one
a=$($H create --json --name s1a); an=$(echo "$a"|jq -r .name)
b=$($H create --json --name s1b --size 1024x640); bn=$(echo "$b"|jq -r .name)
c=$($H create --json --name s1c --size 800x600); cn=$(echo "$c"|jq -r .name)
wa=$(echo "$a"|jq -r .ws_mirror); wb=$(echo "$b"|jq -r .ws_mirror); wc_=$(echo "$c"|jq -r .ws_mirror)
{ [ "$wa" = 6 ] && [ "$wb" = 6 ] && [ "$wc_" = 6 ]; } && pass "the session's three mirrors share workspace 6" || fail "session mirrors on $wa $wb $wc_"
sleep 2
mws=$(hyprctl -j clients | jq -c '[.[]|select(.class=="offstage-mirror")|.workspace.id]')
[ "$mws" = "[6,6,6]" ] && pass "three mirror windows on workspace 6" || fail "mirror windows on $mws"
[ "$(hyprctl -j clients | jq -c '[.[]|select(.class=="offstage-mirror")|.fullscreen]|unique')" = "[0]" ] && pass "tiled, none fullscreen" || fail "fullscreen states: $(hyprctl -j clients | jq -c '[.[]|select(.class=="offstage-mirror")|.fullscreen]')"
# Another session gets a workspace of its own.
d=$(CLAUDE_CODE_SESSION_ID=other-session $H create --json --name s2a); dn=$(echo "$d"|jq -r .name)
[ "$(echo "$d"|jq -r .ws_mirror)" = 7 ] && pass "another session's mirror goes to workspace 7" || fail "other session got ws $(echo "$d"|jq -r .ws_mirror)"
# A mirror opened later joins its session's workspace.
$H mirror -close "$bn"; sleep 1; $H mirror "$bn"; sleep 2
[ "$(hyprctl -j clients | jq -c '[.[]|select(.class=="offstage-mirror")|.workspace.id]|sort')" = "[6,6,6,7]" ] && pass "a reopened mirror rejoins workspace 6" || fail "windows on $(hyprctl -j clients | jq -c '[.[]|select(.class=="offstage-mirror")|.workspace.id]|sort')"
[ "$before" = "$(snap)" ] && pass "the human's monitors never moved" || fail "human state: $(snap)"
for n in "$an" "$bn" "$cn"; do $H destroy "$n" >/dev/null; done; CLAUDE_CODE_SESSION_ID=other-session $H destroy "$dn" >/dev/null; sleep 1
[ -z "$(pgrep -x cage)" ] && pass "no cage left" || fail "cages left"
[ -z "$(pgrep -f '_mirror')" ] && pass "no mirror left" || fail "mirrors left"
unset CLAUDE_CODE_SESSION_ID

mws() { hyprctl -j clients | jq -c '[.[]|select(.class=="offstage-mirror")|.workspace.id]|sort'; }

echo "== mirror.group = pack: mirrors of every session fill a workspace before the next"
printf '[mirror]\ngroup = "pack"\nper_workspace = 2\n' > ~/.config/offstage/config.toml
export CLAUDE_CODE_SESSION_ID=session-one
a=$($H create --json --name p1a); an=$(echo "$a"|jq -r .name)
b=$(CLAUDE_CODE_SESSION_ID=other-session $H create --json --name p2a); bn=$(echo "$b"|jq -r .name)
c=$($H create --json --name p1b); cn=$(echo "$c"|jq -r .name)
wa=$(echo "$a"|jq -r .ws_mirror); wb=$(echo "$b"|jq -r .ws_mirror); wc_=$(echo "$c"|jq -r .ws_mirror)
{ [ "$wa" = 6 ] && [ "$wb" = 6 ]; } && pass "two sessions share workspace 6" || fail "pack put them on $wa and $wb"
[ "$wc_" = 7 ] && pass "the third mirror moves to workspace 7 once 6 holds per_workspace" || fail "third mirror on $wc_"
sleep 2
[ "$(mws)" = "[6,6,7]" ] && pass "mirror windows on 6, 6 and 7" || fail "windows on $(mws)"
CLAUDE_CODE_SESSION_ID=other-session $H destroy "$bn" >/dev/null; sleep 1
d=$($H create --json --name p1c); dn=$(echo "$d"|jq -r .name)
[ "$(echo "$d"|jq -r .ws_mirror)" = 6 ] && pass "the seat freed on workspace 6 is taken first" || fail "new mirror on $(echo "$d"|jq -r .ws_mirror)"
sleep 2
[ "$(mws)" = "[6,6,7]" ] && pass "mirror windows back to 6, 6 and 7" || fail "windows on $(mws)"
for n in "$an" "$cn" "$dn"; do $H destroy "$n" >/dev/null; done; sleep 1

echo "== mirror.per_workspace caps a session's workspace as well"
printf '[mirror]\nper_workspace = 2\n' > ~/.config/offstage/config.toml
a=$($H create --json --name c1a); an=$(echo "$a"|jq -r .name)
b=$($H create --json --name c1b); bn=$(echo "$b"|jq -r .name)
c=$($H create --json --name c1c); cn=$(echo "$c"|jq -r .name)
ws3="$(echo "$a"|jq -r .ws_mirror) $(echo "$b"|jq -r .ws_mirror) $(echo "$c"|jq -r .ws_mirror)"
[ "$ws3" = "6 6 7" ] && pass "a session's third mirror overflows to workspace 7" || fail "session mirrors on $ws3"
for n in "$an" "$bn" "$cn"; do $H destroy "$n" >/dev/null; done; sleep 1
rm -f ~/.config/offstage/config.toml
[ -z "$(pgrep -x cage)" ] && pass "no cage left" || fail "cages left"
[ -z "$(pgrep -f '_mirror')" ] && pass "no mirror left" || fail "mirrors left"
[ "$before" = "$(snap)" ] && pass "the human's monitors never moved" || fail "human state: $(snap)"
unset CLAUDE_CODE_SESSION_ID

echo; [ "$FAILED" = 0 ] && echo "ALL PASS" || echo "SOME FAILURES"; exit $FAILED
