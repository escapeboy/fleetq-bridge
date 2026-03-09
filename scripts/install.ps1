# FleetQ Bridge installer for Windows
# Usage: iwr https://get.fleetq.net/bridge/windows | iex

$ErrorActionPreference = "Stop"
$Repo = "escapeboy/fleetq-bridge"
$Binary = "fleetq-bridge.exe"
$InstallDir = "$env:LOCALAPPDATA\FleetQ"

Write-Host "==> Fetching latest version..." -ForegroundColor Green

$latest = (Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest").tag_name
if (-not $latest) {
    Write-Error "Could not determine latest version."
    exit 1
}

Write-Host "==> Latest version: $latest" -ForegroundColor Green

$url = "https://github.com/$Repo/releases/download/$latest/fleetq-bridge_windows_amd64.exe"
$tmp = [System.IO.Path]::GetTempFileName() + ".exe"

Write-Host "==> Downloading fleetq-bridge_windows_amd64.exe..." -ForegroundColor Green
Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing

if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir | Out-Null
}

Move-Item -Force $tmp "$InstallDir\$Binary"

# Add to PATH if not already there
$currentPath = [Environment]::GetEnvironmentVariable("PATH", "User")
if ($currentPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("PATH", "$currentPath;$InstallDir", "User")
    Write-Host "==> Added $InstallDir to PATH" -ForegroundColor Green
    Write-Host "    Restart your terminal for PATH changes to take effect." -ForegroundColor Yellow
}

Write-Host ""
Write-Host "FleetQ Bridge $latest installed to $InstallDir\$Binary" -ForegroundColor Green
Write-Host ""
Write-Host "Next steps:"
Write-Host "  1. Get your API key from https://fleetq.net/team (AI Keys tab)"
Write-Host "  2. Run: fleetq-bridge login --api-key flq_team_..."
Write-Host "  3. Run: fleetq-bridge install   (auto-start on login)"
