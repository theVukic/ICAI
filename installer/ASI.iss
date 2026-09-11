; Generated values live in generated.iss. Never place tokens, pepper values or
; certificate credentials in this installer source.
#include "generated.iss"

[Setup]
AppId={{6C2B4E24-521B-4E8E-915F-AB9063E9498A}
AppName=ASI
AppVersion={#AppVersion}
AppPublisher=ASI
DefaultDirName={localappdata}\Programs\ASI
DisableDirPage=no
DefaultGroupName=ASI
OutputDir={#OutputDir}
OutputBaseFilename=ASI-Setup-v{#AppVersion}
UninstallDisplayIcon={app}\ASI.exe
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
Compression=lzma2
SolidCompression=yes
CloseApplications=yes
RestartApplications=no
DisableProgramGroupPage=yes

[Languages]
Name: "bosnian"; MessagesFile: "compiler:Default.isl"

[Files]
Source: "{#StageDir}\ASI.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#StageDir}\ASI-Updater.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#StageDir}\dist\*"; DestDir: "{app}\dist"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#StageDir}\models\*.dll"; DestDir: "{app}\models"; Flags: ignoreversion skipifsourcedoesntexist
Source: "{#StageDir}\models\*.exe"; DestDir: "{app}\models"; Flags: ignoreversion skipifsourcedoesntexist
Source: "{#StageDir}\background\*"; DestDir: "{app}\background"; Flags: onlyifdoesntexist recursesubdirs createallsubdirs skipifsourcedoesntexist
Source: "{#StageDir}\defaults\*"; DestDir: "{localappdata}\ASIapp"; Flags: onlyifdoesntexist recursesubdirs createallsubdirs skipifsourcedoesntexist
Source: "{#ExtrasDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs skipifsourcedoesntexist
Source: "{#VCRedist}"; Flags: dontcopy

[Icons]
Name: "{group}\ASI"; Filename: "{app}\ASI.exe"
Name: "{autodesktop}\ASI"; Filename: "{app}\ASI.exe"; Tasks: desktopicon

[Tasks]
Name: "desktopicon"; Description: "Kreiraj Desktop prečicu"; GroupDescription: "Prečice:"; Flags: unchecked

[Run]
Filename: "{app}\ASI.exe"; Description: "Pokreni ASI"; Flags: nowait postinstall skipifsilent

[Code]
var
  ProfileCountPage: TInputQueryWizardPage;
  TokenPages: array of TInputQueryWizardPage;
  PepperPage: TInputQueryWizardPage;
  DownloadPage: TDownloadWizardPage;
  ProfilesCreated: Boolean;

function JsonEscape(const Value: String): String;
begin
  Result := Value;
  StringChangeEx(Result, '\', '\\', True);
  StringChangeEx(Result, '"', '\"', True);
  StringChangeEx(Result, #13#10, '\n', True);
  StringChangeEx(Result, #13, '\n', True);
  StringChangeEx(Result, #10, '\n', True);
end;

procedure CreateProfilePages();
var
  Count, Index, PreviousPage: Integer;
begin
  if ProfilesCreated then Exit;
  Count := StrToIntDef(Trim(ProfileCountPage.Values[0]), 0);
  if (Count < 1) or (Count > 20) then begin
    MsgBox('Unesite broj korisnika od 1 do 20.', mbError, MB_OK);
    Exit;
  end;
  SetArrayLength(TokenPages, Count);
  PreviousPage := ProfileCountPage.ID;
  for Index := 0 to Count - 1 do begin
    TokenPages[Index] := CreateInputQueryPage(PreviousPage, 'OLX korisnik ' + IntToStr(Index + 1), 'Podaci profila', 'Unesite ime profila i OLX API token. Token se provjerava prije instalacije.');
    TokenPages[Index].Add('Ime korisnika/profila:', False);
    TokenPages[Index].Add('OLX token:', True);
    PreviousPage := TokenPages[Index].ID;
  end;
  PepperPage := CreateInputQueryPage(PreviousPage, 'Pepper konfiguracija', 'Lokalna sigurnosna vrijednost', 'Unesite pepper. Sačuvat će se samo u %LOCALAPPDATA%\\ASIapp\\backend\\pepper.json.');
  PepperPage.Add('Pepper:', True);
  ProfilesCreated := True;
end;

function VerifyOLXToken(const Token: String; var ErrorText: String): Boolean;
var
  Request: Variant;
  Status: Integer;
begin
  Result := False;
  ErrorText := '';
  try
    Request := CreateOleObject('WinHttp.WinHttpRequest.5.1');
    Request.SetTimeouts(5000, 5000, 10000, 10000);
    Request.Open('GET', 'https://api.olx.ba/me', False);
    Request.SetRequestHeader('Authorization', 'Bearer ' + Trim(Token));
    Request.Send('');
    Status := Request.Status;
    Result := (Status >= 200) and (Status < 300);
    if not Result then ErrorText := 'OLX je odbio token (HTTP ' + IntToStr(Status) + ').';
  except
    ErrorText := 'Nije moguće provjeriti OLX token. Provjerite internet i pokušajte ponovo.';
  end;
end;

function NextButtonClick(CurPageID: Integer): Boolean;
var
  Index: Integer;
  NameValue, TokenValue, PepperValue, ErrorText: String;
begin
  Result := True;
  if CurPageID = ProfileCountPage.ID then begin
    CreateProfilePages();
    if not ProfilesCreated then Result := False;
    Exit;
  end;
  for Index := 0 to GetArrayLength(TokenPages) - 1 do begin
    if CurPageID = TokenPages[Index].ID then begin
      NameValue := Trim(TokenPages[Index].Values[0]);
      TokenValue := Trim(TokenPages[Index].Values[1]);
      if (NameValue = '') or (TokenValue = '') then begin
        MsgBox('Unesite ime korisnika i OLX token.', mbError, MB_OK);
        Result := False;
      end else if not VerifyOLXToken(TokenValue, ErrorText) then begin
        MsgBox('Token za korisnika "' + NameValue + '" nije potvrđen. ' + ErrorText, mbError, MB_OK);
        Result := False;
      end;
      Exit;
    end;
  end;
  if ProfilesCreated and (CurPageID = PepperPage.ID) then begin
    PepperValue := Trim(PepperPage.Values[0]);
    if PepperValue = '' then begin
      MsgBox('Unesite pepper.', mbError, MB_OK);
      Result := False;
    end;
  end;
end;

procedure WriteConfiguration();
var
  ConfigDir, TokensJSON: String;
  Index: Integer;
begin
  ConfigDir := ExpandConstant('{localappdata}\ASIapp\backend');
  ForceDirectories(ConfigDir);
  TokensJSON := '{"tokens":[';
  for Index := 0 to GetArrayLength(TokenPages) - 1 do begin
    if Index > 0 then TokensJSON := TokensJSON + ',';
    TokensJSON := TokensJSON + '{"name":"' + JsonEscape(Trim(TokenPages[Index].Values[0])) + '","token":"' + JsonEscape(Trim(TokenPages[Index].Values[1])) + '"}';
  end;
  TokensJSON := TokensJSON + ']}';
  SaveStringToFile(AddBackslash(ConfigDir) + 'token.json', TokensJSON, False);
  SaveStringToFile(AddBackslash(ConfigDir) + 'pepper.json', '{"pepper":"' + JsonEscape(Trim(PepperPage.Values[0])) + '"}', False);
end;

procedure DownloadModels();
begin
  if {#SkipModelDownload} = 1 then begin
    Log('Dry-run setup: preskačem preuzimanje modela jer GitHub release nije objavljen.');
    Exit;
  end;
  DownloadPage.Clear;
#include "generated-model-downloads.iss"
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
  // The supported VC++ x64 installer records this value after a successful
  // install. Reading the registry is allowed for a per-user ASI install and
  // avoids launching the redistributable again on every setup/update.
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
  // 0 = installed/already current; 1638 = newer version already installed;
  // 3010 = installed and Windows reports that a restart is recommended.
  if (ResultCode <> 0) and (ResultCode <> 1638) and (ResultCode <> 3010) then
    RaiseException('Microsoft Visual C++ Runtime instalacija nije uspjela (kod ' + IntToStr(ResultCode) + ').');
end;

procedure JoinQwenModel(); forward;

procedure CopyDownloadedModels();
begin
  if {#SkipModelDownload} = 1 then Exit;
  ForceDirectories(ExpandConstant('{app}\models'));
#include "generated-model-copies.iss"
  if {#QwenPartCount} > 0 then JoinQwenModel();
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

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then begin
    WriteConfiguration();
  end;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := '';
  try
    InstallVisualCppRuntime();
    DownloadModels();
    CopyDownloadedModels();
  except
    Result := 'Preuzimanje AI modela nije uspjelo: ' + GetExceptionMessage;
  end;
end;

procedure InitializeWizard();
begin
  ProfileCountPage := CreateInputQueryPage(wpSelectDir, 'OLX korisnici', 'Broj korisnika', 'Unesite koliko OLX korisnika želite dodati. Svakom se posebno unosi ime i token.');
  ProfileCountPage.Add('Broj korisnika:', False);
  ProfileCountPage.Values[0] := '1';
  DownloadPage := CreateDownloadPage('Preuzimanje AI modela', 'ASI preuzima i provjerava potrebne AI modele.', nil);
  DownloadPage.ShowBaseNameInsteadOfUrl := True;
end;
