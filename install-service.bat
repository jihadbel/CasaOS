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

:: Configure an absolute executable and working directory without launching a browser.
powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "%~dp0install-service.ps1"
set INSTALL_RESULT=%errorlevel%

echo.
pause
exit /b %INSTALL_RESULT%
