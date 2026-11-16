# offstage installer for Windows: user level, no admin rights, no system setting changed.
#
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/mrwick1/offstage/main/install.ps1)))
#
# What it does, in order:
#   1. downloads offstage.exe from the GitHub release, checks it against the
#      SHA256SUMS published with it, and puts it in %LOCALAPPDATA%\offstage\bin.
#   2. writes the configuration file with the defaults (an existing file stays).
#   3. registers the MCP server with the agents it finds: Claude Code and Codex.
#      bare, the e2e browser, comes with it: bare-mcp.exe and bare-drive.exe
#      from the bare release, checksum verified, into the same directory. A
#      bare download that fails is a warning: offstage still installs.
#   4. warns when Chrome or ffmpeg is missing. It installs neither.
#
# Options and environment:
#   -Agents LIST             claude, codex, or none (default: every agent found)
#   -NoBare                  do not install or register bare (or OFFSTAGE_NO_BARE=1)
#   -BinaryOnly              step 1 only, bare included (what the plugin's launcher runs)
#   -Uninstall               remove the binary, the registrations and offstage's state
#   OFFSTAGE_VERSION=vX.Y.Z  pin a release (default: the plugin's version, else the latest)
#   OFFSTAGE_BIN_DIR=DIR     where offstage.exe goes (default %LOCALAPPDATA%\offstage\bin)
#   OFFSTAGE_RELEASE_BASE=URL  where the assets are fetched from (tests)
#   OFFSTAGE_BARE_VERSION=vX.Y.Z  pin the bare release (default below)
#   OFFSTAGE_BARE_RELEASE_BASE=URL  where the bare assets are fetched from (tests)
param([string]$Agents = 'all', [switch]$BinaryOnly, [switch]$Uninstall, [switch]$NoBare)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$Repo = if ($env:OFFSTAGE_REPO) { $env:OFFSTAGE_REPO } else { 'mrwick1/offstage' }
$Dir = Join-Path $env:LOCALAPPDATA 'offstage'
$BinDir = if ($env:OFFSTAGE_BIN_DIR) { $env:OFFSTAGE_BIN_DIR } else { Join-Path $Dir 'bin' }
$Exe = Join-Path $BinDir 'offstage.exe'
$BareMcp = Join-Path $BinDir 'bare-mcp.exe'
$BareDrive = Join-Path $BinDir 'bare-drive.exe'
# The bare release that this offstage ships with. Keep it equal to the default
# in install.sh.
$BareVersion = if ($env:OFFSTAGE_BARE_VERSION) { $env:OFFSTAGE_BARE_VERSION } else { 'v0.1.0' }
$SkipBare = $NoBare -or $env:OFFSTAGE_NO_BARE -eq '1'

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
  if ($env:OFFSTAGE_VERSION) { return $env:OFFSTAGE_VERSION }
  # The plugin pins its release: plugin and binary evolve together.
  $manifest = if ($PSScriptRoot) { Join-Path $PSScriptRoot '.claude-plugin\plugin.json' }
  if ($manifest -and (Test-Path $manifest)) { return 'v' + (Get-Content $manifest -Raw | ConvertFrom-Json).version }
  (Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest").tag_name
}

