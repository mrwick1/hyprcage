#!/usr/bin/env bash
# Phase-0 VM for offstage: a vanilla Arch Linux guest built from the official
# cloud image, driven over SSH, with no window on the host desktop. Everything
# that could disturb a desktop (outputs, compositors, input injection) runs
# in here, never on a personal machine.
#
#   tests/vm/vm.sh fetch        download and verify the cloud image
#   tests/vm/vm.sh init         ssh key, overlay disk, cloud-init seed
#   tests/vm/vm.sh start        boot (headless) and wait for ssh
#   tests/vm/vm.sh provision    install Hyprland, cage and friends in the guest
#   tests/vm/vm.sh ssh [cmd]    shell or command in the guest
#   tests/vm/vm.sh scp SRC DST  copy (DST may be guest:path)
#   tests/vm/vm.sh shot [file]  screenshot of the guest framebuffer (png)
#   tests/vm/vm.sh status       is it running, is ssh up
#   tests/vm/vm.sh stop         shut the VM down
#   tests/vm/vm.sh reset        drop the overlay disk (keeps the base image)
#
# OFFSTAGE_VM_PROFILE=name keeps a separate overlay disk, pid, serial log and
# host key per profile (e.g. "omarchy"), sharing the base image and the seed.
# Two profiles run at once only with distinct OFFSTAGE_VM_SSH_PORT values.
set -euo pipefail

VMDIR=${OFFSTAGE_VM_DIR:-$HOME/Work/offstage-vm}
PROFILE=${OFFSTAGE_VM_PROFILE:-default}
SUFFIX=; [ "$PROFILE" = default ] || SUFFIX=-$PROFILE
HERE=$(cd "$(dirname "$0")" && pwd)
MIRROR=${ARCH_MIRROR:-https://geo.mirror.pkgbuild.com/images/latest}
IMAGE=Arch-Linux-x86_64-cloudimg.qcow2
BASE=$VMDIR/base.qcow2
DISK=$VMDIR/disk$SUFFIX.qcow2
KEY=$VMDIR/id_ed25519
SEED=$VMDIR/seed
SSHPORT=${OFFSTAGE_VM_SSH_PORT:-2222}
HTTPPORT=${OFFSTAGE_VM_HTTP_PORT:-8123}
QMP=$VMDIR/qmp$SUFFIX.sock
PIDF=$VMDIR/qemu$SUFFIX.pid
HTTPPID=$VMDIR/http.pid
SERIAL=$VMDIR/serial$SUFFIX.log
KNOWN=$VMDIR/known_hosts$SUFFIX
MEM=${OFFSTAGE_VM_MEM:-4096}
CPUS=${OFFSTAGE_VM_CPUS:-4}
DISKSIZE=${OFFSTAGE_VM_DISK:-20G}
SSH_OPTS=(-i "$KEY" -p "$SSHPORT" -o StrictHostKeyChecking=no -o UserKnownHostsFile="$KNOWN" -o LogLevel=ERROR -o ConnectTimeout=5)

die() { echo "vm: $*" >&2; exit 1; }

running() { [ -f "$PIDF" ] && kill -0 "$(cat "$PIDF")" 2>/dev/null; }

qmp() { # qmp '{"execute":"..."}'
  python3 - "$QMP" "$1" <<'PY'
import json, socket, sys
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM); s.connect(sys.argv[1]); f = s.makefile("rw")
f.readline()  # greeting
for cmd in ('{"execute":"qmp_capabilities"}', sys.argv[2]):
    f.write(cmd + "\n"); f.flush()
    while True:
        line = f.readline()
        if not line: sys.exit(1)
        msg = json.loads(line)
        if "return" in msg or "error" in msg:
            if "error" in msg: print(json.dumps(msg), file=sys.stderr)
            break
PY
}

fetch() {
  mkdir -p "$VMDIR"
  curl -fsSL -o "$VMDIR/$IMAGE.SHA256" "$MIRROR/$IMAGE.SHA256"
  if [ ! -f "$BASE" ]; then
    curl -fL --progress-bar -o "$BASE.part" "$MIRROR/$IMAGE" && mv "$BASE.part" "$BASE"
  fi
  local want have
  want=$(awk '{print $1}' "$VMDIR/$IMAGE.SHA256"); have=$(sha256sum "$BASE" | awk '{print $1}')
  [ "$want" = "$have" ] || die "checksum mismatch for $BASE"
  echo "base image verified: $BASE"
}

init() {
  [ -f "$BASE" ] || die "run fetch first"
  mkdir -p "$SEED"
  [ -f "$KEY" ] || ssh-keygen -q -t ed25519 -N '' -C offstage-vm -f "$KEY"
  [ -f "$DISK" ] || qemu-img create -q -f qcow2 -b "$BASE" -F qcow2 "$DISK" "$DISKSIZE"
  printf 'instance-id: offstage-vm\nlocal-hostname: offstage-vm\n' > "$SEED/meta-data"
  sed "s|@PUBKEY@|$(cat "$KEY.pub")|" "$HERE/user-data.tmpl" > "$SEED/user-data"
  : > "$SEED/vendor-data"
  echo "initialised in $VMDIR"
}

