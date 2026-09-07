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
$aiAssetsDir = Join-Path $backendDir "assets\image-ai"
$aiModelPath = Join-Path $aiAssetsDir "u2net.onnx"
$detailedAIModelPath = Join-Path $aiAssetsDir "birefnet-general.onnx"
$aiRuntimePath = Join-Path $aiAssetsDir "onnxruntime.dll"
$detailedAIModelHash = "58F621F00F5D756097615970A88A791584600DCF7C45B18A0A6267535A1EBD3C"
$textAIAssetsDir = Join-Path $backendDir "assets\text-ai"
$textAIModelPath = Join-Path $textAIAssetsDir "Qwen3-4B-Q4_K_M.gguf"
$textAIServerPath = Join-Path $textAIAssetsDir "llama-server.exe"
$textAIRuntimeManifest = Join-Path $textAIAssetsDir "runtime-sha256.txt"
$textAIModelHash = "7485FE6F11AF29433BC51CAB58009521F205840F5B4AE3A32FA7F92E8534FDF5"
$appModelsDir = Join-Path $appDir "models"
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

    if (!(Test-Path $aiModelPath) -or !(Test-Path $detailedAIModelPath) -or !(Test-Path $aiRuntimePath)) {
        throw "Local AI assets are missing. Expected u2net.onnx, birefnet-general.onnx and onnxruntime.dll in backend/assets/image-ai"
    }
    if ((Get-FileHash $detailedAIModelPath -Algorithm SHA256).Hash -ne $detailedAIModelHash) {
        throw "BiRefNet model integrity check failed; download the official model again"
    }
    if (!(Test-Path $textAIModelPath) -or !(Test-Path $textAIServerPath) -or !(Test-Path $textAIRuntimeManifest)) {
        throw "Local description AI assets are missing. Expected Qwen3-4B-Q4_K_M.gguf and llama-server.exe in backend/assets/text-ai"
    }
    if ((Get-FileHash $textAIModelPath -Algorithm SHA256).Hash -ne $textAIModelHash) {
        throw "Qwen3 description model integrity check failed"
    }
    foreach ($line in Get-Content $textAIRuntimeManifest) {
        $parts = $line.Trim() -split '\s+', 2
        if ($parts.Count -ne 2) { continue }
        $runtimeFile = Join-Path $textAIAssetsDir $parts[0]
        if (!(Test-Path $runtimeFile) -or (Get-FileHash $runtimeFile -Algorithm SHA256).Hash -ne $parts[1]) {
            throw "llama.cpp runtime integrity check failed: $($parts[0])"
        }
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

    Log "Copying local AI model and CPU runtime"
    if (!(Test-Path $appModelsDir)) {
        New-Item -ItemType Directory -Path $appModelsDir | Out-Null
    }
    Copy-Item -Force $aiModelPath (Join-Path $appModelsDir "u2net.onnx")
    Copy-Item -Force $detailedAIModelPath (Join-Path $appModelsDir "birefnet-general.onnx")
    Copy-Item -Force $aiRuntimePath (Join-Path $appModelsDir "onnxruntime.dll")
    Copy-Item -Force (Join-Path $textAIAssetsDir "*") $appModelsDir
    Success "Local AI assets prepared"

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
