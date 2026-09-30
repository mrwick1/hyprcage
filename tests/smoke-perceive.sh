#!/usr/bin/env bash
# Smoke test of perception on a live Hyprland session. For each application:
# create a screen, launch it, snapshot, find a known element, act on it and
# check that the diff shows the expected change. Every failure is reported by
# step, and the script exits non-zero when any step failed.
set -uo pipefail
HC=${HC:-hyprcage}
failed=0
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fail() { echo "FAIL [$1] $2" >&2; failed=1; }

# ref <screen> <regexp> [role]: the ref of the first element that find reports.
ref() {
  local args=(-timeout 15000)
  [ -n "${3:-}" ] && args+=(-role "$3")
  $HC find "${args[@]}" "$1" "$2" | grep -om1 '^\[[eo][0-9]*\]' | tr -d '[]'
}

# row <name> <expect-regexp> <target-regexp> <role> <launch...>
row() {
  local name=$1 expect=$2 target=$3 role=$4
  shift 4
  local s=hc-sp-$name r diff
  $HC create -name "sp-$name" -no-mirror >/dev/null || { fail "$name" create; return; }
  if ! "$@" "$s"; then fail "$name" launch; $HC destroy "$s" >/dev/null 2>&1; return; fi
  # find waits for the element, so the snapshot after it sees the rendered app.
  r=$(ref "$s" "$target" "$role")
  $HC snapshot "$s" >"$tmp/$name.snap" || fail "$name" snapshot
  if [ -z "$r" ]; then
    fail "$name" "find '$target'"
  elif ! diff=$($HC act "$s" "$r" click); then
    fail "$name" "act $r click"
  elif ! grep -qE "$expect" <<<"$diff"; then
    fail "$name" "diff lacks /$expect/: $(head -3 <<<"$diff" | tr '\n' ' ')"
  else
    echo "ok   [$name] $(head -1 "$tmp/$name.snap") act $r → $(wc -l <<<"$diff") diff lines"
  fi
  $HC destroy "$s" >/dev/null 2>&1 || fail "$name" destroy
}

launch_code() {
  $HC launch -debug "$1" -- code --user-data-dir="$tmp/code" --extensions-dir="$tmp/code-ext" >/dev/null
}
# The same launch path as the other rows; `hyprcage browser` also gets a per-screen port.
launch_chrome() {
  $HC launch -debug "$1" -- ${CHROME:-google-chrome-stable} --ozone-platform=wayland \
    --user-data-dir="$tmp/chrome" --no-first-run --no-default-browser-check https://example.org >/dev/null
}
launch_thunar() { $HC launch "$1" -- thunar "$tmp" >/dev/null; }
launch_wireshark() { $HC launch "$1" -- wireshark >/dev/null; }

row code '^- \[e[0-9]+\] button' '^Continue without Signing In$' button launch_code
row chrome '^[+~-] ' '^(More information|Learn more)' link launch_chrome
row thunar '^\+ \[e[0-9]+\] menuitem' '^View$' menuitem launch_thunar
row wireshark '^\+ \[e[0-9]+\] menuitem' '^Help$' menuitem launch_wireshark

[ $failed = 0 ] && echo "smoke-perceive: all rows passed"
exit $failed
