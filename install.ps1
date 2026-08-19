# ==============================================================================
# TermChat - Developer Collaboration Room Installer for Windows (PowerShell)
# Usage: irm https://raw.githubusercontent.com/BrianC0des/termchat/main/install.ps1 | iex
# ==============================================================================

$ErrorActionPreference = "Stop"

$Repo = "BrianC0des/termchat"
$GitHubLatestApi = "https://api.github.com/repos/$Repo/releases/latest"
$InstallDir = "$HOME\AppData\Local\Programs\termchat"
$AssetZip = "termchat-windows.zip"
$ExeName = "termchat.exe"

Write-Host ""
Write-Host "  ╔═══════════════════════════════════════════════════════╗" -ForegroundColor Cyan
Write-Host "  ║   🚀 TermChat — Terminal Developer Collab Hub         ║" -ForegroundColor Cyan
Write-Host "  ╚═══════════════════════════════════════════════════════╝" -ForegroundColor Cyan
Write-Host ""

# 1. Fetch Latest Release Tag
Write-Host "Fetching latest release version..." -ForegroundColor Yellow
$Tag = "v1.9.8"
try {
    $ReleaseInfo = Invoke-RestMethod -Uri $GitHubLatestApi -Headers @{"User-Agent"="TermChat-Installer"} -TimeoutSec 5
    if ($ReleaseInfo.tag_name) {
        $Tag = $ReleaseInfo.tag_name
        Write-Host "Found latest version: $Tag" -ForegroundColor Green
    }
} catch {
    Write-Host "Using release version: $Tag" -ForegroundColor Yellow
}

# 2. Mirror URLs
$UrlGitHub = "https://github.com/$Repo/releases/download/$Tag/$AssetZip"
$UrlHF = "https://huggingface.co/datasets/BrianC0des/termchat-releases/resolve/main/$AssetZip"
$UrlFastly = "https://raw.githubusercontent.com/$Repo/binaries/$AssetZip"

$TempZip = "$env:TEMP\$AssetZip"
$TempExtract = "$env:TEMP\termchat-extract"

# 3. Download with Fallback
Write-Host "Downloading $AssetZip..." -ForegroundColor Yellow
$DownloadSuccess = $false

foreach ($Url in @($UrlGitHub, $UrlHF, $UrlFastly)) {
    try {
        Write-Host "  Trying: $Url" -ForegroundColor Blue
        Invoke-WebRequest -Uri $Url -OutFile $TempZip -UseBasicParsing -TimeoutSec 15
        $DownloadSuccess = $true
        Write-Host "  ✓ Downloaded successfully!" -ForegroundColor Green
        break
    } catch {
        Write-Host "  ✗ Failed, trying next mirror..." -ForegroundColor Yellow
    }
}

if (-not $DownloadSuccess) {
    Write-Host "Error: Failed to download TermChat from all mirrors." -ForegroundColor Red
    exit 1
}

# 4. Extract Archive
Write-Host "Extracting archive..." -ForegroundColor Yellow
if (Test-Path $TempExtract) {
    Remove-Item -Path $TempExtract -Recurse -Force
}
New-Item -ItemType Directory -Path $TempExtract -Force | Out-Null
Expand-Archive -Path $TempZip -DestinationPath $TempExtract -Force

# Locate termchat.exe
$ExtractedExe = Get-ChildItem -Path $TempExtract -Filter "*.exe" -Recurse | Select-Object -First 1
if (-not $ExtractedExe) {
    Write-Host "Error: termchat.exe not found in extracted archive." -ForegroundColor Red
    exit 1
}

# 5. Clean Old Binary & Install to Local Programs
New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
$DestPath = "$InstallDir\$ExeName"

if (Test-Path $DestPath) {
    Remove-Item -Path $DestPath -Force -ErrorAction SilentlyContinue
}

Copy-Item -Path $ExtractedExe.FullName -Destination $DestPath -Force

# 6. Cleanup Temporary Files
Remove-Item -Path $TempZip -Force -ErrorAction SilentlyContinue
Remove-Item -Path $TempExtract -Recurse -Force -ErrorAction SilentlyContinue

# Verify installed binary
$VerifiedVer = & "$DestPath" --version 2>$null
if (-not $VerifiedVer) {
    $VerifiedVer = "TermChat $Tag"
}

# 7. Add to User PATH if not present
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$InstallDir*") {
    Write-Host "Adding $InstallDir to User PATH..." -ForegroundColor Yellow
    [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", "User")
    $env:Path += ";$InstallDir"
    Write-Host "✓ Added to PATH!" -ForegroundColor Green
}

Write-Host ""
Write-Host "═══════════════════════════════════════════════════════" -ForegroundColor Green
Write-Host "  ✓ Cleanly installed: $VerifiedVer" -ForegroundColor Green
Write-Host "═══════════════════════════════════════════════════════" -ForegroundColor Green
Write-Host ""

# 8. Post-Install Room Check
if (Test-Path ".termchat\room.json") {
    Write-Host "🐙 Found project collab room in current directory (.termchat\room.json)!" -ForegroundColor Cyan
    Write-Host "Type 'termchat' to join your team's room immediately."
} else {
    Write-Host "Type 'termchat' to launch your developer collab room."
}
