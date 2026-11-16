# hyprcage installer for Windows: user level, no admin rights, no system setting changed.
#
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/mrwick1/hyprcage/main/install.ps1)))
#
# What it does, in order:
#   1. downloads hyprcage.exe from the GitHub release, checks it against the
#      SHA256SUMS published with it, and puts it in %LOCALAPPDATA%\hyprcage\bin.
#   2. writes the configuration file with the defaults (an existing file stays).
#   3. registers the MCP server with the agents it finds: Claude Code and Codex.
#   4. warns when Chrome or ffmpeg is missing. It installs neither.
#
# Options and environment:
#   -Agents LIST             claude, codex, or none (default: every agent found)
#   -BinaryOnly              step 1 only (what the plugin's launcher runs)
#   -Uninstall               remove the binary, the registrations and hyprcage's state
#   HYPRCAGE_VERSION=vX.Y.Z  pin a release (default: the plugin's version, else the latest)
#   HYPRCAGE_BIN_DIR=DIR     where hyprcage.exe goes (default %LOCALAPPDATA%\hyprcage\bin)
#   HYPRCAGE_RELEASE_BASE=URL  where the assets are fetched from (tests)
param([string]$Agents = 'all', [switch]$BinaryOnly, [switch]$Uninstall)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$Repo = if ($env:HYPRCAGE_REPO) { $env:HYPRCAGE_REPO } else { 'mrwick1/hyprcage' }
$Dir = Join-Path $env:LOCALAPPDATA 'hyprcage'
$BinDir = if ($env:HYPRCAGE_BIN_DIR) { $env:HYPRCAGE_BIN_DIR } else { Join-Path $Dir 'bin' }
$Exe = Join-Path $BinDir 'hyprcage.exe'

# Messages go to stderr: the plugin's launcher keeps stdout for MCP.
function Say($m) { [Console]::Error.WriteLine("==> $m") }
function Warn($m) { [Console]::Error.WriteLine("warning: $m") }
function Wanted($n) { $a = $Agents -split ','; ($a -contains 'all') -or ($a -contains $n) }
function Have($c) { [bool](Get-Command $c -ErrorAction SilentlyContinue) }

# Run a native command, hide its output, return whether it succeeded.
function Try-Run {
  $ErrorActionPreference = 'Continue'
  & $args[0] $args[1..($args.Count - 1)] *> $null
  $LASTEXITCODE -eq 0
}

function Get-Want {
  if ($env:HYPRCAGE_VERSION) { return $env:HYPRCAGE_VERSION }
  # The plugin pins its release: plugin and binary evolve together.
  $manifest = if ($PSScriptRoot) { Join-Path $PSScriptRoot '.claude-plugin\plugin.json' }
  if ($manifest -and (Test-Path $manifest)) { return 'v' + (Get-Content $manifest -Raw | ConvertFrom-Json).version }
  (Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest").tag_name
}

function Install-Binary {
  $want = Get-Want
  $have = if (Test-Path $Exe) { try { (& $Exe version | Select-Object -First 1) } catch { '' } }
  if ($have -eq $want) { Say "hyprcage $want already in $BinDir"; return }
  # The launcher runs this at every start: leave a build from source and a newer release alone.
  if ($BinaryOnly -and $have) {
    if ($have -notmatch '^v\d+\.\d+\.\d+$' -or [version]$have.Substring(1) -ge [version]$want.Substring(1)) { return }
  }
  $asset = 'hyprcage-windows-amd64.exe'
  $base = if ($env:HYPRCAGE_RELEASE_BASE) { $env:HYPRCAGE_RELEASE_BASE } else { "https://github.com/$Repo/releases/download/$want" }
  $tmp = Join-Path ([IO.Path]::GetTempPath()) ([guid]::NewGuid())
  New-Item -ItemType Directory $tmp | Out-Null
  try {
    Say "downloading hyprcage $want for windows/amd64"
    Invoke-WebRequest "$base/$asset" -OutFile "$tmp\$asset"
    Invoke-WebRequest "$base/SHA256SUMS" -OutFile "$tmp\SHA256SUMS"
    $line = Select-String -Path "$tmp\SHA256SUMS" -Pattern "\s\*?$([regex]::Escape($asset))$" | Select-Object -First 1
    if (-not $line) { throw "no checksum for $asset in SHA256SUMS" }
    if ((Get-FileHash "$tmp\$asset" -Algorithm SHA256).Hash -ne ($line.Line -split '\s+')[0]) { throw "checksum mismatch for ${asset}: not installing" }
    New-Item -ItemType Directory -Force $BinDir | Out-Null
    # A running hyprcage.exe cannot be overwritten, but it can be renamed.
    if (Test-Path $Exe) { Move-Item -Force $Exe "$Exe.old" }
    Move-Item "$tmp\$asset" $Exe
  } finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
  }
  Remove-Item "$Exe.old" -ErrorAction SilentlyContinue
  Say "installed $Exe ($(& $Exe version | Select-Object -First 1))"
}

