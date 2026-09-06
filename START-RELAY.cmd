@echo off
title Stream Ingest Relay (SRT to vMix and Detector)
cd /d "%~dp0"

set "URL_FILE=%~dp0stream-url.txt"

if not exist "%URL_FILE%" (
    echo srt://YOUR_STREAM_IP:9001?mode=caller^&transtype=live^&latency=2000000^&pkt_size=1316> "%URL_FILE%"
)

set "SRT_URL="
for /f "delims=" %%I in ('powershell -NoProfile -Command "(Get-Content '%URL_FILE%' -ErrorAction SilentlyContinue | Select-Object -First 1).Trim()"') do (
    set "SRT_URL=%%I"
)

if "%SRT_URL%"=="" set "SRT_URL=srt://YOUR_STREAM_IP:9001?mode=caller&transtype=live&latency=2000000&pkt_size=1316"

if not "%SRT_URL:~0,6%"=="srt://" (
    set "SRT_URL=srt://%SRT_URL%?mode=caller&transtype=live&latency=2000000&pkt_size=1316"
)

echo %SRT_URL% | findstr /i "pkt_size" >nul
if errorlevel 1 (
    echo %SRT_URL% | findstr "?" >nul
    if errorlevel 1 (
        set "SRT_URL=%SRT_URL%?pkt_size=1316"
    ) else (
        set "SRT_URL=%SRT_URL%&pkt_size=1316"
    )
)

set "FFMPEG_BIN=%~dp0ffmpeg.exe"
if not exist "%FFMPEG_BIN%" (
    where ffmpeg >nul 2>&1
    if not errorlevel 1 (
        set "FFMPEG_BIN=ffmpeg"
    ) else (
        echo ========================================================
        echo  [INFO] ffmpeg.exe was not found in folder or PATH.
        echo  Downloading official FFmpeg release automatically...
        echo ========================================================
        echo.
        powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0Install-FFmpeg.ps1"
        echo.
        if not exist "%~dp0ffmpeg.exe" (
            echo ========================================================
            echo  [ERROR] Automatic FFmpeg installation failed.
            echo  Please run Install-FFmpeg.ps1 or download ffmpeg.exe
            echo  manually from https://ffmpeg.org and place it here.
            echo ========================================================
            pause
            exit /b 1
        )
    )
)

echo ========================================================
echo  Stream Ingest Relay (Raw Passthrough)
echo ========================================================
echo  Source File      : stream-url.txt
echo  Source SRT       : %SRT_URL%
echo  Output 1 (vMix)  : srt://127.0.0.1:10001 (SRT Listener, Raw TS)
echo  Output 2 (Det)   : udp://127.0.0.1:10002 (UDP, Raw TS)
echo ========================================================
echo.

:loop
echo [%DATE% %TIME%] Starting Ingest Relay...
"%FFMPEG_BIN%" -hide_banner -loglevel warning ^
  -f data -raw_packet_size 1316 ^
  -i "%SRT_URL%" ^
  -map 0:0 -c copy ^
  -f tee "[f=data]srt://127.0.0.1:10001?mode=listener&transtype=live&latency=100000&pkt_size=1316|[f=data]udp://127.0.0.1:10002?pkt_size=1316"

echo [%DATE% %TIME%] Connection lost. Reconnecting in 2 seconds...
timeout /t 2 /nobreak >nul
goto loop
