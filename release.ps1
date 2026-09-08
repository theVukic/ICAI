[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^\d+\.\d+\.\d+$')]
    [string]$Version,
    [string]$ReleaseRepo = 'theVukic/ASI-Releases',
    [string]$ReleaseNotes,
    [string]$ReleaseNotesFile,
    [switch]$DryRun,
    [switch]$SkipSign
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$backend = Join-Path $root 'backend'
$stage = Join-Path $root 'app'
$out = Join-Path $root 'release\out'
$installerDir = Join-Path $root 'installer'
$generated = Join-Path $installerDir 'generated.iss'
$generatedDownloads = Join-Path $installerDir 'generated-model-downloads.iss'
$generatedCopies = Join-Path $installerDir 'generated-model-copies.iss'
$icon = Join-Path $backend 'icon.png'
$tag = "v$Version"

function Info([string]$Message) { Write-Host "[ASI] $Message" -ForegroundColor Cyan }
function Require([string]$Command) { if (-not (Get-Command $Command -ErrorAction SilentlyContinue)) { throw "$Command nije pronađen." } }
function HashOf([string]$Path) { (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant() }

function Ensure-GitHubLogin {
    & gh auth status 2>$null
    if ($LASTEXITCODE -ne 0) {
        Info 'Potrebna je GitHub prijava; otvaram browser.'
        & gh auth login -h github.com -w
        if ($LASTEXITCODE -ne 0) { throw 'GitHub prijava nije uspjela.' }
    }
}

function Ensure-InnoSetup {
    $compiler = Get-Command iscc.exe -ErrorAction SilentlyContinue
    if ($compiler) { return $compiler.Source }
    $candidates = @(
        'C:\Program Files\Inno Setup 7\ISCC.exe',
        'C:\Program Files (x86)\Inno Setup 7\ISCC.exe',
        (Join-Path $env:LOCALAPPDATA 'Programs\Inno Setup 7\ISCC.exe'),
        (Join-Path $env:LOCALAPPDATA 'Inno Setup 7\ISCC.exe')
    )
    foreach ($candidate in $candidates) {
        if (Test-Path -LiteralPath $candidate) { return $candidate }
    }
    $download = Join-Path ([System.IO.Path]::GetTempPath()) 'innosetup-7.1.0-x64.exe'
    Info 'Preuzimam Inno Setup 7.1.0.'
    Invoke-WebRequest -Uri 'https://github.com/jrsoftware/issrc/releases/download/is-7_1_0/innosetup-7.1.0-x64.exe' -OutFile $download
    $signature = Get-AuthenticodeSignature -FilePath $download
    if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notmatch 'Pyrsys') { throw 'Inno Setup potpis nije validan.' }
    $process = Start-Process -FilePath $download -ArgumentList '/VERYSILENT','/SUPPRESSMSGBOXES','/NORESTART' -Wait -PassThru
    if ($process.ExitCode -ne 0) { throw "Inno Setup instalacija nije uspjela (exit code $($process.ExitCode))." }
    foreach ($candidate in $candidates) {
        if (Test-Path -LiteralPath $candidate) { return $candidate }
    }
    throw 'Inno Setup compiler nije pronađen nakon instalacije. Instalirajte Inno Setup 7 pa ponovite release.'
}

function Choose-Content {
    Write-Host ''
    Write-Host '1. Full (program, frontend, svi modeli, runtime, defaults)'
    Write-Host '2. Custom (program/frontend su obavezni; birate AI modele)'
    $choice = Read-Host 'Odaberite [1]'
    $content = [ordered]@{ ImageAI = $true; TextAI = $true; Defaults = $true }
    if ($choice -eq '2') {
        $content.ImageAI = (Read-Host 'Uključi Image AI modele? [Y/n]') -notmatch '^[nN]'
        $content.TextAI = (Read-Host 'Uključi Text AI model? [Y/n]') -notmatch '^[nN]'
        $content.Defaults = (Read-Host 'Uključi početne resurse? [Y/n]') -notmatch '^[nN]'
    }
    return $content
}

function Choose-ExtraContent([string]$ExtrasRoot) {
    Add-Type -AssemblyName System.Windows.Forms
    New-Item -ItemType Directory -Force -Path $ExtrasRoot | Out-Null
    while ($true) {
        Write-Host ''
        Write-Host 'Dodatni sadržaj: [F] fajl, [D] folder, [Enter] završi'
        $action = Read-Host 'Izbor'
        if ([string]::IsNullOrWhiteSpace($action)) { break }
        $source = $null
        if ($action -match '^[Ff]') {
            $dialog = New-Object System.Windows.Forms.OpenFileDialog
            $dialog.Multiselect = $true
            if ($dialog.ShowDialog() -ne [System.Windows.Forms.DialogResult]::OK) { continue }
            foreach ($file in $dialog.FileNames) {
                $destination = Read-Host "Ciljni folder u aplikaciji za $([IO.Path]::GetFileName($file)) [extensions]"
                if ([string]::IsNullOrWhiteSpace($destination)) { $destination = 'extensions' }
                if ([IO.Path]::IsPathRooted($destination) -or $destination -match '(^|[\\/])\.\.([\\/]|$)') { throw 'Ciljna putanja mora biti relativna i ne smije sadržati ..' }
                $targetDir = Join-Path $ExtrasRoot $destination
                New-Item -ItemType Directory -Force -Path $targetDir | Out-Null
                Copy-Item -LiteralPath $file -Destination $targetDir -Force
            }
        } elseif ($action -match '^[Dd]') {
            $dialog = New-Object System.Windows.Forms.FolderBrowserDialog
            if ($dialog.ShowDialog() -ne [System.Windows.Forms.DialogResult]::OK) { continue }
            $destination = Read-Host "Ciljni folder u aplikaciji [$([IO.Path]::GetFileName($dialog.SelectedPath))]"
            if ([string]::IsNullOrWhiteSpace($destination)) { $destination = [IO.Path]::GetFileName($dialog.SelectedPath) }
            if ([IO.Path]::IsPathRooted($destination) -or $destination -match '(^|[\\/])\.\.([\\/]|$)') { throw 'Ciljna putanja mora biti relativna i ne smije sadržati ..' }
            Copy-Item -LiteralPath $dialog.SelectedPath -Destination (Join-Path $ExtrasRoot $destination) -Recurse -Force
            $gitMetadata = Join-Path (Join-Path $ExtrasRoot $destination) '.git'
            if (Test-Path -LiteralPath $gitMetadata) { Remove-Item -LiteralPath $gitMetadata -Recurse -Force }
        }
    }
}

Require git; Require go; Require npm; Require gh; Require 7z
$iscc = Ensure-InnoSetup
$content = Choose-Content
$extrasRoot = Join-Path $out 'extras'
if (Test-Path -LiteralPath $out) { Remove-Item -LiteralPath $out -Recurse -Force }
New-Item -ItemType Directory -Force -Path $out | Out-Null
Choose-ExtraContent $extrasRoot
if ([string]::IsNullOrWhiteSpace($ReleaseNotes) -and -not [string]::IsNullOrWhiteSpace($ReleaseNotesFile)) { $ReleaseNotes = Get-Content -Raw -LiteralPath $ReleaseNotesFile }
if ([string]::IsNullOrWhiteSpace($ReleaseNotes)) { $ReleaseNotes = Read-Host 'Release poruka za korisnike (npr. Ispravljen login bug)' }
if ([string]::IsNullOrWhiteSpace($ReleaseNotes)) { $ReleaseNotes = "ASI $tag" }

if (-not $DryRun) { Ensure-GitHubLogin }
Info "Buildam ASI v$Version"
Push-Location $backend
go test ./...
Pop-Location
# A dry run creates a local test installer only; it must never require the
# private signing certificate or its password. Keep this explicit instead of
# splatting a switch, because PowerShell can treat a trailing array value as a
# positional argument when invoking another .ps1 script.
if ($SkipSign -or $DryRun) {
    & (Join-Path $root 'build.ps1') -Version $Version -UpdateManifestURL "https://github.com/$ReleaseRepo/releases/latest/download/latest.json" -SkipSign
} else {
    & (Join-Path $root 'build.ps1') -Version $Version -UpdateManifestURL "https://github.com/$ReleaseRepo/releases/latest/download/latest.json"
}
if ($LASTEXITCODE -ne 0) { throw 'Build nije uspio.' }

New-Item -ItemType Directory -Force -Path $out | Out-Null
$assets = New-Object System.Collections.Generic.List[string]
Copy-Item (Join-Path $stage 'ASI.exe') $out -Force
Copy-Item (Join-Path $stage 'ASI-Updater.exe') $out -Force
& 7z a -tzip (Join-Path $out "ASI-frontend-$tag.zip") (Join-Path $stage 'dist\*') | Out-Null
$assets.Add((Join-Path $out 'ASI.exe')); $assets.Add((Join-Path $out 'ASI-Updater.exe')); $assets.Add((Join-Path $out "ASI-frontend-$tag.zip"))

$modelDownloads = New-Object System.Collections.Generic.List[string]
$modelCopies = New-Object System.Collections.Generic.List[string]
$qwenPartCount = 0
$qwenHash = ''
function Add-Model([string]$Id, [string]$Source, [string]$TargetName) {
    if (-not (Test-Path $Source)) { throw "Model nedostaje: $Source" }
    Copy-Item $Source (Join-Path $out $TargetName) -Force
    $assets.Add((Join-Path $out $TargetName))
    $hash = HashOf (Join-Path $out $TargetName)
    $url = "https://github.com/$ReleaseRepo/releases/download/$tag/$TargetName"
    $modelDownloads.Add("  DownloadPage.Add('$url', '$TargetName', '$hash');")
    $modelCopies.Add("  CopyFile(ExpandConstant('{tmp}\\$TargetName'), ExpandConstant('{app}\\models\\$TargetName'), False);")
}
if ($content.ImageAI) {
    Add-Model 'u2net' (Join-Path $backend 'assets\image-ai\u2net.onnx') 'u2net.onnx'
    Add-Model 'birefnet' (Join-Path $backend 'assets\image-ai\birefnet-general.onnx') 'birefnet-general.onnx'
}
if ($content.TextAI) {
    $qwen = Join-Path $backend 'assets\text-ai\Qwen3-4B-Q4_K_M.gguf'
    if (-not (Test-Path $qwen)) { throw 'Qwen model nedostaje.' }
    $qwenHash = HashOf $qwen
    $partSize = 1536MB
    $buffer = New-Object byte[] 1048576
    $input = [System.IO.File]::OpenRead($qwen)
    try {
        $part = 1; $written = 0; $output = $null
        while (($read = $input.Read($buffer, 0, $buffer.Length)) -gt 0) {
            if ($null -eq $output -or ($written + $read) -gt $partSize) {
                if ($output) { $output.Dispose() }
                $name = ('Qwen3-4B-Q4_K_M.part{0:d3}' -f $part)
                $output = [System.IO.File]::Open((Join-Path $out $name), [System.IO.FileMode]::Create, [System.IO.FileAccess]::Write)
                $assets.Add((Join-Path $out $name))
                $hash = $null
                $part += 1; $written = 0
            }
            $output.Write($buffer, 0, $read); $written += $read
        }
        if ($output) { $output.Dispose() }
        $qwenPartCount = $part - 1
    } finally { $input.Dispose() }
    for ($index = 1; $index -le $qwenPartCount; $index++) {
        $name = ('Qwen3-4B-Q4_K_M.part{0:d3}' -f $index)
        $modelDownloads.Add("  DownloadPage.Add('https://github.com/$ReleaseRepo/releases/download/$tag/$name', '$name', '$(HashOf (Join-Path $out $name))');")
    }
}

$modelDownloads | Set-Content -LiteralPath $generatedDownloads -Encoding utf8
$modelCopies | Set-Content -LiteralPath $generatedCopies -Encoding utf8
$skipModelDownload = if ($DryRun) { 1 } else { 0 }
@"
#define AppVersion "$Version"
#define OutputDir "$out"
#define StageDir "$stage"
#define ExtrasDir "$extrasRoot"
#define SkipModelDownload $skipModelDownload
#define QwenPartCount $qwenPartCount
#define QwenHash "$qwenHash"
"@ | Set-Content -LiteralPath $generated -Encoding utf8

Info 'Pravim ASI-Setup.exe'
& $iscc (Join-Path $installerDir 'ASI.iss')
if ($LASTEXITCODE -ne 0) { throw 'Installer build nije uspio.' }
$setup = Join-Path $out "ASI-Setup-v$Version.exe"
$assets.Add($setup)

if ($DryRun) {
    Info "Dry run gotov. Installer: $setup"
    Info 'Napomena: dry-run setup namjerno preskače AI modele; za instalaciju sa svim modelima objavite pravi release bez -DryRun.'
    exit 0
}

& gh repo view $ReleaseRepo 2>$null
if ($LASTEXITCODE -ne 0) { & gh repo create $ReleaseRepo --public --add-readme }
Info 'Objavljujem draft release.'
if ((Get-ChildItem $extrasRoot -Recurse -File -ErrorAction SilentlyContinue).Count -gt 0) {
    & 7z a -tzip (Join-Path $out "ASI-extras-$tag.zip") (Join-Path $extrasRoot '*') | Out-Null
    $assets.Add((Join-Path $out "ASI-extras-$tag.zip"))
}
& gh release create $tag --repo $ReleaseRepo --draft --title "ASI $tag" --notes $ReleaseNotes $assets.ToArray()
if ($LASTEXITCODE -ne 0) { throw 'GitHub draft release nije napravljen.' }

$components = @(
    @{ id = 'app'; url = "https://github.com/$ReleaseRepo/releases/download/$tag/ASI.exe"; sha256 = HashOf (Join-Path $out 'ASI.exe'); size = (Get-Item (Join-Path $out 'ASI.exe')).Length },
    @{ id = 'updater'; url = "https://github.com/$ReleaseRepo/releases/download/$tag/ASI-Updater.exe"; sha256 = HashOf (Join-Path $out 'ASI-Updater.exe'); size = (Get-Item (Join-Path $out 'ASI-Updater.exe')).Length },
    @{ id = 'frontend'; url = "https://github.com/$ReleaseRepo/releases/download/$tag/ASI-frontend-$tag.zip"; sha256 = HashOf (Join-Path $out "ASI-frontend-$tag.zip"); size = (Get-Item (Join-Path $out "ASI-frontend-$tag.zip")).Length }
)
if (Test-Path (Join-Path $out "ASI-extras-$tag.zip")) { $components += @{ id = 'extras'; url = "https://github.com/$ReleaseRepo/releases/download/$tag/ASI-extras-$tag.zip"; sha256 = HashOf (Join-Path $out "ASI-extras-$tag.zip"); size = (Get-Item (Join-Path $out "ASI-extras-$tag.zip")).Length } }
if ($content.ImageAI) {
    $components += @{ id = 'image-model-u2net'; url = "https://github.com/$ReleaseRepo/releases/download/$tag/u2net.onnx"; sha256 = HashOf (Join-Path $out 'u2net.onnx'); size = (Get-Item (Join-Path $out 'u2net.onnx')).Length }
    $components += @{ id = 'image-model-birefnet'; url = "https://github.com/$ReleaseRepo/releases/download/$tag/birefnet-general.onnx"; sha256 = HashOf (Join-Path $out 'birefnet-general.onnx'); size = (Get-Item (Join-Path $out 'birefnet-general.onnx')).Length }
}
@{ version = $Version; notes = $ReleaseNotes; components = $components } | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $out 'latest.json') -Encoding utf8
& gh release upload $tag (Join-Path $out 'latest.json') --repo $ReleaseRepo
& gh release edit $tag --draft=false --latest --repo $ReleaseRepo
git tag $tag; git push origin HEAD; git push origin $tag
Info "Objavljeno: https://github.com/$ReleaseRepo/releases/tag/$tag"
