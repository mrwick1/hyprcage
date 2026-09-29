#!/usr/bin/env bash
# hyprcage installer: one command, everything in place.
#
#   curl -fsSL https://raw.githubusercontent.com/mrwick1/hyprcage/main/install.sh | bash
#
# Run from a checkout, it builds the binary from that source with go.
#
# What it does, in order, skipping what is already there:
#   1. cage (the agent's compositor), ffmpeg and wl-clipboard, through pacman
#      or zypper. sudo asks for your password once.
#   2. the hyprcage binary for this machine, from the GitHub release, checksum
#      verified against the SHA256SUMS published with it, into ~/.local/bin.
#   3. the agents it finds: the MCP server for Claude Code, and the MCP server
#      plus the skill for Codex, Cursor, Gemini CLI, Windsurf and OpenCode.
#   4. hyprcage doctor.
#
# Options and environment:
#   --agents LIST            which agents to register, comma-separated among
#                            claude, codex, gemini, cursor, windsurf, opencode,
#                            or none (default: every agent found on the machine)
#   --binary-only            step 2 only (what the plugin's launcher runs)
#   --uninstall              remove the binary, the registrations and hyprcage's state
#   HYPRCAGE_VERSION=vX.Y.Z  pin a release (default: the latest)
#   HYPRCAGE_FROM_SOURCE=1|0 build with go (1) or download (0); default 1 in a
#                            checkout, 0 elsewhere
#   HYPRCAGE_BIN_DIR=DIR     where the binary goes (default ~/.local/bin)
#   HYPRCAGE_RELEASE_BASE=URL  where the assets are fetched from (tests; default
#                            the GitHub release of the version)
set -euo pipefail

REPO=${HYPRCAGE_REPO:-mrwick1/hyprcage}
BIN_DIR=${HYPRCAGE_BIN_DIR:-$HOME/.local/bin}
AGENTS=${HYPRCAGE_AGENTS:-all}
# The checkout this script runs from. Piped from curl, BASH_SOURCE is empty
# and there is no checkout: the current directory is never taken for one.
SRC_DIR=""
if [ -n "${BASH_SOURCE[0]:-}" ] && [ -f "${BASH_SOURCE[0]}" ]; then
  SRC_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd || echo "")
fi
PACKAGES=(cage:cage ffmpeg:ffmpeg wl-clipboard:wl-copy)

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
  local p; for p in "${PACKAGES[@]}"; do have "${p#*:}" || echo "${p%%:*}"; done
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

# is_checkout reports whether SRC_DIR is a hyprcage source checkout.
is_checkout() { [ -f "$SRC_DIR/go.mod" ] && [ -f "$SRC_DIR/cmd/hyprcage/main.go" ]; }

