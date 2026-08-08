@echo off
echo ===================================================
echo 🛠️  PhotoSync Server ^& Installer Build Script
echo ===================================================

set SCRIPT_DIR=%~dp0
set SERVER_DIR=%SCRIPT_DIR%..\server

echo.
echo 📦 1. Compiling Go Server Executable (photosync_server.exe)...
cd /d "%SERVER_DIR%"
go build -o photosync_server.exe .
if %ERRORLEVEL% NEQ 0 (
    echo ❌ Error: Failed to compile Go server!
    exit /b %ERRORLEVEL%
)
echo ✅ Go Server compiled successfully.

echo.
echo 🚚 2. Copying binary to windowsservice directory...
copy /Y "%SERVER_DIR%\photosync_server.exe" "%SCRIPT_DIR%\photosync_server.exe"
if %ERRORLEVEL% NEQ 0 (
    echo ❌ Error: Failed to copy server binary!
    exit /b %ERRORLEVEL%
)

echo.
echo 🔨 3. Compiling NSIS Installer Package...
cd /d "%SCRIPT_DIR%"

set MAKENSIS=C:\Program Files (x86)\NSIS\makensis.exe
if exist "%MAKENSIS%" (
    "%MAKENSIS%" filesync.nsi
) else (
    makensis filesync.nsi
)

if %ERRORLEVEL% NEQ 0 (
    echo ❌ Error: NSIS build failed!
    exit /b %ERRORLEVEL%
)

echo.
echo ===================================================
echo 🎉 SUCCESS! Installer package created:
echo    %SCRIPT_DIR%PhotoSyncInstaller.exe
echo ===================================================