# The configuration file, every key at its default. An existing file is kept.
function Write-Config {
  try { $out = & $Exe config 2>$null; if ($out -match '^wrote') { Say $out } } catch {}
}

# Each agent gets the MCP server with the absolute path, so PATH does not matter.
function Register-Agents {
  if ($Agents -eq 'none') { Say 'no agent registration asked (-Agents none)'; return }
  $found = $false
  if ((Wanted 'claude') -and (Have 'claude')) {
    $found = $true
    if (Try-Run claude mcp get hyprcage) { Say 'claude: already registered' }
    elseif (Try-Run claude mcp add --scope user hyprcage -- $Exe mcp) { Say 'claude: MCP server registered' }
    else { Warn "claude: run  claude mcp add --scope user hyprcage -- $Exe mcp" }
  }
  if ((Wanted 'codex') -and (Have 'codex')) {
    $found = $true
    if (Select-String -Path (Join-Path $HOME '.codex\config.toml') -Pattern '^\[mcp_servers\.hyprcage\]' -Quiet -ErrorAction SilentlyContinue) { Say 'codex: already registered' }
    elseif (Try-Run codex mcp add hyprcage -- $Exe mcp) { Say 'codex: MCP server registered' }
    else { Warn "codex: run  codex mcp add hyprcage -- $Exe mcp" }
  }
  if (-not $found) { Say "no selected agent found; any MCP client can run  $Exe mcp  (see the README)" }
}

function Test-Tools {
  $chrome = (Have 'chrome.exe') -or @($env:ProgramFiles, ${env:ProgramFiles(x86)}, $env:LOCALAPPDATA |
    Where-Object { $_ } | ForEach-Object { Join-Path $_ 'Google\Chrome\Application\chrome.exe' } | Where-Object { Test-Path $_ }).Count
  if (-not $chrome) { Warn 'Chrome not found: hyprcage drives Chrome. Install it from https://www.google.com/chrome' }
  if (-not (Have 'ffmpeg')) { Warn 'ffmpeg not found: recording needs it. Install it with  winget install ffmpeg' }
}

function Remove-All {
  Say "removing hyprcage from the agents, the binary and hyprcage's state (Chrome and ffmpeg stay)"
  if (Have 'claude') { [void](Try-Run claude mcp remove --scope user hyprcage) }
  if (Have 'codex') { [void](Try-Run codex mcp remove hyprcage) }
  if (Test-Path $Exe) { [void](Try-Run $Exe gc --all) }
  Remove-Item -Recurse -Force $Dir, (Join-Path $env:APPDATA 'hyprcage'), $Exe -ErrorAction SilentlyContinue
}

try {
  if ($Uninstall) { Remove-All; return }
  Install-Binary
  Write-Config
  if (-not $BinaryOnly) { Register-Agents; Test-Tools }
} catch {
  [Console]::Error.WriteLine("error: $($_.Exception.Message)")
  exit 1
}
