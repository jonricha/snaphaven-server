@echo off
echo ===================================================
echo 🛠️  SnapHaven Server ^& Installer Build Script
echo ===================================================

set SCRIPT_DIR=%~dp0
set SERVER_DIR=%SCRIPT_DIR%..\server

echo.
echo 📦 1. Compiling Go Server Executable (snaphaven.exe)...
cd /d "%SERVER_DIR%"
go build -ldflags -H=windowsgui -o snaphaven.exe .
if %ERRORLEVEL% NEQ 0 (
    echo ❌ Error: Failed to compile Go server!
    exit /b %ERRORLEVEL%
)
echo ✅ Go Server compiled successfully.

echo.
echo 🚚 2. Copying binary to windowsservice directory...
copy /Y "%SERVER_DIR%\snaphaven.exe" "%SCRIPT_DIR%\snaphaven.exe"
if %ERRORLEVEL% NEQ 0 (
    echo ❌ Error: Failed to copy server binary!
    exit /b %ERRORLEVEL%
)

echo.
echo 🔨 3. Compiling NSIS Installer Package...
cd /d "%SCRIPT_DIR%"

set MAKENSIS=C:\Program Files (x86)\NSIS\makensis.exe
if exist "%MAKENSIS%" (
    "%MAKENSIS%" snaphaven.nsi
) else (
    makensis snaphaven.nsi
)

if %ERRORLEVEL% NEQ 0 (
    echo ❌ Error: NSIS build failed!
    exit /b %ERRORLEVEL%
)

echo.
echo ===================================================
echo 🎉 SUCCESS! Installer package created:
echo    %SCRIPT_DIR%SnapHavenInstaller.exe
echo ===================================================
