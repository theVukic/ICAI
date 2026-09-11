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
$generatedUpdate = Join-Path $installerDir 'generated-update.iss'
$generatedUpdateDownloads = Join-Path $installerDir 'generated-update-model-downloads.iss'
$generatedUpdateCopies = Join-Path $installerDir 'generated-update-model-copies.iss'
$updateResultFile = Join-Path $out 'update-result.json'
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

function Get-VisualCppRedistributable([string]$Destination) {
    # ONNX Runtime is linked against the Microsoft Visual C++ runtime. A clean
    # Windows VM does not necessarily include it, even when onnxruntime.dll is
    # present beside ASI.exe.
    Info 'Preuzimam Microsoft Visual C++ Runtime (x64) za installer.'
    Invoke-WebRequest -Uri 'https://aka.ms/vc14/vc_redist.x64.exe' -OutFile $Destination
    $signature = Get-AuthenticodeSignature -FilePath $Destination
    if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notmatch 'Microsoft Corporation') {
        Remove-Item -LiteralPath $Destination -Force -ErrorAction SilentlyContinue
        throw 'Microsoft Visual C++ Runtime potpis nije validan.'
    }
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

# Extra folders are product content, not content belonging only to the release
# in which they were first selected. Restore the newest cumulative extras
# archive before asking for additional files, so every later full setup keeps
# e.g. an AS extension folder that was selected for v0.0.1.
function Restore-PreviousExtras([string]$ExtrasRoot) {
    $history = & gh api "repos/$ReleaseRepo/releases?per_page=100" 2>$null | ConvertFrom-Json
    if ($LASTEXITCODE -ne 0) {
        if ($DryRun) {
            Info 'Dry-run: prethodni dodatni sadržaj nije dostupan bez GitHub veze.'
            return
        }
        throw 'Nije moguće provjeriti ranije dodatne fajlove na GitHub releaseu.'
    }
    foreach ($release in $history) {
        $asset = @($release.assets | Where-Object { [string]$_.name -like 'ASI-extras-*.zip' }) | Select-Object -First 1
        if ($null -eq $asset) { continue }

        $temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("asi-extras-" + [guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Force -Path $temporaryDirectory | Out-Null
        try {
            Info "Preuzimam dodatni sadržaj iz ranijeg releasea $($release.tag_name)."
            & gh release download ([string]$release.tag_name) --repo $ReleaseRepo --pattern ([string]$asset.name) --dir $temporaryDirectory
            if ($LASTEXITCODE -ne 0) { throw "Dodatni sadržaj iz releasea $($release.tag_name) nije preuzet." }
            $archive = Join-Path $temporaryDirectory ([string]$asset.name)
            if (-not (Test-Path -LiteralPath $archive)) { throw "Nedostaje preuzeti dodatni sadržaj: $($asset.name)" }
            & 7z x $archive ("-o$ExtrasRoot") '-y' | Out-Null
            if ($LASTEXITCODE -ne 0) { throw "Dodatni sadržaj iz releasea $($release.tag_name) nije moguće raspakovati." }
        } finally {
            if (Test-Path -LiteralPath $temporaryDirectory) { Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force }
        }
        return
    }
    Info 'Nema ranijeg dodatnog sadržaja za nasljeđivanje.'
}

Require git; Require go; Require npm; Require gh; Require 7z
$iscc = Ensure-InnoSetup
$content = Choose-Content
$extrasRoot = Join-Path $out 'extras'
if (Test-Path -LiteralPath $out) { Remove-Item -LiteralPath $out -Recurse -Force }
New-Item -ItemType Directory -Force -Path $out | Out-Null
if (-not $DryRun) { Ensure-GitHubLogin }
Restore-PreviousExtras $extrasRoot
Choose-ExtraContent $extrasRoot
$vcRedist = Join-Path $out 'vc_redist.x64.exe'
Get-VisualCppRedistributable $vcRedist
if ([string]::IsNullOrWhiteSpace($ReleaseNotes) -and -not [string]::IsNullOrWhiteSpace($ReleaseNotesFile)) { $ReleaseNotes = Get-Content -Raw -LiteralPath $ReleaseNotesFile }
if ([string]::IsNullOrWhiteSpace($ReleaseNotes)) { $ReleaseNotes = Read-Host 'Release poruka za korisnike (npr. Ispravljen login bug)' }
if ([string]::IsNullOrWhiteSpace($ReleaseNotes)) { $ReleaseNotes = "ASI $tag" }

$previousTag = ''
$previousAssets = @{}
$previousModelAssets = @{}
$historicalModelAssets = @{}
if (-not $DryRun) {
    # GitHub exposes a SHA-256 digest for release assets. Keep unchanged large
    # models in their old release instead of uploading gigabytes again.
    try {
        $previousRelease = (& gh release view --repo $ReleaseRepo --json tagName,assets 2>$null | ConvertFrom-Json)
        $previousTag = [string]$previousRelease.tagName
        foreach ($asset in $previousRelease.assets) { $previousAssets[[string]$asset.name] = $asset }
        # A model can live in v0.0.1 while v0.0.2 only references it. Build a
        # small hash index across releases so later patches never re-upload it.
        $releaseHistory = (& gh api "repos/$ReleaseRepo/releases?per_page=100" 2>$null | ConvertFrom-Json)
        foreach ($release in $releaseHistory) {
            foreach ($asset in $release.assets) {
                $digest = ([string]$asset.digest).ToLowerInvariant()
                if ($digest.StartsWith('sha256:')) {
                    $historicalModelAssets[([string]$asset.name + '|' + $digest)] = "https://github.com/$ReleaseRepo/releases/download/$($release.tag_name)/$($asset.name)"
                }
            }
        }
        $indexDir = Join-Path ([IO.Path]::GetTempPath()) ("asi-model-index-" + [guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Force -Path $indexDir | Out-Null
        & gh release download $previousTag --repo $ReleaseRepo --pattern 'models-index.json' --dir $indexDir 2>$null
        $indexPath = Join-Path $indexDir 'models-index.json'
        if (Test-Path -LiteralPath $indexPath) {
            $previousIndex = Get-Content -Raw -LiteralPath $indexPath | ConvertFrom-Json
            foreach ($model in $previousIndex.models) { $previousModelAssets[[string]$model.name] = $model }
        }
        Info "Prethodni release za provjeru asseta: $previousTag"
    } catch {
        Info 'Nema prethodnog release indeksa; modeli će biti poslani prvi put.'
    }
}

function Reuse-PreviousAssetUrl([string]$Name, [string]$Hash) {
    if ($previousModelAssets.ContainsKey($Name)) {
        $indexed = $previousModelAssets[$Name]
        if (([string]$indexed.sha256).ToLowerInvariant() -eq $Hash.ToLowerInvariant()) { return [string]$indexed.url }
    }
    if (-not [string]::IsNullOrWhiteSpace($previousTag) -and $previousAssets.ContainsKey($Name)) {
        $digest = ([string]$previousAssets[$Name].digest).ToLowerInvariant()
        if ($digest -eq ('sha256:' + $Hash).ToLowerInvariant()) { return "https://github.com/$ReleaseRepo/releases/download/$previousTag/$Name" }
    }
    $historyKey = $Name + '|sha256:' + $Hash.ToLowerInvariant()
    if ($historicalModelAssets.ContainsKey($historyKey)) { return [string]$historicalModelAssets[$historyKey] }
    return ''
}

Info "Buildam ASI v$Version"
Push-Location $backend
go test ./...
$testsExitCode = $LASTEXITCODE
Pop-Location
if ($testsExitCode -ne 0) { throw 'Automatski testovi nisu prošli. Release je zaustavljen prije builda.' }
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
$updateModelDownloads = New-Object System.Collections.Generic.List[string]
$updateModelCopies = New-Object System.Collections.Generic.List[string]
$modelComponents = New-Object System.Collections.Generic.List[object]
$modelIndexEntries = New-Object System.Collections.Generic.List[object]
$qwenPartCount = 0
$qwenHash = ''
function Add-Model([string]$Id, [string]$Source, [string]$TargetName) {
    if (-not (Test-Path $Source)) { throw "Model nedostaje: $Source" }
    $hash = HashOf $Source
    $url = Reuse-PreviousAssetUrl $TargetName $hash
    if ([string]::IsNullOrWhiteSpace($url)) {
        Copy-Item $Source (Join-Path $out $TargetName) -Force
        $assets.Add((Join-Path $out $TargetName))
        $url = "https://github.com/$ReleaseRepo/releases/download/$tag/$TargetName"
        Info "Model je promijenjen ili nov: $TargetName"
    } else {
        Info "Model je nepromijenjen; ne uploadujem ponovo: $TargetName"
    }
    $modelDownloads.Add("  DownloadPage.Add('$url', '$TargetName', '$hash');")
    $modelCopies.Add("  CopyFile(ExpandConstant('{tmp}\\$TargetName'), ExpandConstant('{app}\\models\\$TargetName'), False);")
    $updateModelDownloads.Add("  if CompareText(GetSHA256OfFile(ExpandConstant('{app}\\models\\$TargetName')), '$hash') <> 0 then begin")
    $updateModelDownloads.Add("    DownloadPage.Add('$url', '$TargetName', '$hash');")
    $updateModelDownloads.Add('    ModelDownloadCount := ModelDownloadCount + 1;')
    $updateModelDownloads.Add('  end;')
    $updateModelCopies.Add("  if FileExists(ExpandConstant('{tmp}\\$TargetName')) then CopyFile(ExpandConstant('{tmp}\\$TargetName'), ExpandConstant('{app}\\models\\$TargetName'), False);")
    $modelComponents.Add(@{ id = "image-model-$Id"; url = $url; sha256 = $hash; size = (Get-Item $Source).Length })
    $modelIndexEntries.Add(@{ name = $TargetName; sha256 = $hash; url = $url })
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
                $part += 1; $written = 0
            }
            $output.Write($buffer, 0, $read); $written += $read
        }
        if ($output) { $output.Dispose() }
        $qwenPartCount = $part - 1
    } finally { $input.Dispose() }
    $qwenUpdateDownloads = New-Object System.Collections.Generic.List[string]
    for ($index = 1; $index -le $qwenPartCount; $index++) {
        $name = ('Qwen3-4B-Q4_K_M.part{0:d3}' -f $index)
        $partPath = Join-Path $out $name
        $partHash = HashOf $partPath
        $partURL = Reuse-PreviousAssetUrl $name $partHash
        if ([string]::IsNullOrWhiteSpace($partURL)) {
            $assets.Add($partPath)
            $partURL = "https://github.com/$ReleaseRepo/releases/download/$tag/$name"
            Info "Qwen dio je promijenjen ili nov: $name"
        } else {
            Remove-Item -LiteralPath $partPath -Force
            Info "Qwen dio je nepromijenjen; ne uploadujem ponovo: $name"
        }
        $modelDownloads.Add("  DownloadPage.Add('$partURL', '$name', '$partHash');")
        $qwenUpdateDownloads.Add("    DownloadPage.Add('$partURL', '$name', '$partHash');")
        $qwenUpdateDownloads.Add('    ModelDownloadCount := ModelDownloadCount + 1;')
        $modelIndexEntries.Add(@{ name = $name; sha256 = $partHash; url = $partURL })
    }
    if ($qwenPartCount -gt 0) {
        $updateModelDownloads.Add("  if CompareText(GetSHA256OfFile(ExpandConstant('{app}\\models\\Qwen3-4B-Q4_K_M.gguf')), '$qwenHash') <> 0 then begin")
        foreach ($line in $qwenUpdateDownloads) { $updateModelDownloads.Add($line) }
        $updateModelDownloads.Add('  end;')
    }
}

$modelDownloads | Set-Content -LiteralPath $generatedDownloads -Encoding utf8
$modelCopies | Set-Content -LiteralPath $generatedCopies -Encoding utf8
$updateModelDownloads | Set-Content -LiteralPath $generatedUpdateDownloads -Encoding utf8
$updateModelCopies | Set-Content -LiteralPath $generatedUpdateCopies -Encoding utf8
$skipModelDownload = if ($DryRun) { 1 } else { 0 }
@"
#define AppVersion "$Version"
#define OutputDir "$out"
#define StageDir "$stage"
#define ExtrasDir "$extrasRoot"
#define VCRedist "$vcRedist"
#define SkipModelDownload $skipModelDownload
#define QwenPartCount $qwenPartCount
#define QwenHash "$qwenHash"
"@ | Set-Content -LiteralPath $generated -Encoding utf8

@"
#define AppVersion "$Version"
#define OutputDir "$out"
#define StageDir "$stage"
#define ExtrasDir "$extrasRoot"
#define VCRedist "$vcRedist"
#define UpdateResultFile "$updateResultFile"
#define QwenPartCount $qwenPartCount
#define QwenHash "$qwenHash"
"@ | Set-Content -LiteralPath $generatedUpdate -Encoding utf8

@{ version = $Version; notes = $ReleaseNotes } | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath $updateResultFile -Encoding utf8

Info 'Pravim ASI-Setup.exe'
& $iscc (Join-Path $installerDir 'ASI.iss')
if ($LASTEXITCODE -ne 0) { throw 'Installer build nije uspio.' }
$setup = Join-Path $out "ASI-Setup-v$Version.exe"
$assets.Add($setup)

Info 'Pravim Inno Setup updater.'
& $iscc (Join-Path $installerDir 'ASI-Update.iss')
if ($LASTEXITCODE -ne 0) { throw 'Update installer build nije uspio.' }
$updateSetup = Join-Path $out "ASI-Update-Setup-v$Version.exe"
$assets.Add($updateSetup)

# This tiny immutable index carries forward URLs for unchanged model assets.
# Thus a later release only needs to read the immediately preceding index.
@{ models = $modelIndexEntries } | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $out 'models-index.json') -Encoding utf8
$assets.Add((Join-Path $out 'models-index.json'))

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
$draftAssets = ((& gh release view $tag --repo $ReleaseRepo --json assets | ConvertFrom-Json).assets | ForEach-Object { [string]$_.name })
if ($draftAssets -notcontains (Split-Path -Leaf $setup)) {
    throw 'Draft release nema ASI-Setup.exe fallback. Release neće biti objavljen.'
}
if ($draftAssets -notcontains (Split-Path -Leaf $updateSetup)) {
    throw 'Draft release nema Inno Setup updater. Release neće biti objavljen.'
}

