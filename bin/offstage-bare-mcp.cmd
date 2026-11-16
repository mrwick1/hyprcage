@echo off
rem MCP launcher for bare on Windows, the twin of offstage-bare-mcp: finds
rem bare-mcp.exe and bare-drive.exe in the offstage bin directory, lets
rem install.ps1 install them when one is missing, then serves MCP on stdio.
rem Messages go to stderr: stdout is the MCP channel.
setlocal
set "root=%~dp0.."
set "dir=%LOCALAPPDATA%\offstage\bin"
if defined OFFSTAGE_BIN_DIR set "dir=%OFFSTAGE_BIN_DIR%"
if not exist "%dir%\bare-mcp.exe" powershell -NoProfile -ExecutionPolicy Bypass -File "%root%\install.ps1" -BareOnly 1>&2
if not exist "%dir%\bare-drive.exe" powershell -NoProfile -ExecutionPolicy Bypass -File "%root%\install.ps1" -BareOnly 1>&2
if not exist "%dir%\bare-mcp.exe" echo bare: could not install bare-mcp.exe; run install.ps1 from a terminal 1>&2
if not exist "%dir%\bare-mcp.exe" exit /b 1
set "BARE_MCP_REPORT=offstage"
"%dir%\bare-mcp.exe"
exit /b %errorlevel%
