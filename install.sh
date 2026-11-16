#!/usr/bin/env bash
# offstage installer: one command, everything in place.
#
#   curl -fsSL https://raw.githubusercontent.com/mrwick1/offstage/main/install.sh | bash
#
# Run from a checkout, it builds the binary from that source with go.
#
# What it does, in order, skipping what is already there:
#   1. cage (the agent's compositor), ffmpeg, wl-clipboard, tesseract and its
#      English data (OCR snapshots), and at-spi2-core (the a11y bus that
#      snapshots of GTK and Qt apps read), through pacman or zypper. sudo asks
#      for your password once.
#   2. the offstage binary for this machine, from the GitHub release, checksum
#      verified against the SHA256SUMS published with it, into ~/.local/bin.
#   3. the agents it finds: the MCP server for Claude Code, and the MCP server
#      plus the skill for Codex, Cursor, Gemini CLI, Windsurf and OpenCode.
#      bare, the e2e browser, comes with it: bare-mcp and bare-drive from the
#      bare release, checksum verified, into the same directory. A bare
#      download that fails is a warning: offstage still installs. The MCP
#      server "bare" goes to each agent beside offstage.
#   4. the user unit offstage-notifyd.service, which keeps 48 hours of
#      desktop notifications for the notify tools.
#   5. offstage doctor.
#
# Options and environment:
#   --agents LIST            which agents to register, comma-separated among
#                            claude, codex, gemini, cursor, windsurf, opencode,
#                            or none (default: every agent found on the machine)
#   --binary-only            step 2 only, bare included (what the plugin's launcher runs)
#   --bare-only              bare only (what the plugin's bare launcher runs)
#   --uninstall              remove the binary, the registrations and offstage's state
#   OFFSTAGE_VERSION=vX.Y.Z  pin a release (default: the latest)
#   OFFSTAGE_FROM_SOURCE=1|0 build with go (1) or download (0); default 1 in a
#                            checkout, 0 elsewhere
#   OFFSTAGE_BIN_DIR=DIR     where the binary goes (default ~/.local/bin)
#   OFFSTAGE_RELEASE_BASE=URL  where the assets are fetched from (tests; default
#                            the GitHub release of the version)
#   OFFSTAGE_BARE_VERSION=vX.Y.Z  pin the bare release (default below)
#   OFFSTAGE_NO_BARE=1       do not install or register bare
#   OFFSTAGE_BARE_RELEASE_BASE=URL  where the bare assets are fetched from (tests)
set -euo pipefail

REPO=${OFFSTAGE_REPO:-mrwick1/offstage}
BIN_DIR=${OFFSTAGE_BIN_DIR:-$HOME/.local/bin}
AGENTS=${OFFSTAGE_AGENTS:-all}
# The bare release that this offstage ships with. Keep it equal to the default
# in install.ps1.
BARE_VERSION=${OFFSTAGE_BARE_VERSION:-v0.1.1}
BARE_REPO=mrwick1/bare
# Its file marks a bare that this installer put in BIN_DIR. Uninstall removes
# only that one.
BARE_MARK_DIR=${XDG_STATE_HOME:-$HOME/.local/state}/offstage
# The checkout this script runs from. Piped from curl, BASH_SOURCE is empty
# and there is no checkout: the current directory is never taken for one.
SRC_DIR=""
if [ -n "${BASH_SOURCE[0]:-}" ] && [ -f "${BASH_SOURCE[0]}" ]; then
  SRC_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd || echo "")
fi
# package:proof. The proof is a binary in PATH, or a file when it starts with /.
PACKAGES=(cage:cage ffmpeg:ffmpeg wl-clipboard:wl-copy tesseract:tesseract
  tesseract-data-eng:/usr/share/tessdata/eng.traineddata
  at-spi2-core:/usr/share/dbus-1/services/org.a11y.Bus.service
  libxkbcommon:ldconfig:libxkbcommon.so.0)

say() { printf '\033[1;36m==>\033[0m %s\n' "$*" >&2; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

arch() {
  case $(uname -m) in
    x86_64) echo amd64 ;;
    aarch64 | arm64) echo arm64 ;;
    *) die "unsupported architecture $(uname -m) (releases cover x86_64 and aarch64)" ;;
  esac
}

# --- 1. packages -------------------------------------------------------------