start_http() {
  if [ -f "$HTTPPID" ] && kill -0 "$(cat "$HTTPPID")" 2>/dev/null; then return; fi
  (cd "$SEED" && nohup python3 -m http.server "$HTTPPORT" --bind 127.0.0.1 >"$VMDIR/http.log" 2>&1 & echo $! >"$HTTPPID")
}

start() {
  [ -f "$DISK" ] || die "run init first"
  running && { echo "already running (pid $(cat "$PIDF"))"; return; }
  start_http
  rm -f "$QMP"
  local gpu_args=(-device virtio-vga -display none)
  if [ "${OFFSTAGE_VM_GPU:-0}" = 1 ]; then
    # Hardware GL passed to the guest through virtio-gpu + virglrenderer,
    # rendered offscreen on the host GPU render node. No window on the host.
    gpu_args=(-device virtio-gpu-gl -display "egl-headless,rendernode=${OFFSTAGE_VM_RENDERNODE:-/dev/dri/renderD128}")
  fi
  qemu-system-x86_64 -name "offstage-vm$SUFFIX" -enable-kvm -cpu host -smp "$CPUS" -m "$MEM" \
    -drive "file=$DISK,if=virtio,format=qcow2" \
    "${gpu_args[@]}" \
    -device virtio-rng-pci \
    -nic "user,model=virtio-net-pci,hostfwd=tcp:127.0.0.1:$SSHPORT-:22" \
    -smbios "type=1,serial=ds=nocloud;s=http://10.0.2.2:$HTTPPORT/" \
    -qmp "unix:$QMP,server,nowait" -serial "file:$SERIAL" \
    -daemonize -pidfile "$PIDF"
  echo "booting (pid $(cat "$PIDF")), waiting for ssh on 127.0.0.1:$SSHPORT"
  wait_ssh
}

wait_ssh() {
  local i
  for i in $(seq 1 90); do
    if ssh "${SSH_OPTS[@]}" arch@127.0.0.1 true 2>/dev/null; then echo "ssh up"; return 0; fi
    sleep 2
  done
  die "ssh not reachable after 3 minutes; see $SERIAL"
}

vssh() { ssh "${SSH_OPTS[@]}" arch@127.0.0.1 "$@"; }

vscp() { # guest paths are written guest:/path
  local args=()
  for a in "$@"; do args+=("${a/guest:/arch@127.0.0.1:}"); done
  scp -q -i "$KEY" -P "$SSHPORT" -o StrictHostKeyChecking=no -o UserKnownHostsFile="$KNOWN" -o LogLevel=ERROR "${args[@]}"
}

provision() {
  vscp "$HERE"/guest/provision.sh "$HERE"/guest/omarchy-setup.sh "$HERE"/guest/hyprland.conf "$HERE"/guest/hyprland.lua "$HERE"/guest/hypr-start.sh "$HERE"/guest/hypr-stop.sh "$HERE"/guest/spikes3.sh "$HERE"/guest/multi.sh "$HERE"/guest/test.html "$HERE"/guest/keys.html guest:/home/arch/
  vssh chmod +x /home/arch/provision.sh /home/arch/omarchy-setup.sh /home/arch/hypr-start.sh /home/arch/hypr-stop.sh /home/arch/spikes3.sh /home/arch/multi.sh
  vssh sudo /home/arch/provision.sh
  # The omarchy profile adds the Omarchy package on top (same repository and
  # keyring as an ISO install; no ISO disk layout, bootloader or login manager).
  [ "$PROFILE" = omarchy ] && vssh sudo /home/arch/omarchy-setup.sh
  true
}

shot() {
  running || die "not running"
  local out=${1:-$VMDIR/shot.png}
  qmp "{\"execute\":\"screendump\",\"arguments\":{\"filename\":\"$VMDIR/shot.ppm\"}}"
  magick "$VMDIR/shot.ppm" "$out" 2>/dev/null || convert "$VMDIR/shot.ppm" "$out"
  echo "$out"
}

status() {
  if running; then echo "qemu: running (pid $(cat "$PIDF"))"; else echo "qemu: stopped"; fi
  if vssh true 2>/dev/null; then echo "ssh: up"; else echo "ssh: down"; fi
}

stop() {
  if running; then
    qmp '{"execute":"system_powerdown"}' || true
    local i; for i in $(seq 1 30); do running || break; sleep 1; done
    running && kill "$(cat "$PIDF")" 2>/dev/null || true
  fi
  rm -f "$PIDF"
  local other; for other in "$VMDIR"/qemu*.pid; do # keep the seed server for another profile
    [ -f "$other" ] && kill -0 "$(cat "$other")" 2>/dev/null && { echo "stopped"; return; }
  done
  [ -f "$HTTPPID" ] && { kill "$(cat "$HTTPPID")" 2>/dev/null || true; rm -f "$HTTPPID"; }
  echo "stopped"
}

reset() { running && die "stop it first"; rm -f "$DISK" "$KNOWN"; echo "overlay removed"; }

case ${1:-} in
  fetch) fetch ;;
  init) init ;;
  start) start ;;
  wait-ssh) wait_ssh ;;
  provision) provision ;;
  ssh) shift; vssh "$@" ;;
  scp) shift; vscp "$@" ;;
  shot) shift; shot "$@" ;;
  status) status ;;
  stop) stop ;;
  reset) reset ;;
  *) sed -n '2,17p' "$0"; exit 1 ;;
esac
