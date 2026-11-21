@echo off
rem ---------------------------------------------------------------------------
rem Control-script entry point used by the upgrade helper.
rem
rem Why this wrapper exists: the helper starts the registered control script with
rem CreateProcess (through cmd.exe), and neither a .ps1 nor a .cmd can be exec'd
rem directly by Go on Windows. Registering this .cmd keeps one stable command
rem line for "start" / "stop" / "status" / "migrate" while the real logic stays
rem in uvp-gb28181-ctl.ps1.
rem
rem ASCII only on purpose: cmd.exe reads this file in the OEM code page.
rem ---------------------------------------------------------------------------
setlocal
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0uvp-gb28181-ctl.ps1" %*
exit /b %ERRORLEVEL%
