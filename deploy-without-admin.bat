@echo off
setlocal EnableExtensions EnableDelayedExpansion
rem chcp 65001 removed: GBK-encoded file, cmd default codepage 936
title JoyCode2Api Deploy (no admin) - transactional hot swap
cd /d "%~dp0"

rem ============================================================
rem  JoyCode2Api 无管理员权限事务化部署脚本
rem  流程: 定位新二进制 -> 停服 -> 备份 -> 替换 -> 启动
rem        -> 健康检查(90s) -> 成功清理 / 失败自动回滚
rem
rem  用法:
rem    deploy-without-admin.bat                 自动探测新二进制
rem    deploy-without-admin.bat <new.exe 路径>  指定新二进制
rem    deploy-without-admin.bat --dry-run       只做检查，不改动任何文件
rem    deploy-without-admin.bat --port 34891    指定端口(默认 34891)
rem ============================================================

set "EXE=JoyCode2Api.exe"
set "PORT=34891"
set "DRYRUN=0"
set "NEWEXE="
set "TS="
set "NEED_ROLLBACK=0"
set "HEALTH_URL=http://127.0.0.1:%PORT%/api/health"
set "LOG_DIR=%USERPROFILE%\.joycode-proxy\logs"

rem ---------------- 参数解析 ----------------
rem 注意: 用 goto 循环而非 shift，避免脚本名含连字符时 %~1 解析异常
set "ARG1=%~1"
set "ARG2=%~2"
:parse_args
if not defined ARG1 goto args_done
if /I "%ARG1%"=="--dry-run" ( set "DRYRUN=1" & set "ARG1=%ARG2%" & set "ARG2=" & goto parse_args )
if /I "%ARG1%"=="-n"        ( set "DRYRUN=1" & set "ARG1=%ARG2%" & set "ARG2=" & goto parse_args )
if /I "%ARG1%"=="--port"    ( set "PORT=%ARG2%" & set "HEALTH_URL=http://127.0.0.1=%ARG2%/api/health" & set "ARG1=" & set "ARG2=" & goto parse_args )
if /I "%ARG1%"=="--help"    goto usage
if /I "%ARG1%"=="-h"        goto usage
set "NEWEXE=%ARG1%"
set "ARG1="
goto parse_args
:args_done
set "HEALTH_URL=http://127.0.0.1:%PORT%/api/health"

echo ============================================================
echo   JoyCode2Api Deploy (no admin)
if "%DRYRUN%"=="1" echo   MODE: DRY-RUN (不会修改任何文件)
echo   dir : %CD%
echo   port: %PORT%
echo ============================================================
echo.

rem ---------------- [1/7] 定位新二进制 ----------------
echo [1/7] 定位待部署二进制 ...
if defined NEWEXE (
  if not exist "%NEWEXE%" (
    echo [FAIL] 指定的新二进制不存在: %NEWEXE%
    goto :fail
  )
  goto found_new
)
rem 自动探测（兼容多种命名）
for %%F in ("JoyCode2Api.exe.new" "JoyCode2Api.new.exe" "JoyCode2Api-new.exe" "JoyCode2Api.exe.new.exe") do (
  if not defined NEWEXE if exist "%%~F" set "NEWEXE=%%~F"
)
if not defined NEWEXE (
  echo [SKIP] 未找到待部署的新二进制。
  echo        请先把新版本放到以下任一名称后重试:
  echo          JoyCode2Api.exe.new  /  JoyCode2Api.new.exe
  echo        或直接指定路径: deploy-without-admin.bat D:\path\to\new.exe
  goto :fail
)
:found_new
echo       新二进制: %NEWEXE%

rem 同名自检：若新二进制与当前 exe 字节相同则无需部署
if /I not "%NEWEXE%"=="%EXE%" (
  if exist "%EXE%" (
    fc /b "%EXE%" "%NEWEXE%" >nul 2>&1
    if not errorlevel 1 (
      echo [SKIP] 新二进制与当前 %EXE% 内容完全相同，无需部署。
      goto :fail
    )
  )
)

rem ---------------- [2/7] 停止服务 ----------------
echo [2/7] 停止 daemon 与所有 %EXE% 进程 ...
if "%DRYRUN%"=="1" (
  echo       [dry-run] 跳过停服
  goto after_stop
)
if exist "%EXE%" (
  "%EXE%" daemon stop >nul 2>&1
)
taskkill /F /IM "%EXE%" >nul 2>&1
call :wait_no_proc 10
if errorlevel 1 (
  echo [FAIL] %EXE% 仍在运行，请在任务管理器中结束后再重试。
  goto :fail
)
echo       所有进程已停止
:after_stop

