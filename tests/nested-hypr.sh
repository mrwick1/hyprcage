#!/usr/bin/env bash
# Start or stop a nested Hyprland on the hidden special workspace
# special:hyprcage-e2e of the human's Hyprland. The desktop tests point
# HYPRCAGE_DESKTOP_INSTANCE at it, so they never touch the human's screens.
#
#   tests/nested-hypr.sh start        prints the signature of the new instance
#   tests/nested-hypr.sh stop <sig>   exits that instance
set -euo pipefail

base="$XDG_RUNTIME_DIR/hypr"

case "${1:-}" in
start)
	dir=$(mktemp -d /tmp/hc-nested.XXXXXX)
	cat >"$dir/hyprland.conf" <<'EOF'
misc {
  disable_hyprland_logo = true
  disable_splash_rendering = true
}
EOF
	cmd="env -u HYPRLAND_INSTANCE_SIGNATURE Hyprland -c $dir/hyprland.conf"
	before=$(ls "$base")
	if hyprctl eval 'return 1' >/dev/null 2>&1; then
		hyprctl eval "hl.exec_cmd(\"$cmd\", { workspace = \"special:hyprcage-e2e silent\", no_initial_focus = true })" >/dev/null
	else
		hyprctl dispatch exec "[workspace special:hyprcage-e2e silent; noinitialfocus] $cmd" >/dev/null
	fi
	for _ in $(seq 50); do
		sig=$(comm -13 <(echo "$before" | sort) <(ls "$base" | sort) | head -1)
		if [ -n "$sig" ] && HYPRLAND_INSTANCE_SIGNATURE=$sig hyprctl monitors -j >/dev/null 2>&1; then
			# The host window sits on a hidden workspace, so its output never
			# renders and screencopy stalls. A headless output renders, and at
			# 1920x1080 scale 1.5 it matches the human's monitor.
			export HYPRLAND_INSTANCE_SIGNATURE=$sig
			hyprctl output create headless HC >/dev/null
			hyprctl keyword monitor "HC,1920x1080@60,0x0,1.5" >/dev/null
			hyprctl keyword monitor "WAYLAND-1,disable" >/dev/null
			echo "$sig"
			exit 0
		fi
		sleep 0.2
	done
	echo "nested Hyprland did not start" >&2
	exit 1
	;;
stop)
	HYPRLAND_INSTANCE_SIGNATURE="${2:?signature}" hyprctl dispatch exit >/dev/null 2>&1 || true
	;;
*)
	echo "usage: $0 start | stop <signature>" >&2
	exit 2
	;;
esac
