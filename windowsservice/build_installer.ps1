# PhotoSync One-Shot Build Script
$ErrorActionPreference = "Stop"

$scriptDir = $PSScriptRoot
$serverDir = Join-Path $scriptDir "..\server"

Write-Host "===================================================" -ForegroundColor Cyan
Write-Host "🛠️  PhotoSync Server & Installer Build Script" -ForegroundColor Cyan
Write-Host "===================================================" -ForegroundColor Cyan

Write-Host "`n📦 1. Compiling Go Server Executable..." -ForegroundColor Yellow
Set-Location $serverDir
go build -o photosync_server.exe .
Write-Host "✅ Go Server compiled successfully." -ForegroundColor Green

Write-Host "`n🚚 2. Copying binary to windowsservice directory..." -ForegroundColor Yellow
$serverBinary = Join-Path $serverDir "photosync_server.exe"
$targetBinary = Join-Path $scriptDir "photosync_server.exe"
Copy-Item -Path $serverBinary -Destination $targetBinary -Force

Write-Host "`n🔨 3. Compiling NSIS Installer Package..." -ForegroundColor Yellow
Set-Location $scriptDir

$nsisCompiler = "C:\Program Files (x86)\NSIS\makensis.exe"
if (-not (Test-Path $nsisCompiler)) {
    $nsisCmd = Get-Command makensis -ErrorAction SilentlyContinue
    if ($nsisCmd) {
        $nsisCompiler = "makensis"
    } else {
        throw "NSIS Compiler (makensis.exe) was not found on this machine."
    }
}

& $nsisCompiler filesync.nsi

Write-Host "`n===================================================" -ForegroundColor Cyan
Write-Host "🎉 SUCCESS! Installer package created:" -ForegroundColor Green
Write-Host "   $(Join-Path $scriptDir 'PhotoSyncInstaller.exe')" -ForegroundColor Green
Write-Host "===================================================" -ForegroundColor Cyan
