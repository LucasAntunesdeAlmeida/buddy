# Builds buddy.exe (with Buddy's icon, no console window) and buddysim.exe,
# lets Buddy practice so his network ships with him, and packages it all with
# the README and licenses into dist\buddy-<version>-windows-amd64.zip, with its
# SHA-256 checksum next to it.
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

go run ./tools/mknotices -out "$out\THIRD_PARTY_NOTICES.txt" ./cmd/buddy ./cmd/buddysim
if ($LASTEXITCODE -ne 0) { throw "mknotices failed" }
Copy-Item README.md, LICENSE $out

$name = "buddy-$Version-windows-amd64.zip"
$zip = "dist\$name"
if (Test-Path $zip) { Remove-Item $zip }
# File by file, because Compress-Archive and CreateFromDirectory can write
# backslashes into the paths inside a zip, as in brain\network.gob, and not
# every unzip tool reads those. The zip format calls for forward slashes.
Add-Type -AssemblyName System.IO.Compression, System.IO.Compression.FileSystem
$root = (Resolve-Path $out).Path
$archive = [IO.Compression.ZipFile]::Open((Join-Path (Get-Location).Path $zip), "Create")
try {
    Get-ChildItem $root -Recurse -File | ForEach-Object {
        $entry = $_.FullName.Substring($root.Length + 1).Replace("\", "/")
        [void][IO.Compression.ZipFileExtensions]::CreateEntryFromFile($archive, $_.FullName, $entry)
    }
} finally {
    $archive.Dispose()
}

# In the format sha256sum -c reads: LF, and two spaces before the name.
$hash = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLower()
Set-Content -Path "$zip.sha256" -Value "$hash  $name`n" -NoNewline -Encoding ascii
Write-Host "Built $zip"
