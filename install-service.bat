@echo off
chcp 65001 >nul
title CasaOS Auto-Start Setup

echo ================================================================
echo       CasaOS Windows - Setup Auto-Start on System Boot
echo ================================================================
echo.

:: Ensure admin rights
net session >nul 2>&1
if %errorLevel% neq 0 (
    echo [!] Please right-click this file and select 'Run as administrator'.
    echo.
    pause
    exit /b
)

set SCRIPT_DIR=%~dp0
set TASK_NAME=CasaOS_FileServer

echo [*] Registering Windows Scheduled Task to run CasaOS on boot...
schtasks /create /tn "%TASK_NAME%" /tr "\"%SCRIPT_DIR%start-server.bat\"" /sc onstart /ru SYSTEM /f >nul 2>&1

if %errorlevel% equ 0 (
    echo [OK] CasaOS File Server has been successfully configured to run on Windows startup!
    echo [OK] Task Name: %TASK_NAME%
) else (
    echo [!] Failed to create scheduled task. Please check permissions.
)

echo.
pause
