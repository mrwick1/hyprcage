@echo off
rem MCP launcher for Windows, the twin of hyprcage-mcp: finds hyprcage.exe,
rem lets install.ps1 install or update it when it is not the version the
rem plugin pins, then serves MCP on stdio. Messages go to stderr: stdout is
rem the MCP channel.
setlocal
set "root=%~dp0.."
set "bin="
for /f "delims=" %%b in ('where hyprcage.exe 2^>nul') do if not defined bin set "bin=%%b"
if not defined bin if exist "%LOCALAPPDATA%\hyprcage\bin\hyprcage.exe" set "bin=%LOCALAPPDATA%\hyprcage\bin\hyprcage.exe"
set "want="
for /f "tokens=2 delims=:" %%v in ('findstr /c:"\"version\"" "%root%\.claude-plugin\plugin.json"') do if not defined want set "want=%%v"
set want=%want:"=%
set want=%want:,=%
set want=%want: =%
set "have="
if defined bin for /f %%h in ('"%bin%" version') do if not defined have set "have=%%h"
if not "%have%"=="v%want%" powershell -NoProfile -ExecutionPolicy Bypass -File "%root%\install.ps1" -BinaryOnly 1>&2
if not defined bin set "bin=%LOCALAPPDATA%\hyprcage\bin\hyprcage.exe"
if not exist "%bin%" echo hyprcage: could not install the binary; run install.ps1 from a terminal 1>&2
if not exist "%bin%" exit /b 1
"%bin%" mcp
exit /b %errorlevel%
