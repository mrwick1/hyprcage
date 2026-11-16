#!/usr/bin/env bash
# Installer checks, run INSIDE the VM as user arch: install.sh end to end
# against a release base (the seed server of vm.sh serves $VMDIR/seed/rel),
# the refusal of a tampered checksum, the plugin launcher's first-run
# bootstrap, `offstage setup`, and uninstall. Expects the repository's
# install.sh, bin/ and .claude-plugin/ under ~/repo (tar over vm.sh ssh).
#   OFFSTAGE_RELEASE_BASE=http://10.0.2.2:8123/rel OFFSTAGE_VERSION=test ./install-test.sh
set -uo pipefail
FAILED=0; pass() { echo "PASS $*"; }; fail() { echo "FAIL $*"; FAILED=1; }
R=$HOME/repo
export PATH=$HOME/.local/bin:$PATH XDG_RUNTIME_DIR=/run/user/$(id -u)
# Without a local base, the assets come from the GitHub release of OFFSTAGE_VERSION.
: "${OFFSTAGE_VERSION:?set OFFSTAGE_VERSION (vX.Y.Z, or test with OFFSTAGE_RELEASE_BASE)}"
export OFFSTAGE_RELEASE_BASE=${OFFSTAGE_RELEASE_BASE:-https://github.com/mrwick1/offstage/releases/download/$OFFSTAGE_VERSION}
pgrep -x Hyprland >/dev/null || { HYPR_CONFIG=classic ./hypr-start.sh >/dev/null 2>&1; sleep 3; }
export HYPRLAND_INSTANCE_SIGNATURE=$(ls "$XDG_RUNTIME_DIR/hypr" | head -1)
sudo pacman -R --noconfirm cage >/dev/null 2>&1; rm -f ~/.local/bin/offstage
command -v cage >/dev/null && fail "precondition: cage still installed"
# Fake agents: codex and gemini as scripts that log their arguments, config
# directories for cursor, windsurf and opencode.
mkdir -p ~/.local/bin ~/.cursor ~/.codeium/windsurf ~/.config/opencode ~/.codex ~/.claude; rm -f ~/fake-agents.log
printf '#!/bin/sh\necho "codex $*" >> "$HOME/fake-agents.log"\ncase "$*" in "mcp add"*) printf "[mcp_servers.offstage]\\n" >> "$HOME/.codex/config.toml";; "mcp remove"*) : > "$HOME/.codex/config.toml";; esac\n' > ~/.local/bin/codex
printf '#!/bin/sh\necho "gemini $*" >> "$HOME/fake-agents.log"\n' > ~/.local/bin/gemini
chmod +x ~/.local/bin/codex ~/.local/bin/gemini; : > ~/.codex/config.toml
echo '{"mcpServers":{"other":{"command":"x"}}}' > ~/.cursor/mcp.json

echo "== install.sh end to end (packages by sudo -n, binary from the release base)"
bash "$R/install.sh" > ~/install.log 2>&1 && pass "install.sh exits 0" || { fail "install.sh failed"; tail -5 ~/install.log; }
command -v cage >/dev/null && pass "cage installed" || fail "cage not installed"

[ -x ~/.local/bin/offstage ] && pass "binary in ~/.local/bin ($(offstage version))" || fail "binary missing"
grep -q 'plugin marketplace add' ~/install.log && pass "Claude Code: plugin commands printed (~/.claude present, no claude CLI)" || fail "plugin step: $(grep -i plugin ~/install.log | head -2)"
grep -q '^offstage  *ok' ~/install.log && pass "doctor ran at the end" || fail "doctor did not run: $(tail -3 ~/install.log)"
[ -s ~/.config/offstage/config.toml ] && grep -q '^group = "session"' ~/.config/offstage/config.toml && pass "config written with the defaults" || fail "config: $(head -3 ~/.config/offstage/config.toml 2>&1)"
grep -q 'wrote .*config.toml' ~/install.log && pass "install.sh said where the config is" || fail "install.sh silent about the config"
sed -i 's/^width = 1280/width = 1024/' ~/.config/offstage/config.toml

echo "== agents registered"
bin=$HOME/.local/bin/offstage
grep -q "codex mcp add offstage -- $bin mcp" ~/fake-agents.log && pass "codex: mcp add called" || fail "codex: $(grep codex ~/fake-agents.log)"
grep -q "gemini mcp add -s user offstage $bin mcp" ~/fake-agents.log && pass "gemini: mcp add called" || fail "gemini: $(grep gemini ~/fake-agents.log)"
[ "$(jq -r '.mcpServers.offstage.command' ~/.cursor/mcp.json)" = "$bin" ] && [ "$(jq -r '.mcpServers.other.command' ~/.cursor/mcp.json)" = x ] && pass "cursor: entry added, others kept" || fail "cursor: $(cat ~/.cursor/mcp.json)"
[ "$(jq -r '.mcpServers.offstage.args[0]' ~/.codeium/windsurf/mcp_config.json)" = mcp ] && pass "windsurf: entry added" || fail "windsurf: $(cat ~/.codeium/windsurf/mcp_config.json 2>&1)"
[ "$(jq -r '.mcp.offstage.type' ~/.config/opencode/opencode.json)" = local ] && [ "$(jq -r '.mcp.offstage.command[1]' ~/.config/opencode/opencode.json)" = mcp ] && pass "opencode: entry added" || fail "opencode: $(cat ~/.config/opencode/opencode.json 2>&1)"
for d in .codex/skills .cursor/skills .codeium/windsurf/skills .config/opencode/skills; do [ -s ~/$d/offstage/SKILL.md ] || fail "skill missing in ~/$d"; done; [ -s ~/.codex/skills/offstage/SKILL.md ] && pass "skill copied to the agents' skill directories"

echo "== a tampered checksum is refused"
rm -f ~/.local/bin/offstage; rm -rf ~/bad; mkdir -p ~/bad
curl -fsSL -o ~/bad/offstage-linux-amd64 "$OFFSTAGE_RELEASE_BASE/offstage-linux-amd64"
echo "0000000000000000000000000000000000000000000000000000000000000000  offstage-linux-amd64" > ~/bad/SHA256SUMS
(cd ~/bad && python3 -m http.server 8999 --bind 127.0.0.1 >/dev/null 2>&1 &); sleep 1
OFFSTAGE_RELEASE_BASE=http://127.0.0.1:8999 bash "$R/install.sh" --binary-only > ~/bad.log 2>&1 && fail "tampered binary accepted" || { grep -q 'checksum mismatch' ~/bad.log && [ ! -e ~/.local/bin/offstage ] && pass "checksum mismatch refused, nothing installed" || fail "wrong failure: $(tail -2 ~/bad.log)"; }
pkill -f 'http.server 8999' >/dev/null 2>&1

echo "== plugin launcher: first run installs the binary, then serves MCP"
rm -f ~/.local/bin/offstage
bash "$R/bin/offstage-mcp" < /dev/null > ~/launcher.out 2> ~/launcher.err; rc=$?
[ -x ~/.local/bin/offstage ] && pass "launcher installed the binary on first run" || fail "launcher did not install: $(tail -2 ~/launcher.err)"
[ $rc = 0 ] && pass "launcher served MCP and exited cleanly on EOF" || fail "launcher rc=$rc: $(tail -2 ~/launcher.err)"
grep -q '^width = 1024' ~/.config/offstage/config.toml && pass "an edited config survives a reinstall" || fail "config overwritten: $(grep '^width' ~/.config/offstage/config.toml)"
echo "== plugin launcher: an outdated release is updated, a dev build is kept"
cp ~/.local/bin/offstage ~/offstage.real
printf '#!/bin/sh\n[ "$1" = version ] && echo v0.0.1\n' > ~/.local/bin/offstage; chmod +x ~/.local/bin/offstage
bash "$R/bin/offstage-mcp" < /dev/null > /dev/null 2> ~/launcher2.err
{ grep -q 'updating the binary' ~/launcher2.err && [ "$(~/.local/bin/offstage version)" != v0.0.1 ]; } && pass "launcher replaced an outdated binary" || fail "launcher kept v0.0.1: $(tail -2 ~/launcher2.err)"
printf '#!/bin/sh\n[ "$1" = version ] && echo dev\n' > ~/.local/bin/offstage; chmod +x ~/.local/bin/offstage
bash "$R/bin/offstage-mcp" < /dev/null > /dev/null 2> ~/launcher3.err
[ "$(~/.local/bin/offstage version)" = dev ] && pass "launcher leaves a dev build alone" || fail "launcher replaced a dev build: $(tail -2 ~/launcher3.err)"
mv ~/offstage.real ~/.local/bin/offstage
bash "$R/bin/offstage-hook" session-start < /dev/null >/dev/null 2>&1 && pass "hook shim runs the hook" || fail "hook shim failed"
mv ~/.local/bin/offstage ~/offstage.bak; bash "$R/bin/offstage-hook" session-start < /dev/null >/dev/null 2>&1 && pass "hook shim is silent without the binary" || fail "hook shim failed without the binary"; mv ~/offstage.bak ~/.local/bin/offstage

echo "== offstage setup"
sudo pacman -R --noconfirm cage >/dev/null 2>&1
out=$(offstage setup --json 2>&1)
{ echo "$out" | jq -e '.installed | index("cage")' >/dev/null && command -v cage >/dev/null; } && pass "setup installed cage via $(echo "$out" | jq -r .method)" || fail "setup: $out"
offstage setup | grep -q 'nothing to install' && pass "setup is idempotent" || fail "setup second run"
offstage doctor --json | jq -e '.[]|select(.name=="setup")' >/dev/null && fail "doctor still shows a setup row with nothing missing" || pass "doctor has no setup row when nothing is missing"

echo "== uninstall"
bash "$R/install.sh" --uninstall >/dev/null 2>&1
[ ! -e ~/.local/bin/offstage ] && [ ! -e ~/.local/state/offstage ] && pass "uninstall removed the binary and the state" || fail "uninstall left files"
grep -q "codex mcp remove offstage" ~/fake-agents.log && grep -q "gemini mcp remove -s user offstage" ~/fake-agents.log && pass "uninstall: codex and gemini unregistered" || fail "uninstall: $(grep remove ~/fake-agents.log)"
[ "$(jq -r '.mcpServers.offstage // "gone"' ~/.cursor/mcp.json)" = gone ] && [ "$(jq -r '.mcpServers.other.command' ~/.cursor/mcp.json)" = x ] && [ "$(jq -r '.mcp.offstage // "gone"' ~/.config/opencode/opencode.json)" = gone ] && pass "uninstall: JSON entries removed, others kept" || fail "uninstall: cursor=$(cat ~/.cursor/mcp.json) opencode=$(cat ~/.config/opencode/opencode.json)"
[ ! -e ~/.cursor/skills/offstage ] && [ ! -e ~/.codex/skills/offstage ] && pass "uninstall: skills removed" || fail "uninstall: skills left"
echo "== --agents limits the registration"
n=$(grep -c 'mcp add' ~/fake-agents.log); mkdir -p ~/.cursor ~/.config/opencode
bash "$R/install.sh" --agents cursor >/dev/null 2>&1
[ "$(jq -r '.mcpServers.offstage.command' ~/.cursor/mcp.json)" = "$bin" ] && pass "--agents cursor: cursor registered" || fail "--agents cursor: $(cat ~/.cursor/mcp.json)"
[ "$(jq -r '.mcp.offstage // "none"' ~/.config/opencode/opencode.json 2>/dev/null || echo none)" = none ] && [ "$(grep -c 'mcp add' ~/fake-agents.log)" = "$n" ] && pass "--agents cursor: nobody else touched" || fail "--agents cursor: others touched"
bash "$R/install.sh" --uninstall >/dev/null 2>&1
rm -f ~/.local/bin/codex ~/.local/bin/gemini; rm -rf ~/.cursor ~/.codeium ~/.config/opencode ~/.codex ~/.claude
command -v cage >/dev/null && pass "uninstall leaves cage, as documented" || fail "uninstall removed cage"
bash "$R/install.sh" --binary-only >/dev/null 2>&1 || true   # back in place for the other suites
echo; [ $FAILED = 0 ] && echo "ALL PASS" || echo "SOME FAILURES"; exit $FAILED