function Install-Binary {
  $want = Get-Want
  $have = if (Test-Path $Exe) { try { (& $Exe version | Select-Object -First 1) } catch { '' } }
  if ($have -eq $want) { Say "offstage $want already in $BinDir"; return }
  # The launcher runs this at every start: leave a build from source and a newer release alone.
  if ($BinaryOnly -and $have) {
    if ($have -notmatch '^v\d+\.\d+\.\d+$' -or [version]$have.Substring(1) -ge [version]$want.Substring(1)) { return }
  }
  $asset = 'offstage-windows-amd64.exe'
  $base = if ($env:OFFSTAGE_RELEASE_BASE) { $env:OFFSTAGE_RELEASE_BASE } else { "https://github.com/$Repo/releases/download/$want" }
  $tmp = Join-Path ([IO.Path]::GetTempPath()) ([guid]::NewGuid())
  New-Item -ItemType Directory $tmp | Out-Null
  try {
    Say "downloading offstage $want for windows/amd64"
    Invoke-WebRequest "$base/$asset" -OutFile "$tmp\$asset"
    Invoke-WebRequest "$base/SHA256SUMS" -OutFile "$tmp\SHA256SUMS"
    $line = Select-String -Path "$tmp\SHA256SUMS" -Pattern "\s\*?$([regex]::Escape($asset))$" | Select-Object -First 1
    if (-not $line) { throw "no checksum for $asset in SHA256SUMS" }
    if ((Get-FileHash "$tmp\$asset" -Algorithm SHA256).Hash -ne ($line.Line -split '\s+')[0]) { throw "checksum mismatch for ${asset}: not installing" }
    New-Item -ItemType Directory -Force $BinDir | Out-Null
    # A running offstage.exe cannot be overwritten, but it can be renamed.
    if (Test-Path $Exe) { Move-Item -Force $Exe "$Exe.old" }
    Move-Item "$tmp\$asset" $Exe
  } finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
  }
  Remove-Item "$Exe.old" -ErrorAction SilentlyContinue
  Say "installed $Exe ($(& $Exe version | Select-Object -First 1))"
}

# Download, verify and install bare-mcp.exe and bare-drive.exe. A failure is a
# warning: offstage works without bare.
function Install-Bare {
  if ($SkipBare) { Say 'bare skipped'; return }
  $tmp = Join-Path ([IO.Path]::GetTempPath()) ([guid]::NewGuid())
  New-Item -ItemType Directory $tmp | Out-Null
  try {
    $base = if ($env:OFFSTAGE_BARE_RELEASE_BASE) { $env:OFFSTAGE_BARE_RELEASE_BASE } else { "https://github.com/mrwick1/bare/releases/download/$BareVersion" }
    $sums = "$tmp\SHA256SUMS"
    Invoke-WebRequest "$base/SHA256SUMS" -OutFile $sums
    function Want($asset) {
      $l = Select-String -Path $sums -Pattern "\s\*?$([regex]::Escape($asset))$" | Select-Object -First 1
      if (-not $l) { throw "no checksum for $asset in SHA256SUMS" }
      ($l.Line -split '\s+')[0]
    }
    $mcp = 'bare-mcp-windows-amd64.exe'; $drive = 'bare-drive-windows-amd64.exe'
    $wantMcp = Want $mcp; $wantDrive = Want $drive
    if ((Test-Path $BareMcp) -and (Test-Path $BareDrive) -and (Get-FileHash $BareMcp -Algorithm SHA256).Hash -eq $wantMcp) {
      Say "bare $BareVersion already in $BinDir"; return
    }
    Say "downloading bare $BareVersion for windows/amd64"
    foreach ($p in @(@($mcp, $wantMcp, $BareMcp), @($drive, $wantDrive, $BareDrive))) {
      Invoke-WebRequest "$base/$($p[0])" -OutFile "$tmp\$($p[0])"
      if ((Get-FileHash "$tmp\$($p[0])" -Algorithm SHA256).Hash -ne $p[1]) { throw "checksum mismatch for $($p[0]): not installing" }
    }
    New-Item -ItemType Directory -Force $BinDir | Out-Null
    foreach ($p in @(@($mcp, $BareMcp), @($drive, $BareDrive))) {
      # A running exe cannot be overwritten, but it can be renamed.
      if (Test-Path $p[1]) { Move-Item -Force $p[1] "$($p[1]).old" }
      Move-Item "$tmp\$($p[0])" $p[1]
      Remove-Item "$($p[1]).old" -ErrorAction SilentlyContinue
    }
    Say "installed bare-mcp.exe and bare-drive.exe in $BinDir"
  } catch {
    Warn "could not install bare ${BareVersion}: $($_.Exception.Message). offstage works without it; rerun the installer later"
  } finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
  }
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
    if (Try-Run claude mcp get offstage) { Say 'claude: already registered' }
    elseif (Try-Run claude mcp add --scope user offstage -- $Exe mcp) { Say 'claude: MCP server registered' }
    else { Warn "claude: run  claude mcp add --scope user offstage -- $Exe mcp" }
  }
  if ((Wanted 'codex') -and (Have 'codex')) {
    $found = $true
    if (Select-String -Path (Join-Path $HOME '.codex\config.toml') -Pattern '^\[mcp_servers\.offstage\]' -Quiet -ErrorAction SilentlyContinue) { Say 'codex: already registered' }
    elseif (Try-Run codex mcp add offstage -- $Exe mcp) { Say 'codex: MCP server registered' }
    else { Warn "codex: run  codex mcp add offstage -- $Exe mcp" }
  }
  # bare: its reports go through offstage's report_issue. A bare that is
  # already registered stays as it is.
  if (-not $SkipBare -and (Test-Path $BareMcp)) {
    $env1 = 'BARE_MCP_REPORT=offstage'
    if ((Wanted 'claude') -and (Have 'claude')) {
      if (Try-Run claude mcp get bare) { Say 'claude: bare already registered, left alone' }
      elseif (Try-Run claude mcp add --scope user bare -e $env1 -- $BareMcp) { Say 'claude: bare registered' }
      else { Warn "claude: run  claude mcp add --scope user bare -e $env1 -- $BareMcp" }
    }
    if ((Wanted 'codex') -and (Have 'codex')) {
      if (Select-String -Path (Join-Path $HOME '.codex\config.toml') -Pattern '^\[mcp_servers\.bare\]' -Quiet -ErrorAction SilentlyContinue) { Say 'codex: bare already registered, left alone' }
      elseif (Try-Run codex mcp add --env $env1 bare -- $BareMcp) { Say 'codex: bare registered' }
      else { Warn "codex: run  codex mcp add --env $env1 bare -- $BareMcp" }
    }
  }
  if (-not $found) { Say "no selected agent found; any MCP client can run  $Exe mcp  (see the README)" }
}

