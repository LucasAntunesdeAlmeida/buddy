# Builds buddy.exe (with Buddy's icon, no console window) and buddysim.exe,
# lets Buddy practice so his network ships with him, and packages it all with
# the README and license into dist\buddy-<version>-windows-amd64.zip.
#
#   .\scripts\build.ps1               # version from git describe
#   .\scripts\build.ps1 -Version 1.0.0
param([string]$Version = "")

$ErrorActionPreference = "Stop"
Set-Location (Split-Path $PSScriptRoot -Parent)

if (-not $Version) {
    $Version = git describe --tags --always --dirty 2>$null
    if (-not $Version) { $Version = "dev" }
}

go run ./tools/mkres -version $Version -out cmd/buddy/rsrc_windows_amd64.syso
if ($LASTEXITCODE -ne 0) { throw "mkres failed" }

$out = "dist\buddy-$Version"
if (Test-Path $out) { Remove-Item -Recurse -Force $out }
New-Item -ItemType Directory -Force $out | Out-Null
go build -trimpath -ldflags "-s -w -H=windowsgui" -o "$out\buddy.exe" ./cmd/buddy
if ($LASTEXITCODE -ne 0) { throw "building buddy failed" }
go build -trimpath -ldflags "-s -w" -o "$out\buddysim.exe" ./cmd/buddysim
if ($LASTEXITCODE -ne 0) { throw "building buddysim failed" }

# Buddy copies these into his data folder the first time he runs.
& "$out\buddysim.exe" -data "$out\brain"
if ($LASTEXITCODE -ne 0) { throw "training failed" }

Copy-Item README.md, LICENSE $out

$zip = "dist\buddy-$Version-windows-amd64.zip"
Compress-Archive -Path "$out\*" -DestinationPath $zip -Force
Write-Host "Built $zip"
