#!/usr/bin/env bash
# The launcher's outdated check must order release tags by semver: a release
# ranks above its prereleases, and numeric identifiers compare as numbers.
# Runs the function with stub binaries; it changes nothing on the machine.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
. "$here/bin/offstage-mcp"

# check HAVE WANT yes|no: is the installed HAVE outdated against WANT?
fail=0
check() {
  want=$2
  printf '#!/bin/sh\necho %s\n' "$1" > "$tmp/offstage"; chmod +x "$tmp/offstage"
  got=no; outdated "$tmp/offstage" && got=yes
  if [ "$got" = "$3" ]; then echo "PASS $1 outdated against $2: $got"
  else echo "FAIL $1 against $2: outdated=$got, want $3"; fail=1; fi
}
check v0.11.0-beta.8 v0.11.0 yes
check v0.11.0-beta.9 v0.11.0-beta.10 yes
check v0.11.0-beta.10 v0.11.0-beta.9 no
check v0.10.9 v0.11.0 yes
check v0.11.0 v0.11.1-beta.1 yes
check v0.11.0 v0.11.0 no
check v0.11.0 v0.11.0-beta.8 no
check v0.11.1 v0.11.0 no
check v0.2.0 v0.10.0 yes
check v0.10.0 v0.2.0 no
check source v0.11.0 yes
check v0.11.0-g1a2b3c v0.11.0 no
[ "$fail" = 0 ] && echo "PASS all launcher version checks"
exit "$fail"
