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

function Sign-AppExecutable($path) {
    $maxAttempts = 5

    for ($attempt = 1; $attempt -le $maxAttempts; $attempt++) {
        $signOutput = @(& $signtool sign `
            /tr http://timestamp.digicert.com `
            /td sha256 `
            /fd sha256 `
            /f $certPath `
            /p $certPassword `
            $path 2>&1)
        $exitCode = $LASTEXITCODE
        $signMessage = ($signOutput | Out-String).Trim()

        if ($signMessage) {
            Write-Host $signMessage
        }

        if ($exitCode -eq 0) {
            return
        }

        if ($signMessage -notmatch "being used by another process") {
            throw "Code signing failed: $signMessage"
        }

        if ($attempt -eq $maxAttempts) {
            throw "Code signing failed after $maxAttempts attempts: $signMessage"
        }

        $delaySeconds = $attempt * 2
        Log "ASI.exe is temporarily locked; retrying code signing in $delaySeconds seconds (attempt $($attempt + 1)/$maxAttempts)"
        Start-Sleep -Seconds $delaySeconds
    }
}

$rootDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$frontendDir = Join-Path $rootDir "frontend"
$backendDir = Join-Path $rootDir "backend"
$appDir = Join-Path $rootDir "app"
$appDistDir = Join-Path $appDir "dist"
$appExePath = Join-Path $appDir "ASI.exe"
$legacyAppExePath = Join-Path $appDir "app.exe"
$previousAppExePath = Join-Path $appDir "ICAI.exe"
$backendExePath = Join-Path $backendDir "ASI.exe"
$legacyBackendExePath = Join-Path $backendDir "backend.exe"
$previousBackendExePath = Join-Path $backendDir "ICAI.exe"
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
    if (Test-Path $previousAppExePath) {
        Remove-Item -Force $previousAppExePath
    }
    if (Test-Path $backendExePath) {
        Remove-Item -Force $backendExePath
    }
    if (Test-Path $legacyBackendExePath) {
        Remove-Item -Force $legacyBackendExePath
    }
    if (Test-Path $previousBackendExePath) {
        Remove-Item -Force $previousBackendExePath
    }

    Log "Generating Windows resources from app icon"
    Push-Location $backendDir
    & $goWinResPath simply `
        --arch amd64 `
        --icon $iconPath `
        --product-name "ASI" `
        --file-description "ASI desktop application" `
        --original-filename "ASI.exe"
    if ($LASTEXITCODE -ne 0) {
        throw "Windows resource generation failed"
    }

    Log "Building Go executable to app/ASI.exe with release flags"
    & go @goBuildArgs
    if ($LASTEXITCODE -ne 0) {
        throw "Go build failed"
    }
    Pop-Location
    Success "Executable built: $appExePath"

    Log "Signing ASI.exe"
    Sign-AppExecutable $appExePath

    Success "Executable signed successfully"
    Success "Build process finished successfully"
    Log "Final output folder: $appDir"
}
catch {
    ErrorMsg $_.Exception.Message
    exit 1
}
