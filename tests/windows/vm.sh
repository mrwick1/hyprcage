#!/usr/bin/env bash
# Start, stop and reset the Windows test VM on the host that holds it.
# The VM was made with `quickget windows 11`. See README.md in this folder.
set -euo pipefail

dir=${HYPRCAGE_WINVM_DIR:-/data/vms}
name=windows-11-English-United-States
vm=$dir/$name
unit=winvm
key=${HYPRCAGE_WINVM_KEY:-$HOME/.ssh/hyprcage-winvm}
port=22220

ssh_vm() {
	ssh -i "$key" -o BatchMode=yes -o UserKnownHostsFile="$dir/known_hosts" \
		-o ConnectTimeout=5 -p "$port" Quickemu@127.0.0.1 "$@"
}

wait_ssh() {
	for _ in $(seq 1 90); do
		ssh_vm 'hostname' >/dev/null 2>&1 && return 0
		systemctl --user is-active -q "$unit" || { echo "vm stopped" >&2; return 1; }
		sleep 5
	done
	echo "ssh did not answer" >&2
	return 1
}

case ${1:-} in
start)
	systemctl --user is-active -q "$unit" && { echo "already running"; exit 0; }
	# launch-stdvga.sh is quickemu's script with qxl-vga replaced by -vga std.
	# Windows 11 build 26300 shows a black screen on qxl-vga.
	systemd-run --user --unit="$unit" --collect --working-directory="$dir" \
		bash "$vm/launch-stdvga.sh" >/dev/null
	wait_ssh && echo "ready: ssh -p $port Quickemu@127.0.0.1"
	;;
stop)
	ssh_vm 'Stop-Computer -Force' >/dev/null 2>&1 || true
	for _ in $(seq 1 60); do
		systemctl --user is-active -q "$unit" || { echo "stopped"; exit 0; }
		sleep 3
	done
	systemctl --user stop "$unit"
	;;
reset)
	systemctl --user is-active -q "$unit" && { echo "stop the vm first" >&2; exit 1; }
	qemu-img snapshot -a clean-base "$vm/disk.qcow2"
	cp -a "$vm/clean-base/OVMF_VARS.fd" "$vm/clean-base/tpm2-00.permall" "$vm/"
	echo "reset to clean-base"
	;;
ssh)
	shift
	ssh_vm "$@"
	;;
view)
	exec spicy -h 127.0.0.1 -p 5930
	;;
*)
	echo "usage: $0 start|stop|reset|ssh [cmd]|view" >&2
	exit 2
	;;
esac
