@echo off
chcp 65001 >nul
title JoyCode2Api Daemon

echo ========================================================
echo           Starting JoyCode2Api (daemon mode)
echo ========================================================
echo.
echo URL  http://127.0.0.1:34891
echo API  http://127.0.0.1:34891/v1
echo.

cd /d "%~dp0"

.\JoyCode2Api.exe daemon status 2>nul | findstr /C:"running" >nul
if %errorlevel%==0 (
    echo Daemon already running, nothing to do.
) else (
    .\JoyCode2Api.exe daemon start --port 34891 --skip-validation
)

echo.
echo Runs in background. Safe to close this window.
echo Logs    %USERPROFILE%\.joycode-proxy\logs\daemon.log
echo Stop    JoyCode2Api.exe daemon stop
echo Restart JoyCode2Api.exe daemon restart
echo.
pause
