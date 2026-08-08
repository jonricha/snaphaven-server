# SnapHaven One-Shot Build Script
$ErrorActionPreference = "Stop"

$scriptDir = $PSScriptRoot
$serverDir = Join-Path $scriptDir "..\server"

Write-Host "===================================================" -ForegroundColor Cyan
Write-Host "🛠️  SnapHaven Server & Installer Build Script" -ForegroundColor Cyan
Write-Host "===================================================" -ForegroundColor Cyan

Write-Host "`n📦 1. Compiling Go Server Executable..." -ForegroundColor Yellow
Set-Location $serverDir
$ver = if ($env:VERSION) { $env:VERSION } else { "v1.0.0-dev" }
$buildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-dd HH:mm:ss UTC")
$commitHash = try { git rev-parse --short HEAD } catch { "none" }
go build -ldflags "-X 'main.Version=$ver' -X 'main.Commit=$commitHash' -X 'main.BuildTime=$buildDate' -H=windowsgui" -o snaphaven.exe .
Write-Host "✅ Go Server ($ver) compiled successfully." -ForegroundColor Green

Write-Host "`n🚚 2. Copying binary to windowsservice directory..." -ForegroundColor Yellow
$serverBinary = Join-Path $serverDir "snaphaven.exe"
$targetBinary = Join-Path $scriptDir "snaphaven.exe"
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

& $nsisCompiler snaphaven.nsi

Write-Host "`n===================================================" -ForegroundColor Cyan
Write-Host "🎉 SUCCESS! Installer package created:" -ForegroundColor Green
Write-Host "   $(Join-Path $scriptDir 'SnapHavenInstaller.exe')" -ForegroundColor Green
Write-Host "===================================================" -ForegroundColor Cyan