function Test-Tools {
  $chrome = (Have 'chrome.exe') -or @($env:ProgramFiles, ${env:ProgramFiles(x86)}, $env:LOCALAPPDATA |
    Where-Object { $_ } | ForEach-Object { Join-Path $_ 'Google\Chrome\Application\chrome.exe' } | Where-Object { Test-Path $_ }).Count
  if (-not $chrome) { Warn 'Chrome not found: offstage drives Chrome. Install it from https://www.google.com/chrome' }
  # A winget install in this shell does not update its PATH: read the saved one.
  $env:Path = @([Environment]::GetEnvironmentVariable('Path', 'Machine'), [Environment]::GetEnvironmentVariable('Path', 'User'), $env:Path) -join ';'
  if (-not (Have 'ffmpeg')) { Warn 'ffmpeg not found: recording needs it. Install it with  winget install ffmpeg' }
}

function Remove-All {
  Say "removing offstage from the agents, the binary and offstage's state (Chrome and ffmpeg stay)"
  if (Have 'claude') { [void](Try-Run claude mcp remove --scope user offstage) }
  if (Have 'codex') { [void](Try-Run codex mcp remove offstage) }
  # Only when this installer put bare there: a bare of the user's own stays.
  if (Test-Path $BareMcp) {
    if (Have 'claude') { [void](Try-Run claude mcp remove --scope user bare) }
    if (Have 'codex') { [void](Try-Run codex mcp remove bare) }
  }
  if (Test-Path $Exe) { [void](Try-Run $Exe gc --all) }
  Remove-Item -Recurse -Force $Dir, (Join-Path $env:APPDATA 'offstage'), $Exe, $BareMcp, $BareDrive -ErrorAction SilentlyContinue
}

try {
  if ($Uninstall) { Remove-All; return }
  Install-Binary
  Install-Bare
  Write-Config
  if (-not $BinaryOnly) { Register-Agents; Test-Tools }
} catch {
  [Console]::Error.WriteLine("error: $($_.Exception.Message)")
  exit 1
}