missing_packages() {
  local p c; for p in "${PACKAGES[@]}"; do
    c=${p#*:}  # the proof may hold colons: ldconfig:LIB
    case $c in
      ldconfig:*) PATH=$PATH:/sbin:/usr/sbin ldconfig -p 2>/dev/null | grep -q "${c#ldconfig:}" || echo "${p%%:*}" ;;
      /*) [ -e "$c" ] || echo "${p%%:*}" ;;
      *) have "$c" || echo "${p%%:*}" ;;
    esac
  done
}

# zypper_name PACKAGE: the openSUSE name of a pacman package.
zypper_name() {
  case $1 in
    tesseract) echo tesseract-ocr ;;
    tesseract-data-eng) echo tesseract-ocr-traineddata-english ;;
    libxkbcommon) echo libxkbcommon0 ;;
    *) echo "$1" ;;
  esac
}

pm_install() {
  if have pacman; then echo "pacman -S --needed --noconfirm"
  elif have zypper; then echo "zypper --non-interactive install --no-recommends"
  else return 1; fi
}

install_packages() {
  local missing pm; mapfile -t missing < <(missing_packages)
  if [ ${#missing[@]} -eq 0 ]; then say "packages already installed"; return; fi
  pm=$(pm_install) || die "no pacman or zypper: install ${missing[*]} with your package manager, then rerun"
  if [ "${pm%% *}" = zypper ]; then
    local i; for i in "${!missing[@]}"; do missing[i]=$(zypper_name "${missing[i]}"); done
  fi
  say "installing ${missing[*]} (sudo will ask for your password)"
  if sudo -n true 2>/dev/null || [ -t 0 ]; then
    sudo $pm "${missing[@]}" || die "package install failed"
  elif have pkexec; then
    pkexec $pm "${missing[@]}" || die "package install failed"
  else
    die "no terminal for sudo and no pkexec: run  sudo $pm ${missing[*]}  then rerun"
  fi
}

# --- 2. binary -------------------------------------------------------------------

latest_version() {
  # The release page of "latest" redirects to the tagged one. A repository
  # without a release redirects to its release list instead: no version.
  local url
  url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest") || return 1
  case $url in */tag/*) echo "${url##*/tag/}" ;; *) return 1 ;; esac
}

# is_checkout reports whether SRC_DIR is a offstage source checkout.
is_checkout() { [ -f "$SRC_DIR/go.mod" ] && [ -f "$SRC_DIR/cmd/offstage/main.go" ]; }

