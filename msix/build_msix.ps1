param(
    [Parameter(Mandatory = $false)]
    [string]$Tag = $(
        if ($env:GITHUB_REF_NAME) {
            $env:GITHUB_REF_NAME
        } else {
            $gitTag = (git describe --tags --abbrev=0 2>$null)
            if ($gitTag) { $gitTag.Trim() } else { "v1.0.0" }
        }
    ),

    [Parameter(Mandatory = $false)]
    [string]$ExecutablePath = "$PSScriptRoot\..\server\snaphaven.exe",

    [Parameter(Mandatory = $false)]
    [string]$OutputDir = "$PSScriptRoot\dist"
)

$ErrorActionPreference = "Stop"

Write-Host "=========================================" -ForegroundColor Cyan
Write-Host " SnapHaven Server MSIX Packager" -ForegroundColor Cyan
Write-Host "=========================================" -ForegroundColor Cyan

# 1. Parse Version to 4-digit MSIX format (Major.Minor.Build.0)
$cleanVer = $Tag.Trim().TrimStart('v').TrimStart('V')
$parts = $cleanVer.Split('.')
if ($parts.Count -eq 1) {
    $msixVersion = "$($parts[0]).0.0.0"
} elseif ($parts.Count -eq 2) {
    $msixVersion = "$($parts[0]).$($parts[1]).0.0"
} elseif ($parts.Count -eq 3) {
    $msixVersion = "$($parts[0]).$($parts[1]).$($parts[2]).0"
} else {
    # Force 4th digit to 0 as required by Microsoft Store
    $msixVersion = "$($parts[0]).$($parts[1]).$($parts[2]).0"
}

Write-Host "Input Tag:    $Tag" -ForegroundColor Gray
Write-Host "MSIX Version: $msixVersion" -ForegroundColor Green

# 2. Verify Executable exists
if (-not (Test-Path $ExecutablePath)) {
    throw "Executable not found at: $ExecutablePath. Please build snaphaven.exe before packaging."
}
Write-Host "Executable:   $ExecutablePath" -ForegroundColor Gray

# 3. Locate makeappx.exe
function Find-WindowsTool($toolName) {
    $cmd = Get-Command $toolName -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    $kits = Get-ChildItem "C:\Program Files (x86)\Windows Kits\10\bin\*\x64\$toolName" -ErrorAction SilentlyContinue | Sort-Object FullName -Descending
    if ($kits -and $kits.Count -gt 0) { return $kits[0].FullName }
    $kits64 = Get-ChildItem "C:\Program Files\Windows Kits\10\bin\*\x64\$toolName" -ErrorAction SilentlyContinue | Sort-Object FullName -Descending
    if ($kits64 -and $kits64.Count -gt 0) { return $kits64[0].FullName }
    throw "Could not find $toolName in PATH or Windows Kits directory."
}

$makeappx = Find-WindowsTool "makeappx.exe"
Write-Host "makeappx.exe: $makeappx" -ForegroundColor Gray

# 4. Prepare Staging Directory
$stageDir = Join-Path $PSScriptRoot "temp_stage"
if (Test-Path $stageDir) {
    Remove-Item $stageDir -Recurse -Force
}
New-Item -ItemType Directory -Path $stageDir -Force | Out-Null

Write-Host "Staging files into: $stageDir ..." -ForegroundColor Gray

# Copy static assets and directories
Copy-Item -Path (Join-Path $PSScriptRoot "Assets") -Destination (Join-Path $stageDir "Assets") -Recurse -Force
Copy-Item -Path (Join-Path $PSScriptRoot "VFS") -Destination (Join-Path $stageDir "VFS") -Recurse -Force

if (Test-Path (Join-Path $PSScriptRoot "Registry.dat")) {
    Copy-Item -Path (Join-Path $PSScriptRoot "Registry.dat") -Destination $stageDir -Force
}
if (Test-Path (Join-Path $PSScriptRoot "User.dat")) {
    Copy-Item -Path (Join-Path $PSScriptRoot "User.dat") -Destination $stageDir -Force
}
if (Test-Path (Join-Path $PSScriptRoot "UserClasses.dat")) {
    Copy-Item -Path (Join-Path $PSScriptRoot "UserClasses.dat") -Destination $stageDir -Force
}
if (Test-Path (Join-Path $PSScriptRoot "Resources.pri")) {
    Copy-Item -Path (Join-Path $PSScriptRoot "Resources.pri") -Destination $stageDir -Force
}

# Copy snaphaven.exe
Copy-Item -Path $ExecutablePath -Destination (Join-Path $stageDir "snaphaven.exe") -Force

# Read and update AppxManifest.xml with target version
$manifestSource = Join-Path $PSScriptRoot "AppxManifest.xml"
$manifestContent = Get-Content -Path $manifestSource -Raw
$updatedManifest = [regex]::Replace($manifestContent, 'Version="[0-9.]+"', "Version=""$msixVersion""")
$stageManifest = Join-Path $stageDir "AppxManifest.xml"
Set-Content -Path $stageManifest -Value $updatedManifest -Encoding UTF8

# 5. Pack MSIX
if (-not (Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null
}

$outputPackage = Join-Path $OutputDir "SnapHavenServer-$msixVersion.msix"
if (Test-Path $outputPackage) {
    Remove-Item $outputPackage -Force
}

Write-Host "Packing MSIX package: $outputPackage ..." -ForegroundColor Yellow
& $makeappx pack /d $stageDir /p $outputPackage /o /nv

if (-not (Test-Path $outputPackage)) {
    throw "Failed to create MSIX package: $outputPackage"
}

# 6. Cleanup Staging Directory
Remove-Item $stageDir -Recurse -Force

$pkgSizeMB = [math]::Round(((Get-Item $outputPackage).Length / 1MB), 2)
Write-Host "=========================================" -ForegroundColor Cyan
Write-Host " MSIX Build Succeeded!" -ForegroundColor Green
Write-Host " Output: $outputPackage ($pkgSizeMB MB)" -ForegroundColor Green
Write-Host "=========================================" -ForegroundColor Cyan
