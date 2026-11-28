@echo off
rem ============================================================
rem  UVP-GB28181 standalone package (Windows)  --  STOP
rem
rem  NOTE: this file is intentionally PURE ASCII.
rem  cmd.exe decodes .cmd files with the OEM code page (936 on
rem  zh-CN Windows), so Chinese text stored as UTF-8 here would
rem  come out garbled. Keep it English.
rem
rem  Set UVP_NO_PAUSE=1 to skip the final "press any key" prompt
rem  (useful when calling this from another script / scheduler).
rem
rem  The tray launcher passes UVP_STOP_MARKER=<file>: the exit code is
rem  written there BEFORE the pause, so the tray can tell when the
rem  stop really finished. (The window is opened by `cmd /c start`,
rem  so its process id / exit code are not visible to the tray.)
rem ============================================================
setlocal
title UVP-GB28181 - Stopping
set "CTL=%~dp0uvp-gb28181-ctl.ps1"
set "PS=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
if not exist "%PS%" set "PS=powershell.exe"

if not exist "%CTL%" goto :missing

echo.
echo  Stopping UVP-GB28181 services - please keep this window open.
echo  (closing it mid-way only aborts the stop; the tray will finish it silently)
echo.
"%PS%" -NoProfile -ExecutionPolicy Bypass -File "%CTL%" stop
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
if not defined UVP_STOP_MARKER goto :nopause
>"%UVP_STOP_MARKER%" echo %RC%
:nopause
if defined UVP_NO_PAUSE exit /b %RC%
echo.
echo ===============================================================
if "%RC%"=="0" echo  [OK] All services stopped - no leftover processes.
if not "%RC%"=="0" echo  [ERROR] Some components could not be stopped - see above.
echo.
echo  You can close this window now.
echo  The UVP-GB28181 icon in the system tray (bottom-right; may be
echo  hidden under the "show hidden icons" arrow) keeps running and
echo  can start the service again.
echo ===============================================================
echo.
pause
exit /b %RC%
