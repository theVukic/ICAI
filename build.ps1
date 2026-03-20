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
$backendDistDir = Join-Path $backendDir "dist"
$appDistDir = Join-Path $appDir "dist"
$appExePath = Join-Path $appDir "app.exe"
$certPath = Join-Path $rootDir "certs\ICAIcert.pfx"

# PROMIJENI OVO
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

    if (!(Test-Path $certPath)) {
        throw "Certificate not found: $certPath"
    }

    if (!(Test-Path $signtool)) {
        throw "signtool.exe not found: $signtool"
    }

    Log "Preparing app output folder"
    if (!(Test-Path $appDir)) {
        New-Item -ItemType Directory -Path $appDir | Out-Null
    }

    Log "Building frontend"
    Push-Location $frontendDir

    npm run build
    if ($LASTEXITCODE -ne 0) {
        throw "Frontend build failed"
    }

    Pop-Location
    Success "Frontend build completed"

    $frontendDistDir = Join-Path $frontendDir "dist"
    if (!(Test-Path $frontendDistDir)) {
        throw "frontend/dist does not exist after build"
    }

    Log "Preparing backend/dist for Go embed"
    if (Test-Path $backendDistDir) {
        Remove-Item -Recurse -Force $backendDistDir
    }
    New-Item -ItemType Directory -Path $backendDistDir | Out-Null

    Log "Copying frontend/dist to backend/dist"
    Copy-Item -Recurse "$frontendDistDir\*" $backendDistDir
    Success "backend/dist prepared"

    Log "Preparing app/dist output"
    if (Test-Path $appDistDir) {
        Remove-Item -Recurse -Force $appDistDir
    }
    New-Item -ItemType Directory -Path $appDistDir | Out-Null

    Log "Copying frontend/dist to app/dist"
    Copy-Item -Recurse "$frontendDistDir\*" $appDistDir
    Success "app/dist prepared"

    Log "Building Go executable to app/app.exe"
    Push-Location $backendDir

    go build -ldflags="-s -w" -o $appExePath
    if ($LASTEXITCODE -ne 0) {
        throw "Go build failed"
    }

    Pop-Location
    Success "Executable built: $appExePath"

    Log "Signing app.exe"
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
    ErrorMsg $_
    exit 1
}