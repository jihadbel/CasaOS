@echo off
cd /d "%~dp0"
chcp 65001 >nul
title CasaOS Windows File & Photo Server

echo ================================================================
echo           CasaOS Windows Home Cloud File & Photo Server        
echo ================================================================
echo.

:: Ensure default DATA directory exists
if not exist "C:\CasaOS\DATA" (
    echo [*] Creating default storage directory: C:\CasaOS\DATA
    mkdir "C:\CasaOS\DATA" 2>nul
)

:: Check if pre-compiled executable exists
if exist "casaos-server.exe" (
    echo [*] Starting CasaOS Server from binary...
    casaos-server.exe %*
    goto end
)

:: Check if Go is installed
where go >nul 2>&1
if %errorlevel% equ 0 (
    echo [*] Go environment detected. Building and running CasaOS Server...
    echo [*] Open the dashboard address printed by the server in your browser.
    go run . %*
    goto end
)

echo [!] Error: Go compiler or casaos-server.exe not found!
echo [!] To run the server, please install Go (https://go.dev/dl/) or place casaos-server.exe in this folder.
echo.
pause

:end