build_from_source() {
  have go || return 1
  local src=$SRC_DIR
  if ! is_checkout; then
    src=$(mktemp -d); say "cloning $REPO"; git clone -q --depth 1 "https://github.com/$REPO" "$src" || return 1
  fi
  say "building from source in $src"
  (cd "$src" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/hexadecimil/hyprcage/internal/version.Version=$(git -C "$src" describe --tags --always --dirty 2>/dev/null || echo source)" -o "$BIN_DIR/hyprcage" ./cmd/hyprcage)
}

install_binary() {
  mkdir -p "$BIN_DIR"
  local from_source=0; is_checkout && from_source=1
  if [ "${HYPRCAGE_FROM_SOURCE:-$from_source}" = 1 ]; then build_from_source || die "build failed"; return; fi
  local version=${HYPRCAGE_VERSION:-} a tmp
  a=$(arch)
  if [ -z "$version" ]; then version=$(latest_version) || true; fi
  if [ -z "$version" ]; then
    warn "no release of $REPO found; building from source"
    build_from_source || die "no release of $REPO and no go toolchain to build it: install go, or HYPRCAGE_VERSION=vX.Y.Z"
    return
  fi
  VERSION=$version
  if [ -x "$BIN_DIR/hyprcage" ] && [ "$("$BIN_DIR/hyprcage" version 2>/dev/null)" = "$version" ]; then
    say "hyprcage $version already in $BIN_DIR"; return
  fi
  tmp=$(mktemp -d); trap 'rm -rf "$tmp"' RETURN
  local base=${HYPRCAGE_RELEASE_BASE:-"https://github.com/$REPO/releases/download/$version"}
  say "downloading hyprcage $version for linux/$a"
  if ! curl -fsSL -o "$tmp/hyprcage-linux-$a" "$base/hyprcage-linux-$a" || ! curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"; then
    warn "download failed"; build_from_source || die "no release for $version and no go toolchain to build from source"; return
  fi
  (cd "$tmp" && sha256sum -c --ignore-missing --quiet SHA256SUMS) || die "checksum mismatch for hyprcage-linux-$a: not installing"
  install -m 0755 "$tmp/hyprcage-linux-$a" "$BIN_DIR/hyprcage"
  say "installed $BIN_DIR/hyprcage ($("$BIN_DIR/hyprcage" version))"
}

# The configuration file, every key at its default, so that a setting is a
# line to edit rather than a key to discover. An existing file is kept.
write_config() {
  local out; out=$("$BIN_DIR/hyprcage" config 2>/dev/null) || return 0
  case $out in wrote*) say "$out" ;; esac
}

check_path() {
  case ":$PATH:" in *":$BIN_DIR:"*) return ;; esac
  warn "$BIN_DIR is not on your PATH; add it to your shell profile and to your graphical session (Hyprland's env), or the agents will not find hyprcage"
}

# --- 3. agents -------------------------------------------------------------------

# json_set FILE TOPKEY JSON: writes hyprcage's entry under TOPKEY in a JSON
# config file, creating or preserving the rest of it.
json_set() {
  have python3 || { warn "python3 missing: add hyprcage by hand to $1 (see the README)"; return 1; }
  python3 - "$1" "$2" "$3" <<'PY'
import json, os, sys
path, top, entry = sys.argv[1], sys.argv[2], json.loads(sys.argv[3])
data = {}
if os.path.exists(path):
    with open(path) as f:
        text = f.read().strip()
    if text:
        data = json.loads(text)
data.setdefault(top, {})["hyprcage"] = entry
os.makedirs(os.path.dirname(path), exist_ok=True)
with open(path, "w") as f:
    json.dump(data, f, indent=2); f.write("\n")
PY
}

# json_unset FILE TOPKEY: removes hyprcage's entry, leaving the rest.
json_unset() {
  [ -f "$1" ] && have python3 || return 0
  python3 - "$1" "$2" <<'PY'
import json, sys
path, top = sys.argv[1], sys.argv[2]
with open(path) as f:
    data = json.load(f)
if isinstance(data.get(top), dict) and data[top].pop("hyprcage", None) is not None:
    with open(path, "w") as f:
        json.dump(data, f, indent=2); f.write("\n")
PY
}

# install_skill DIR: copies the skill for an agent that has no plugin system.
install_skill() {
  local dst=$1/hyprcage src=$SRC_DIR/skills/hyprcage/SKILL.md
  mkdir -p "$dst"
  if [ -f "$src" ]; then
    cp -f "$src" "$dst/SKILL.md"
  else
    curl -fsSL -o "$dst/SKILL.md" "https://raw.githubusercontent.com/$REPO/${VERSION:-main}/skills/hyprcage/SKILL.md" || { warn "could not fetch the skill for $dst"; rmdir "$dst" 2>/dev/null; return; }
  fi
}

# Each agent gets the MCP server (the absolute path, so PATH does not matter)
# and, outside Claude Code, a copy of the skill. The safety timer closes the
# screens of a finished session.
# wanted NAME: is this agent selected by --agents (default: all found)?
wanted() { case ",$AGENTS," in *,all,* | *,"$1",*) return 0 ;; esac; return 1; }

