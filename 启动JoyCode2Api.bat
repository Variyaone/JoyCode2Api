@echo off
chcp 65001 >nul
title JoyCode2Api Daemon

cd /d "%~dp0"

.\JoyCode2Api.exe daemon status 2>nul | findstr /C:"Daemon running" >nul
if %errorlevel%==0 exit /b 0

.\JoyCode2Api.exe daemon start --port 34891 --skip-validation
if not %errorlevel%==0 (
    echo.
    echo Start FAILED. Logs: %USERPROFILE%\.joycode-proxy\logs\daemon.log
    echo.
    pause
)