build_from_source() {
  have go || return 1
  local src=$SRC_DIR
  if ! is_checkout; then
    src=$(mktemp -d); say "cloning $REPO"; git clone -q --depth 1 "https://github.com/$REPO" "$src" || return 1
  fi
  say "building from source in $src"
  (cd "$src" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/hexadecimil/offstage/internal/version.Version=$(git -C "$src" describe --tags --always --dirty 2>/dev/null || echo source)" -o "$BIN_DIR/offstage" ./cmd/offstage)
}

install_binary() {
  mkdir -p "$BIN_DIR"
  local from_source=0; is_checkout && from_source=1
  if [ "${OFFSTAGE_FROM_SOURCE:-$from_source}" = 1 ]; then build_from_source || die "build failed"; return; fi
  local version=${OFFSTAGE_VERSION:-} a tmp
  a=$(arch)
  if [ -z "$version" ]; then version=$(latest_version) || true; fi
  if [ -z "$version" ]; then
    warn "no release of $REPO found; building from source"
    build_from_source || die "no release of $REPO and no go toolchain to build it: install go, or OFFSTAGE_VERSION=vX.Y.Z"
    return
  fi
  VERSION=$version
  if [ -x "$BIN_DIR/offstage" ] && [ "$("$BIN_DIR/offstage" version 2>/dev/null)" = "$version" ]; then
    say "offstage $version already in $BIN_DIR"; return
  fi
  tmp=$(mktemp -d); trap 'rm -rf "$tmp"' RETURN
  local base=${OFFSTAGE_RELEASE_BASE:-"https://github.com/$REPO/releases/download/$version"}
  say "downloading offstage $version for linux/$a"
  if ! curl -fsSL -o "$tmp/offstage-linux-$a" "$base/offstage-linux-$a" || ! curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"; then
    warn "download failed"; build_from_source || die "no release for $version and no go toolchain to build from source"; return
  fi
  (cd "$tmp" && sha256sum -c --ignore-missing --quiet SHA256SUMS) || die "checksum mismatch for offstage-linux-$a: not installing"
  install -m 0755 "$tmp/offstage-linux-$a" "$BIN_DIR/offstage"
  say "installed $BIN_DIR/offstage ($("$BIN_DIR/offstage" version))"
}

# _install_bare TMPDIR: downloads, verifies and installs bare-mcp and bare-drive.
# Every failure returns 1; install_bare turns it into a warning.
_install_bare() {
  local tmp=$1 a base m d want
  a=$(arch) || return 1
  m=bare-mcp-linux-$a; d=bare-drive-linux-$a
  base=${OFFSTAGE_BARE_RELEASE_BASE:-"https://github.com/$BARE_REPO/releases/download/$BARE_VERSION"}
  curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" || return 1
  want=$(awk -v n="$m" '$2 == n || $2 == "*" n { print $1 }' "$tmp/SHA256SUMS")
  if [ -n "$want" ] && [ -x "$BIN_DIR/bare-drive" ] && [ -x "$BIN_DIR/bare-mcp" ] \
    && [ "$(sha256sum "$BIN_DIR/bare-mcp" | cut -d' ' -f1)" = "$want" ]; then
    say "bare $BARE_VERSION already in $BIN_DIR"; return 0
  fi
  say "downloading bare $BARE_VERSION for linux/$a"
  curl -fsSL -o "$tmp/$m" "$base/$m" || return 1
  curl -fsSL -o "$tmp/$d" "$base/$d" || return 1
  # --ignore-missing passes when no line names a file, so check both are named.
  grep -qE " \*?$m\$" "$tmp/SHA256SUMS" && grep -qE " \*?$d\$" "$tmp/SHA256SUMS" || { warn "no checksum for $m or $d in SHA256SUMS"; return 1; }
  (cd "$tmp" && sha256sum -c --ignore-missing --quiet SHA256SUMS) || { warn "checksum mismatch for the bare assets: not installing"; return 1; }
  mkdir -p "$BIN_DIR"
  install -m 0755 "$tmp/$m" "$BIN_DIR/bare-mcp" || return 1
  install -m 0755 "$tmp/$d" "$BIN_DIR/bare-drive" || return 1
  mkdir -p "$BARE_MARK_DIR" && : >"$BARE_MARK_DIR/bare-installed"
  say "installed bare-mcp and bare-drive in $BIN_DIR"
}

# install_bare: never fatal. offstage works without bare.
install_bare() {
  [ "${OFFSTAGE_NO_BARE:-0}" = 1 ] && { say "bare skipped (OFFSTAGE_NO_BARE=1)"; return 0; }
  local tmp; tmp=$(mktemp -d)
  _install_bare "$tmp" || warn "could not install bare $BARE_VERSION; offstage works without it. Rerun the installer later"
  rm -rf "$tmp"
}

# The configuration file, every key at its default, so that a setting is a
# line to edit rather than a key to discover. An existing file is kept.
write_config() {
  local out; out=$("$BIN_DIR/offstage" config 2>/dev/null) || return 0
  case $out in wrote*) say "$out" ;; esac
}

check_path() {
  case ":$PATH:" in *":$BIN_DIR:"*) return ;; esac
  warn "$BIN_DIR is not on your PATH; add it to your shell profile and to your graphical session (Hyprland's env), or the agents will not find offstage"
}

# --- 3. agents -------------------------------------------------------------------

# json_set FILE TOPKEY JSON [NAME [keep]]: writes the entry NAME (default
# offstage) under TOPKEY in a JSON config file, creating or preserving the
# rest of it. With keep, an existing entry stays and the status is 3.
json_set() {
  have python3 || { warn "python3 missing: add offstage by hand to $1 (see the README)"; return 1; }
  python3 - "$1" "$2" "$3" "${4:-offstage}" "${5:-}" <<'PY'
import json, os, sys
path, top, entry, name, keep = sys.argv[1], sys.argv[2], json.loads(sys.argv[3]), sys.argv[4], sys.argv[5]
data = {}
if os.path.exists(path):
    with open(path) as f:
        text = f.read().strip()
    if text:
        data = json.loads(text)
if keep and name in data.get(top, {}):
    sys.exit(3)
data.setdefault(top, {})[name] = entry
os.makedirs(os.path.dirname(path), exist_ok=True)
with open(path, "w") as f:
    json.dump(data, f, indent=2); f.write("\n")
PY
}

