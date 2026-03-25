$ErrorActionPreference = "Stop"

function Log($msg) {
    Write-Host "[INFO] $msg" -ForegroundColor Cyan
}

function Success($msg) {
    Write-Host "[SUCCESS] $msg" -ForegroundColor Green
}

function ErrorMsg($msg) {
    Write-Host "[ERROR] $msg" -ForegroundColor Red
}

$rootDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$frontendDir = Join-Path $rootDir "frontend"
$backendDir = Join-Path $rootDir "backend"
$appDir = Join-Path $rootDir "app"
$appDistDir = Join-Path $appDir "dist"
$appExePath = Join-Path $appDir "ICAI.exe"
$legacyAppExePath = Join-Path $appDir "app.exe"
$backendExePath = Join-Path $backendDir "ICAI.exe"
$legacyBackendExePath = Join-Path $backendDir "backend.exe"
$iconPath = Join-Path $backendDir "icon.png"
$goWinResPath = Join-Path $env:USERPROFILE "go\bin\go-winres.exe"
$certPath = Join-Path $rootDir "certs\ICAIcert.pfx"
$goBuildLdFlags = "-s -w"
$goBuildArgs = @("build", "-trimpath", "-buildvcs=false", "-ldflags=$goBuildLdFlags", "-o", $appExePath)

$certPassword = "snayrexkrmak2002"

$signtool = "C:\Program Files (x86)\Windows Kits\10\bin\10.0.26100.0\x64\signtool.exe"

try {
    Log "Starting build process"

    if (!(Test-Path $frontendDir)) {
        throw "frontend folder not found"
    }

    if (!(Test-Path $backendDir)) {
        throw "backend folder not found"
    }

    if (!(Test-Path $iconPath)) {
        throw "Application icon not found: $iconPath"
    }

    if (!(Test-Path $certPath)) {
        throw "Certificate not found: $certPath"
    }

    if (!(Test-Path $signtool)) {
        throw "signtool.exe not found: $signtool"
    }

    if (!(Test-Path $goWinResPath)) {
        throw "go-winres.exe not found: $goWinResPath"
    }

    Log "Preparing app output folder"
    if (!(Test-Path $appDir)) {
        New-Item -ItemType Directory -Path $appDir | Out-Null
    }

    Log "Cleaning old app/dist"
    if (Test-Path $appDistDir) {
        Remove-Item -Recurse -Force $appDistDir
    }

    Log "Building frontend directly into app/dist"
    Push-Location $frontendDir
    npm run build
    if ($LASTEXITCODE -ne 0) {
        throw "Frontend build failed"
    }
    Pop-Location
    Success "Frontend build completed"

    if (!(Test-Path $appDistDir)) {
        throw "app/dist does not exist after build"
    }
    Success "app/dist prepared"

    Log "Removing old executables if they exist"
    if (Test-Path $appExePath) {
        Remove-Item -Force $appExePath
    }
    if (Test-Path $legacyAppExePath) {
        Remove-Item -Force $legacyAppExePath
    }
    if (Test-Path $backendExePath) {
        Remove-Item -Force $backendExePath
    }
    if (Test-Path $legacyBackendExePath) {
        Remove-Item -Force $legacyBackendExePath
    }

    Log "Generating Windows resources from app icon"
    Push-Location $backendDir
    & $goWinResPath simply `
        --arch amd64 `
        --icon $iconPath `
        --product-name "ICAI" `
        --file-description "ICAI desktop application" `
        --original-filename "ICAI.exe"
    if ($LASTEXITCODE -ne 0) {
        throw "Windows resource generation failed"
    }

    Log "Building Go executable to app/ICAI.exe with release flags"
    & go @goBuildArgs
    if ($LASTEXITCODE -ne 0) {
        throw "Go build failed"
    }
    Pop-Location
    Success "Executable built: $appExePath"

    Log "Signing ICAI.exe"
    & $signtool sign `
        /tr http://timestamp.digicert.com `
        /td sha256 `
        /fd sha256 `
        /f $certPath `
        /p $certPassword `
        $appExePath

    if ($LASTEXITCODE -ne 0) {
        throw "Code signing failed"
    }

    Success "Executable signed successfully"
    Success "Build process finished successfully"
    Log "Final output folder: $appDir"
}
catch {
    ErrorMsg $_.Exception.Message
    exit 1
}
