#!/usr/bin/env bash
# Required browser downloads must fail installation. Use local assets only.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/release" "$tmp/home"
if env -i PATH=/usr/bin:/bin HOME="$tmp/home" OFFSTAGE_BARE_RELEASE_BASE="file://$tmp/release" bash "$here/install.sh" --bare-only >"$tmp/out" 2>&1; then
  echo 'FAIL installation succeeded without Bare'; cat "$tmp/out"; exit 1
fi
if env -i PATH=/usr/bin:/bin HOME="$tmp/home" OFFSTAGE_NO_BARE=1 bash "$here/install.sh" --bare-only >"$tmp/out" 2>&1; then
  echo 'FAIL installation allowed Bare to be skipped'; exit 1
fi
echo 'PASS Bare is required'
case $(uname -m) in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; *) exit 0 ;; esac
for name in bare-mcp bare-drive; do
  printf '#!/bin/sh\nexit 0\n' > "$tmp/release/$name-linux-$arch"
done
(cd "$tmp/release" && sha256sum "bare-mcp-linux-$arch" "bare-drive-linux-$arch" > SHA256SUMS)
install_bare() {
  env -i PATH=/usr/bin:/bin HOME="$tmp/home" OFFSTAGE_BARE_RELEASE_BASE="file://$tmp/release" bash "$here/install.sh" --bare-only >"$tmp/out" 2>&1
}
install_bare || { cat "$tmp/out"; exit 1; }
[ -x "$tmp/home/.local/bin/bare-mcp" ] && [ -x "$tmp/home/.local/bin/bare-drive" ]
printf '#!/bin/sh\nexit 99\n' > "$tmp/home/.local/bin/bare-drive"
install_bare || { cat "$tmp/out"; exit 1; }
cmp "$tmp/home/.local/bin/bare-drive" "$tmp/release/bare-drive-linux-$arch"
echo 'PASS installation repairs a mismatched driver'
printf 'corrupted' > "$tmp/release/bare-drive-linux-$arch"
rm "$tmp/home/.local/bin/bare-drive"
if install_bare; then echo 'FAIL installation accepted a checksum mismatch'; exit 1; fi
echo 'PASS invalid browser assets fail installation'
