@echo off
chcp 65001 >nul
title JoyCode2Api 代理服务

echo ========================================================
echo               正在启动 JoyCode2Api 代理服务...
echo ========================================================
echo.
echo 访问地址: http://127.0.0.1:34891
echo API 端点: http://127.0.0.1:34891/v1
echo.
echo 按 Ctrl+C 可停止服务
echo --------------------------------------------------------

cd /d "%~dp0JoyCode2Api"
JoyCode2Api.exe serve --skip-validation --tls=false
pause