rem ---------------- [3/7] 备份当前二进制 ----------------
echo [3/7] 备份当前二进制 ...
if "%DRYRUN%"=="1" (
  echo       [dry-run] 跳过备份
  goto after_backup
)
if not exist "%EXE%" (
  echo [WARN] 当前目录没有 %EXE%，跳过备份（首次部署场景）。
  goto after_backup
)
for /f "tokens=1-4 delims=/: " %%a in ("%date% %time%") do set "TS=%%a%%b%%c_%%d"
set "TS=%TS: =0%"
set "TS=%TS:,=%"
set "TS=%TS:.=%"
set "BAK=%EXE%.bak-%TS%"
move /y "%EXE%" "%BAK%" >nul
if errorlevel 1 (
  echo [FAIL] 备份失败，文件可能被占用。
  goto :fail
)
echo       已备份为 %BAK%
:after_backup

rem ---------------- [4/7] 替换二进制 ----------------
echo [4/7] 替换二进制 ...
if "%DRYRUN%"=="1" (
  echo       [dry-run] 跳过替换
  goto after_swap
)
move /y "%NEWEXE%" "%EXE%" >nul
if errorlevel 1 (
  echo [FAIL] 替换失败，正在回滚 ...
  if defined BAK if exist "%BAK%" move /y "%BAK%" "%EXE%" >nul
  goto :fail
)
echo       已替换为 %NEWEXE%
:after_swap

rem ---------------- [5/7] 启动 daemon ----------------
echo [5/7] 启动 daemon ...
if "%DRYRUN%"=="1" (
  echo       [dry-run] 跳过启动
  goto after_start
)
set "NEED_ROLLBACK=1"
start "" /min "%EXE%" daemon start --port %PORT% --skip-validation
call :wait_health 45
if errorlevel 1 goto :health_fail
:after_start

rem ---------------- [6/7] 健康检查 ----------------
echo [6/7] 健康检查 ...
if "%DRYRUN%"=="1" (
  echo       [dry-run] 跳过健康检查
  goto after_health
)
echo       %HEALTH_URL% -> OK
curl.exe -s "%HEALTH_URL%"
echo.
:after_health

rem ---------------- [7/7] 收尾 ----------------
echo [7/7] 收尾 ...
if "%DRYRUN%"=="1" (
  echo       [dry-run] 完成，未做任何改动。
  goto :done
)
set "NEED_ROLLBACK=0"
echo.
echo ============================================================
echo   SUCCESS - 部署完成
echo   Dashboard: http://127.0.0.1:%PORT%
echo ============================================================
goto :done

rem ---------------- 失败回滚 ----------------
:health_fail
echo [FAIL] 90 秒内健康检查未通过，正在回滚 ...
taskkill /F /IM "%EXE%" >nul 2>&1
call :wait_no_proc 10
if exist "%EXE%" move /y "%EXE%" "%EXE%.bad" >nul 2>&1
if defined BAK if exist "%BAK%" (
  move /y "%BAK%" "%EXE%" >nul
  echo       已恢复旧版本，重新启动 ...
  start "" /min "%EXE%" daemon start --port %PORT% --skip-validation
  call :wait_health 20
)
echo       回滚完成。请检查日志:
echo         %LOG_DIR%\daemon.log
echo         %LOG_DIR%\serve.log
goto :fail

:fail
echo.
echo 部署未完成。日志目录: %LOG_DIR%
if "%DRYRUN%"=="1" exit /b 1
pause
exit /b 1

:done
if "%DRYRUN%"=="1" exit /b 0
pause
exit /b 0

rem ============================================================
rem  子过程
rem ============================================================

rem 等待 %EXE% 进程全部退出，最多 %1 秒；成功返回 0，超时返回 1
:wait_no_proc
set /a _w=0
:wait_no_proc_loop
tasklist /FI "IMAGENAME eq %EXE%" 2>nul | findstr /I /C:"%EXE%" >nul
if errorlevel 1 exit /b 0
set /a _w+=1
if %_w% geq %1 exit /b 1
ping -n 2 127.0.0.1 >nul
goto wait_no_proc_loop

rem 轮询健康检查，最多 %1 次（每次约 2 秒）；健康返回 0，超时返回 1
:wait_health
set /a _h=0
:wait_health_loop
curl.exe -s -o NUL -m 2 "%HEALTH_URL%" 2>nul
if not errorlevel 1 exit /b 0
set /a _h+=1
if %_h% geq %1 exit /b 1
ping -n 3 127.0.0.1 >nul
goto wait_health_loop

:usage
echo 用法:
echo   deploy-without-admin.bat [--dry-run] [--port N] [新二进制路径]
echo.
echo   --dry-run   只做检查，不修改任何文件
echo   --port N    指定服务端口（默认 34891）
echo   新二进制路径  留空则自动探测 JoyCode2Api.exe.new / JoyCode2Api.new.exe
exit /b 0