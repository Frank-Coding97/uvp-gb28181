@echo off
rem ============================================================
rem  UVP-GB28181 standalone package (Windows)  --  START
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
title UVP-GB28181 - Startup
set "CTL=%~dp0uvp-gb28181-ctl.ps1"
set "PS=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
if not exist "%PS%" set "PS=powershell.exe"

if not exist "%CTL%" goto :missing

"%PS%" -NoProfile -ExecutionPolicy Bypass -File "%CTL%" start
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
if defined UVP_NO_PAUSE exit /b %RC%
rem ---------------------------------------------------------------
rem  Tray launcher also runs this file in its own console window, so
rem  the window must stay open long enough to read the access URL.
rem  Make it explicit that closing it is harmless: the services live
rem  in their own processes and are managed by the tray icon.
rem ---------------------------------------------------------------
echo.
echo ===============================================================
if "%RC%"=="0" echo  [OK] Startup finished - the access URL is printed above.
if not "%RC%"=="0" echo  [ERROR] Startup failed - see the messages above.
echo.
echo  Closing this window does NOT stop the service.
echo  The service keeps running; use the UVP-GB28181 icon in the
echo  system tray (bottom-right; may be hidden under the small
echo  "show hidden icons" arrow) to open / restart / stop it.
echo ===============================================================
echo.
pause
exit /b %RC%
