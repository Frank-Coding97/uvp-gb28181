@echo off
rem ============================================================
rem  UVP-GB28181 standalone package (Windows)  --  STATUS
rem
rem  NOTE: this file is intentionally PURE ASCII.
rem  cmd.exe decodes .cmd files with the OEM code page (936 on
rem  zh-CN Windows), so Chinese text stored as UTF-8 here would
rem  come out garbled. Keep it English.
rem
rem  Set UVP_NO_PAUSE=1 to skip the final "press any key" prompt
rem  (useful when calling this from another script / scheduler).
rem ============================================================
setlocal
set "CTL=%~dp0uvp-gb28181-ctl.ps1"
set "PS=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
if not exist "%PS%" set "PS=powershell.exe"

if not exist "%CTL%" goto :missing

"%PS%" -NoProfile -ExecutionPolicy Bypass -File "%CTL%" status
set "RC=%ERRORLEVEL%"
goto :done

:missing
echo.
echo [ERROR] uvp-gb28181-ctl.ps1 was not found next to this file:
echo         %CTL%
echo         Please unzip the whole package again and retry.
echo.
set "RC=1"

:done
if not defined UVP_NO_PAUSE pause
exit /b %RC%