# json_unset FILE TOPKEY [NAME]: removes the entry NAME (default offstage),
# leaving the rest.
json_unset() {
  [ -f "$1" ] && have python3 || return 0
  python3 - "$1" "$2" "${3:-offstage}" <<'PY'
import json, sys
path, top, name = sys.argv[1], sys.argv[2], sys.argv[3]
with open(path) as f:
    data = json.load(f)
if isinstance(data.get(top), dict) and data[top].pop(name, None) is not None:
    with open(path, "w") as f:
        json.dump(data, f, indent=2); f.write("\n")
PY
}

# install_skill DIR: copies the skill for an agent that has no plugin system.
install_skill() {
  local dst=$1/offstage src=$SRC_DIR/skills/offstage/SKILL.md
  mkdir -p "$dst"
  if [ -f "$src" ]; then
    cp -f "$src" "$dst/SKILL.md"
  else
    curl -fsSL -o "$dst/SKILL.md" "https://raw.githubusercontent.com/$REPO/${VERSION:-main}/skills/offstage/SKILL.md" || { warn "could not fetch the skill for $dst"; rmdir "$dst" 2>/dev/null; return; }
  fi
}

# Each agent gets the MCP server (the absolute path, so PATH does not matter)
# and, outside Claude Code, a copy of the skill. The safety timer closes the
# screens of a finished session.
# wanted NAME: is this agent selected by --agents (default: all found)?
wanted() { case ",$AGENTS," in *,all,* | *,"$1",*) return 0 ;; esac; return 1; }

register_agents() {
  local bin=$BIN_DIR/offstage found=0
  [ "$AGENTS" = none ] && { say "no agent registration asked (--agents none)"; return; }
  if wanted claude && have claude; then
    found=1
    if claude mcp get offstage >/dev/null 2>&1; then say "claude: already registered"
    elif claude mcp add --scope user offstage -- "$bin" mcp >/dev/null 2>&1; then say "claude: MCP server registered"
    else warn "claude: run  claude mcp add --scope user offstage -- $bin mcp"; fi
  elif wanted claude && [ -d "$HOME/.claude" ]; then found=1; say "Claude Code (no claude CLI in PATH): run  claude mcp add --scope user offstage -- $bin mcp"
  fi
  if wanted codex && have codex; then
    found=1
    if grep -qs '^\[mcp_servers\.offstage\]' "$HOME/.codex/config.toml"; then say "codex: already registered"
    elif codex mcp add offstage -- "$bin" mcp >/dev/null 2>&1; then say "codex: MCP server registered"
    else warn "codex: run  codex mcp add offstage -- $bin mcp"; fi
    install_skill "$HOME/.codex/skills"
  fi
  if wanted gemini && have gemini; then
    found=1
    if gemini mcp list 2>/dev/null | grep -q offstage; then say "gemini: already registered"
    elif gemini mcp add -s user offstage "$bin" mcp >/dev/null 2>&1; then say "gemini: MCP server registered"
    else warn "gemini: run  gemini mcp add -s user offstage $bin mcp"; fi
  fi
  if wanted cursor && [ -d "$HOME/.cursor" ]; then
    found=1; json_set "$HOME/.cursor/mcp.json" mcpServers "{\"command\":\"$bin\",\"args\":[\"mcp\"]}" && say "cursor: MCP server registered"
    install_skill "$HOME/.cursor/skills"
  fi
  if wanted windsurf && [ -d "$HOME/.codeium/windsurf" ]; then
    found=1; json_set "$HOME/.codeium/windsurf/mcp_config.json" mcpServers "{\"command\":\"$bin\",\"args\":[\"mcp\"]}" && say "windsurf: MCP server registered"
    install_skill "$HOME/.codeium/windsurf/skills"
  fi
  if wanted opencode && { have opencode || [ -d "$HOME/.config/opencode" ]; }; then
    found=1; json_set "$HOME/.config/opencode/opencode.json" mcp "{\"type\":\"local\",\"command\":[\"$bin\",\"mcp\"],\"enabled\":true}" && say "opencode: MCP server registered"
    install_skill "$HOME/.config/opencode/skills"
  fi
  [ $found = 1 ] || say "no selected agent found; any MCP client can run  $bin mcp  (see the README)"
}

