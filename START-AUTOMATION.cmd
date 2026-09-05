@echo off
setlocal
cd /d "%~dp0"
if exist "settings.local.cmd" call "settings.local.cmd"
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Start-Detector-ACTIVE.ps1"
if errorlevel 1 pause