$components = @(
    @{ id = 'app'; url = "https://github.com/$ReleaseRepo/releases/download/$tag/ASI.exe"; sha256 = HashOf (Join-Path $out 'ASI.exe'); size = (Get-Item (Join-Path $out 'ASI.exe')).Length },
    @{ id = 'updater'; url = "https://github.com/$ReleaseRepo/releases/download/$tag/ASI-Updater.exe"; sha256 = HashOf (Join-Path $out 'ASI-Updater.exe'); size = (Get-Item (Join-Path $out 'ASI-Updater.exe')).Length },
    @{ id = 'frontend'; url = "https://github.com/$ReleaseRepo/releases/download/$tag/ASI-frontend-$tag.zip"; sha256 = HashOf (Join-Path $out "ASI-frontend-$tag.zip"); size = (Get-Item (Join-Path $out "ASI-frontend-$tag.zip")).Length }
)
if (Test-Path (Join-Path $out "ASI-extras-$tag.zip")) { $components += @{ id = 'extras'; url = "https://github.com/$ReleaseRepo/releases/download/$tag/ASI-extras-$tag.zip"; sha256 = HashOf (Join-Path $out "ASI-extras-$tag.zip"); size = (Get-Item (Join-Path $out "ASI-extras-$tag.zip")).Length } }
if ($content.ImageAI) { $components += $modelComponents.ToArray() }
@{ version = $Version; notes = $ReleaseNotes; components = $components; updateSetup = @{ url = "https://github.com/$ReleaseRepo/releases/download/$tag/$(Split-Path -Leaf $updateSetup)"; sha256 = HashOf $updateSetup; size = (Get-Item $updateSetup).Length } } | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $out 'latest.json') -Encoding utf8
& gh release upload $tag (Join-Path $out 'latest.json') --repo $ReleaseRepo
& gh release edit $tag --draft=false --latest --repo $ReleaseRepo
git tag $tag; git push origin HEAD; git push origin $tag
Info "Objavljeno: https://github.com/$ReleaseRepo/releases/tag/$tag"