# json_bare FILE TOPKEY JSON LABEL: adds the bare entry unless one exists.
json_bare() {
  local rc=0; json_set "$1" "$2" "$3" bare keep || rc=$?
  case $rc in 0) say "$4: bare registered" ;; 3) say "$4: bare already registered, left alone" ;; esac
}

# register_bare: the MCP server bare, for each selected agent, with the env
# that sends bare's problem reports through offstage's report_issue. A bare
# that is already registered stays as it is.
register_bare() {
  local bin=$BIN_DIR/bare-mcp
  [ "${OFFSTAGE_NO_BARE:-0}" = 1 ] && return 0
  [ "$AGENTS" = none ] && return 0
  [ -x "$bin" ] || { say "bare is not installed: bare registration skipped"; return 0; }
  local env=BARE_MCP_REPORT=offstage
  if wanted claude && have claude; then
    if claude mcp get bare >/dev/null 2>&1; then say "claude: bare already registered, left alone"
    elif claude mcp add --scope user bare -e "$env" -- "$bin" >/dev/null 2>&1; then say "claude: bare registered"
    else warn "claude: run  claude mcp add --scope user bare -e $env -- $bin"; fi
  fi
  if wanted codex && have codex; then
    if grep -qs '^\[mcp_servers\.bare\]' "$HOME/.codex/config.toml"; then say "codex: bare already registered, left alone"
    elif codex mcp add --env "$env" bare -- "$bin" >/dev/null 2>&1; then say "codex: bare registered"
    else warn "codex: run  codex mcp add --env $env bare -- $bin"; fi
  fi
  if wanted gemini && have gemini; then
    if gemini mcp list 2>/dev/null | grep -qw bare; then say "gemini: bare already registered, left alone"
    elif gemini mcp add -s user bare "$bin" -e "$env" >/dev/null 2>&1; then say "gemini: bare registered"
    else warn "gemini: run  gemini mcp add -s user bare $bin -e $env"; fi
  fi
  local e="{\"command\":\"$bin\",\"env\":{\"BARE_MCP_REPORT\":\"offstage\"}}"
  if wanted cursor && [ -d "$HOME/.cursor" ]; then json_bare "$HOME/.cursor/mcp.json" mcpServers "$e" cursor; fi
  if wanted windsurf && [ -d "$HOME/.codeium/windsurf" ]; then json_bare "$HOME/.codeium/windsurf/mcp_config.json" mcpServers "$e" windsurf; fi
  if wanted opencode && { have opencode || [ -d "$HOME/.config/opencode" ]; }; then
    json_bare "$HOME/.config/opencode/opencode.json" mcp "{\"type\":\"local\",\"command\":[\"$bin\"],\"environment\":{\"BARE_MCP_REPORT\":\"offstage\"},\"enabled\":true}" opencode
  fi
}

# unregister_agents [NAME]: removes the registrations named NAME (default offstage).
unregister_agents() {
  local name=${1:-offstage}
  # Each remove fails when the name is not registered. Under set -e, a failed
  # last command of an && list ends the script, so each one ends in || true.
  have claude && claude mcp remove --scope user "$name" >/dev/null 2>&1 || true
  have codex && codex mcp remove "$name" >/dev/null 2>&1 || true
  have gemini && gemini mcp remove -s user "$name" >/dev/null 2>&1 || true
  json_unset "$HOME/.cursor/mcp.json" mcpServers "$name"
  json_unset "$HOME/.codeium/windsurf/mcp_config.json" mcpServers "$name"
  json_unset "$HOME/.config/opencode/opencode.json" mcp "$name"
  local d; for d in .codex/skills .cursor/skills .codeium/windsurf/skills .config/opencode/skills; do [ "$name" = bare ] || rm -rf "${HOME:?}/$d/$name"; done
  return 0
}