register_agents() {
  local bin=$BIN_DIR/hyprcage found=0
  [ "$AGENTS" = none ] && { say "no agent registration asked (--agents none)"; return; }
  if wanted claude && have claude; then
    found=1
    if claude mcp get hyprcage >/dev/null 2>&1; then say "claude: already registered"
    elif claude mcp add --scope user hyprcage -- "$bin" mcp >/dev/null 2>&1; then say "claude: MCP server registered"
    else warn "claude: run  claude mcp add --scope user hyprcage -- $bin mcp"; fi
  elif wanted claude && [ -d "$HOME/.claude" ]; then found=1; say "Claude Code (no claude CLI in PATH): run  claude mcp add --scope user hyprcage -- $bin mcp"
  fi
  if wanted codex && have codex; then
    found=1
    if grep -qs '^\[mcp_servers\.hyprcage\]' "$HOME/.codex/config.toml"; then say "codex: already registered"
    elif codex mcp add hyprcage -- "$bin" mcp >/dev/null 2>&1; then say "codex: MCP server registered"
    else warn "codex: run  codex mcp add hyprcage -- $bin mcp"; fi
    install_skill "$HOME/.codex/skills"
  fi
  if wanted gemini && have gemini; then
    found=1
    if gemini mcp list 2>/dev/null | grep -q hyprcage; then say "gemini: already registered"
    elif gemini mcp add -s user hyprcage "$bin" mcp >/dev/null 2>&1; then say "gemini: MCP server registered"
    else warn "gemini: run  gemini mcp add -s user hyprcage $bin mcp"; fi
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

unregister_agents() {
  have claude && claude mcp remove --scope user hyprcage >/dev/null 2>&1
  have codex && codex mcp remove hyprcage >/dev/null 2>&1
  have gemini && gemini mcp remove -s user hyprcage >/dev/null 2>&1
  json_unset "$HOME/.cursor/mcp.json" mcpServers
  json_unset "$HOME/.codeium/windsurf/mcp_config.json" mcpServers
  json_unset "$HOME/.config/opencode/opencode.json" mcp
  local d; for d in .codex/skills .cursor/skills .codeium/windsurf/skills .config/opencode/skills; do rm -rf "$HOME/$d/hyprcage"; done
  return 0
}

# --- uninstall ---------------------------------------------------------------------

uninstall() {
  say "removing hyprcage from the agents, the binary and hyprcage's state (the packages stay)"
  unregister_agents
  if [ -x "$BIN_DIR/hyprcage" ]; then "$BIN_DIR/hyprcage" gc --all >/dev/null 2>&1 || true; fi
  rm -f "$BIN_DIR/hyprcage"
  rm -rf "${XDG_STATE_HOME:-$HOME/.local/state}/hyprcage" "${XDG_CONFIG_HOME:-$HOME/.config}/hyprcage"
  say "done. cage, ffmpeg and wl-clipboard stay installed; remove them with your package manager"
}

# --- main ------------------------------------------------------------------------------

mode=install
while [ $# -gt 0 ]; do
  case $1 in
    --binary-only) mode=binary ;;
    --uninstall) mode=uninstall ;;
    --agents) shift; AGENTS=${1:-}; [ -n "$AGENTS" ] || die "--agents needs a list" ;;
    --agents=*) AGENTS=${1#--agents=} ;;
    *) die "usage: install.sh [--agents LIST] [--binary-only | --uninstall]" ;;
  esac
  shift
done
case $mode in
  binary) install_binary; write_config ;;
  uninstall) uninstall ;;
  install)
    [ "$(uname -s)" = Linux ] || die "hyprcage runs on Linux with Hyprland"
    have Hyprland || warn "Hyprland not found in PATH; hyprcage needs a running Hyprland to do anything"
    install_packages
    install_binary
    write_config
    check_path
    register_agents
    say "checking the installation"
    "$BIN_DIR/hyprcage" doctor || true
    ;;
esac
