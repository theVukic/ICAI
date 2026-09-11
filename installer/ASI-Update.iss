; This is the native Inno Setup updater shown after the user accepts an update.
; Its generated include only contains public release URLs and SHA-256 values.
#include "generated-update.iss"

[Setup]
AppId={{6C2B4E24-521B-4E8E-915F-AB9063E9498A}
AppName=ASI ažuriranje
AppVersion={#AppVersion}
AppPublisher=ASI
DefaultDirName={localappdata}\Programs\ASI
DisableDirPage=yes
DisableProgramGroupPage=yes
DisableWelcomePage=no
DisableReadyPage=yes
CreateUninstallRegKey=no
Uninstallable=no
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
Compression=lzma2
SolidCompression=yes
CloseApplications=yes
RestartApplications=no
OutputDir={#OutputDir}
OutputBaseFilename=ASI-Update-Setup-v{#AppVersion}

[Files]
; Core files are deliberately embedded in this small updater. Large AI models
; are fetched only when their SHA-256 differs from the installed model.
Source: "{#StageDir}\ASI.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#StageDir}\ASI-Updater.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#StageDir}\dist\*"; DestDir: "{app}\dist"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#StageDir}\models\*.dll"; DestDir: "{app}\models"; Flags: ignoreversion skipifsourcedoesntexist
Source: "{#StageDir}\models\*.exe"; DestDir: "{app}\models"; Flags: ignoreversion skipifsourcedoesntexist
Source: "{#StageDir}\background\*"; DestDir: "{app}\background"; Flags: onlyifdoesntexist recursesubdirs createallsubdirs skipifsourcedoesntexist
Source: "{#ExtrasDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs skipifsourcedoesntexist
Source: "{#UpdateResultFile}"; DestDir: "{localappdata}\ASIapp"; DestName: "update-result.json"; Flags: ignoreversion
Source: "{#VCRedist}"; Flags: dontcopy

[Run]
Filename: "{app}\ASI.exe"; Description: "Pokreni ASI"; Flags: nowait postinstall skipifsilent

[Code]
var
  DownloadPage: TDownloadWizardPage;
  ModelDownloadCount: Integer;
  UpdateStarted: Boolean;

procedure DownloadChangedModels();
begin
  DownloadPage.Clear;
  ModelDownloadCount := 0;
#include "generated-update-model-downloads.iss"
  if ModelDownloadCount = 0 then Exit;
  DownloadPage.Show;
  try
    DownloadPage.Download;
  finally
    DownloadPage.Hide;
  end;
end;

function HasVisualCppRuntime(): Boolean;
var
  Installed: Cardinal;
begin
  Result := RegQueryDWordValue(HKLM64, 'SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\x64', 'Installed', Installed) and (Installed = 1);
end;

procedure InstallVisualCppRuntime();
var
  ResultCode: Integer;
begin
  if HasVisualCppRuntime() then begin
    Log('Microsoft Visual C++ Runtime (x64) je već instaliran; preskačem.');
    Exit;
  end;
  ExtractTemporaryFile('vc_redist.x64.exe');
  if not Exec(ExpandConstant('{tmp}\vc_redist.x64.exe'), '/install /quiet /norestart', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
    RaiseException('Microsoft Visual C++ Runtime nije moguće pokrenuti.');
  if (ResultCode <> 0) and (ResultCode <> 1638) and (ResultCode <> 3010) then
    RaiseException('Microsoft Visual C++ Runtime instalacija nije uspjela (kod ' + IntToStr(ResultCode) + ').');
end;

procedure JoinQwenModel(); forward;

procedure CopyDownloadedModels();
begin
  ForceDirectories(ExpandConstant('{app}\models'));
#include "generated-update-model-copies.iss"
  if FileExists(ExpandConstant('{tmp}\Qwen3-4B-Q4_K_M.part001')) then JoinQwenModel();
end;

procedure JoinQwenModel();
var
  OutputStream, InputStream: TFileStream;
  Index: Integer;
  PartPath, FinalPath: String;
begin
  FinalPath := ExpandConstant('{app}\models\Qwen3-4B-Q4_K_M.gguf');
  OutputStream := TFileStream.Create(FinalPath + '.tmp', fmCreate);
  try
    for Index := 1 to {#QwenPartCount} do begin
      PartPath := ExpandConstant('{tmp}\Qwen3-4B-Q4_K_M.part' + Format('%.3d', [Index]));
      InputStream := TFileStream.Create(PartPath, fmOpenRead);
      try
        OutputStream.CopyFrom(InputStream, 0, $100000);
      finally
        InputStream.Free;
      end;
    end;
  finally
    OutputStream.Free;
  end;
  if CompareText(GetSHA256OfFile(FinalPath + '.tmp'), '{#QwenHash}') <> 0 then begin
    DeleteFile(FinalPath + '.tmp');
    RaiseException('Provjera Qwen modela nije uspjela.');
  end;
  if FileExists(FinalPath) then DeleteFile(FinalPath);
  RenameFile(FinalPath + '.tmp', FinalPath);
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := '';
  try
    InstallVisualCppRuntime();
    DownloadChangedModels();
    CopyDownloadedModels();
  except
    Result := 'Preuzimanje ažuriranja nije uspjelo: ' + GetExceptionMessage;
  end;
end;

procedure InitializeWizard();
begin
  DownloadPage := CreateDownloadPage('Preuzimanje ažuriranja', 'ASI preuzima samo promijenjene AI modele i provjerava ih.', nil);
  DownloadPage.ShowBaseNameInsteadOfUrl := True;
end;

procedure CurPageChanged(CurPageID: Integer);
begin
  // Inno remains the only visible updater, but it must not wait for another
  // confirmation after the user already clicked "Da, ažuriraj" in ASI.
  if (CurPageID = wpWelcome) and not UpdateStarted then begin
    UpdateStarted := True;
    WizardForm.NextButton.OnClick(nil);
  end;
end;