# drop_old_name: offstage was called hyprcage before 0.10. Removes the old
# binary, its registrations and its notification unit. The binary moves the
# old config, state and data on its first run.
drop_old_name() {
  [ -e "$BIN_DIR/hyprcage" ] || [ -f "$UNIT_DIR/hyprcage-notifyd.service" ] || return 0
  say "removing the old name hyprcage (settings, logs and browser profiles move to offstage)"
  systemctl --user disable --now hyprcage-notifyd.service >/dev/null 2>&1 || true
  rm -f "$UNIT_DIR/hyprcage-notifyd.service"
  if [ -x "$BIN_DIR/hyprcage" ]; then "$BIN_DIR/hyprcage" gc --all >/dev/null 2>&1 || true; fi
  rm -f "$BIN_DIR/hyprcage"
  unregister_agents hyprcage
}

# --- 4. notification daemon ---------------------------------------------------------

UNIT_DIR=$HOME/.config/systemd/user

install_notifyd() {
  local src=$SRC_DIR/contrib/offstage-notifyd.service unit exe
  if [ -f "$src" ]; then
    unit=$(cat "$src")
  else
    unit=$(curl -fsSL "https://raw.githubusercontent.com/$REPO/${VERSION:-main}/contrib/offstage-notifyd.service") || { warn "could not fetch offstage-notifyd.service"; return; }
  fi
  # ExecStart runs the binary installed above, quoted for systemd.
  exe=$(cd "$BIN_DIR" && pwd -P)/offstage
  exe=${exe//\\/\\\\}; exe=${exe//\"/\\\"}; exe=${exe//%/%%}
  mkdir -p "$UNIT_DIR"
  printf '%s\n' "${unit/"%h/.local/bin/offstage"/"\"$exe\""}" >"$UNIT_DIR/offstage-notifyd.service"
  systemctl --user daemon-reload && systemctl --user enable --now offstage-notifyd.service >/dev/null 2>&1 \
    && say "offstage-notifyd.service enabled" || warn "could not enable offstage-notifyd.service; run  systemctl --user enable --now offstage-notifyd.service"
}

# --- uninstall ---------------------------------------------------------------------

uninstall() {
  say "removing offstage from the agents, the binary and offstage's state (the packages stay)"
  unregister_agents
  # Only when this installer put bare there: a bare of the user's own stays.
  if [ -e "$BARE_MARK_DIR/bare-installed" ]; then
    unregister_agents bare
    rm -f "$BIN_DIR/bare-mcp" "$BIN_DIR/bare-drive"
  fi
  systemctl --user disable --now offstage-notifyd.service >/dev/null 2>&1 || true
  rm -f "$UNIT_DIR/offstage-notifyd.service"
  if [ -x "$BIN_DIR/offstage" ]; then "$BIN_DIR/offstage" gc --all >/dev/null 2>&1 || true; fi
  rm -f "$BIN_DIR/offstage"
  rm -rf "${XDG_STATE_HOME:-$HOME/.local/state}/offstage" "${XDG_CONFIG_HOME:-$HOME/.config}/offstage"
  say "done. cage, ffmpeg, wl-clipboard, tesseract, its English data and at-spi2-core stay installed; remove them with your package manager"
}

# --- main ------------------------------------------------------------------------------

mode=install
while [ $# -gt 0 ]; do
  case $1 in
    --binary-only) mode=binary ;;
    --bare-only) mode=bare ;;
    --uninstall) mode=uninstall ;;
    --agents) shift; AGENTS=${1:-}; [ -n "$AGENTS" ] || die "--agents needs a list" ;;
    --agents=*) AGENTS=${1#--agents=} ;;
    *) die "usage: install.sh [--agents LIST] [--binary-only | --bare-only | --uninstall]" ;;
  esac
  shift
done
case $mode in
  binary) install_binary; install_bare; write_config ;;
  bare) install_bare ;;
  uninstall) uninstall ;;
  install)
    [ "$(uname -s)" = Linux ] || die "offstage runs on Linux with Hyprland"
    have Hyprland || warn "Hyprland not found in PATH; offstage needs a running Hyprland to do anything"
    install_packages
    drop_old_name
    install_binary
    install_bare
    write_config
    check_path
    register_agents
    register_bare
    install_notifyd
    say "checking the installation"
    "$BIN_DIR/offstage" doctor || true
    [ -s "${XDG_CONFIG_HOME:-$HOME/.config}/offstage/beta-key" ] || say "beta tester? store your beta key once:  $BIN_DIR/offstage report login"
    ;;
esac
